// Package api is the HTTP API of the Telegram Mini App.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
)

type Server struct {
	svc    *santa.Service
	token  string
	origin string
	// names caches the last stored Telegram name per user, so that a user row
	// is written only on first contact or after a rename.
	names sync.Map
}

// New returns the API handler. appURL is the frontend URL; its origin is the
// only one allowed to call the API from a browser.
func New(svc *santa.Service, token, appURL string) http.Handler {
	parsed, _ := url.Parse(appURL)
	s := &Server{svc: svc, token: token, origin: parsed.Scheme + "://" + parsed.Host}
	return s.security(s.routes())
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /api/me", s.json(s.profile))
	mux.Handle("PUT /api/me", s.json(s.saveProfile))
	mux.Handle("GET /api/locations", s.json(s.locations))

	mux.Handle("GET /api/games", s.json(s.games))
	mux.Handle("POST /api/games", s.json(s.createGame))
	mux.Handle("GET /api/games/{id}", s.json(s.game))
	mux.Handle("PATCH /api/games/{id}", s.json(s.updateGame))
	mux.Handle("POST /api/games/{id}/readiness", s.json(s.readiness))
	mux.Handle("POST /api/games/{id}/draw", s.json(s.draw))
	mux.Handle("POST /api/games/{id}/reset", s.json(s.resetGame))
	mux.Handle("GET /api/games/{id}/receiver", s.json(s.receiver))
	mux.Handle("GET /api/games/{id}/announcements/latest", s.json(s.latestAnnouncement))
	mux.Handle("GET /api/games/{id}/announcements", s.json(s.announcements))
	mux.Handle("POST /api/games/{id}/announcements", s.json(s.announce))
	mux.Handle("GET /api/games/{id}/threads", s.json(s.threads))
	mux.Handle("GET /api/games/{id}/members", s.json(s.members))
	mux.Handle("DELETE /api/games/{id}/members/{user}", s.json(s.removeMember))
	mux.Handle("POST /api/games/{id}/blocked", s.json(s.block))
	mux.Handle("DELETE /api/games/{id}/blocked/{sender}/{receiver}", s.json(s.unblock))

	mux.Handle("GET /api/invites/{token}", s.json(s.invite))
	mux.Handle("POST /api/invites/{token}/join", s.json(s.join))

	mux.Handle("GET /api/threads/{thread}/messages", s.json(s.messages))
	mux.Handle("POST /api/threads/{thread}/messages", s.json(s.send))
	mux.Handle("PUT /api/threads/{thread}/tracking", s.json(s.saveTracking))
	mux.Handle("POST /api/threads/{thread}/received", s.json(s.setReceived))

	mux.Handle("GET /api/files/{key}", s.authed(s.file))

	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Not found", http.StatusNotFound) })
	return mux
}

type handler func(r *http.Request, actor santa.Actor) (any, error)

// authed validates Telegram init data and makes sure the user row exists
// before calling next.
func (s *Server) authed(next func(w http.ResponseWriter, r *http.Request, actor santa.Actor)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		user, err := validateInitData(strings.TrimPrefix(r.Header.Get("Authorization"), "tma "), s.token, time.Now())
		if err != nil {
			writeError(w, httpError{http.StatusUnauthorized, err.Error()})
			return
		}
		actor := santa.Actor{ID: user.ID, Name: strings.TrimSpace(user.FirstName + " " + user.LastName)}
		if name, ok := s.names.Load(actor.ID); !ok || name != actor.Name {
			if err := s.svc.EnsureUser(r.Context(), actor); err != nil {
				writeError(w, err)
				return
			}
			s.names.Store(actor.ID, actor.Name)
		}
		r.Body = http.MaxBytesReader(w, r.Body, santa.MaxUploadBytes+1<<20)
		next(w, r, actor)
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	})
}

// json serves an authenticated endpoint returning JSON; a nil result is
// reported as {"ok":true}.
func (s *Server) json(endpoint handler) http.Handler {
	return s.authed(func(w http.ResponseWriter, r *http.Request, actor santa.Actor) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		result, err := endpoint(r, actor)
		if err != nil {
			writeError(w, err)
			return
		}
		if result == nil {
			result = struct {
				OK bool `json:"ok"`
			}{true}
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.svc.Ping(ctx); err != nil {
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Add("Vary", "Origin")
			if r.Header.Get("Origin") == s.origin {
				w.Header().Set("Access-Control-Allow-Origin", s.origin)
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// httpError is an error raised by the API layer itself with its own status.
type httpError struct {
	status int
	text   string
}

func (e httpError) Error() string { return e.text }

func writeError(w http.ResponseWriter, err error) {
	status, text := http.StatusInternalServerError, "Ошибка сервера"
	var serviceError santa.Error
	var apiError httpError
	switch {
	case errors.As(err, &apiError):
		status, text = apiError.status, apiError.text
	case errors.As(err, &serviceError):
		status, text = statuses[serviceError.Kind], serviceError.Text
	case errors.Is(err, sql.ErrNoRows):
		status, text = http.StatusNotFound, "Не найдено"
	case errors.Is(err, context.DeadlineExceeded):
		status, text = http.StatusUnprocessableEntity, "Поиск занял больше двух секунд. Повторите попытку или измените настройки игры."
	case errors.Is(err, context.Canceled):
		status, text = http.StatusRequestTimeout, "Запрос отменён"
	default:
		slog.Error("api request failed", "error", err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{text})
}

var statuses = map[santa.ErrorKind]int{
	santa.Invalid:       http.StatusBadRequest,
	santa.Forbidden:     http.StatusForbidden,
	santa.Conflict:      http.StatusConflict,
	santa.Unprocessable: http.StatusUnprocessableEntity,
}

func badRequest(text string) error { return santa.Error{Kind: santa.Invalid, Text: text} }

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 65537))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return badRequest("Некорректные данные")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return badRequest("Некорректные данные")
	}
	return nil
}

func gameID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, badRequest("Некорректная игра")
	}
	return id, nil
}

func userID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		return 0, badRequest("Некорректный участник")
	}
	return id, nil
}

func cursor(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, badRequest("Некорректный курсор")
	}
	return value, nil
}

func (s *Server) file(w http.ResponseWriter, r *http.Request, actor santa.Actor) {
	file, err := s.svc.File(r.Context(), actor, r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}
	f, err := os.Open(file.Path)
	if err != nil {
		writeError(w, httpError{http.StatusNotFound, "Файл недоступен"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Disposition", `attachment; filename="attachment"`)
	http.ServeContent(w, r, file.Name, info.ModTime(), f)
}
