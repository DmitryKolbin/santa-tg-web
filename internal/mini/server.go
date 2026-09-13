package mini

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed schema.sql
var schema string

type Server struct {
	db                              *sql.DB
	token, appURL, botName, uploads string
	locations                       map[string]string
	// ponytail: one process serializes database operations, including bounded draws.
	// Use per-game concurrency and database-level retries if throughput requires it.
	mu     sync.Mutex
	notify func(int64, string)
}

type apiError struct {
	status int
	text   string
}

func (e apiError) Error() string { return e.text }
func bad(text string) error      { return apiError{400, text} }
func forbidden() error           { return apiError{403, "Нет доступа"} }

type request struct {
	s             *Server
	tx            *sql.Tx
	r             *http.Request
	user          int64
	notifications []notification
	files         []string
}
type notification struct {
	user int64
	text string
}
type download struct{ key, name, mime string }
type endpoint func(*request) (any, error)

func New(db *sql.DB, token, appURL, botName, uploads string, locations map[string]string, notify func(int64, string)) (*Server, error) {
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(uploads, 0700); err != nil {
		return nil, err
	}
	return &Server{db: db, token: token, appURL: appURL, botName: botName, uploads: uploads, locations: locations, notify: notify}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.db.PingContext(ctx); err != nil {
			http.Error(w, "unhealthy", 503)
			return
		}
		w.Write([]byte("ok"))
	})
	routes := map[string]endpoint{
		"GET /api/me": getMe, "PUT /api/me": saveMe, "GET /api/locations": getLocations,
		"GET /api/games": listGames, "POST /api/games": createGame,
		"GET /api/invites/{token}": getInvite, "POST /api/invites/{token}/join": joinGame,
		"GET /api/games/{id}": getGame, "PATCH /api/games/{id}": editGame,
		"POST /api/games/{id}/readiness": readiness, "POST /api/games/{id}/draw": drawGame,
		"POST /api/games/{id}/reset": resetGame, "GET /api/games/{id}/receiver": getReceiver,
		"GET /api/games/{id}/threads":        listThreads,
		"GET /api/threads/{thread}/messages": listMessages, "POST /api/threads/{thread}/messages": sendMessage,
		"GET /api/games/{id}/announcements": listAnnouncements, "POST /api/games/{id}/announcements": announce,
		"GET /api/files/{key}": getFile,
	}
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, s.wrap(handler))
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Not found", 404) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Add("Vary", "Origin")
			if r.Header.Get("Origin") == s.appURLOrigin() {
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
			}
		}
		if r.Method == http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) appURLOrigin() string {
	parsed, _ := url.Parse(s.appURL)
	return parsed.Scheme + "://" + parsed.Host
}

func (s *Server) wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		user, err := validateInitData(strings.TrimPrefix(r.Header.Get("Authorization"), "tma "), s.token, time.Now())
		if err != nil {
			writeError(w, apiError{401, err.Error()})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
		s.mu.Lock()
		result, notes, err := s.execute(r, user, fn)
		s.mu.Unlock()
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		if err != nil {
			writeError(w, err)
			return
		}
		for _, note := range notes {
			if s.notify != nil {
				s.notify(note.user, note.text)
			}
		}
		if file, ok := result.(download); ok {
			f, err := os.Open(filepath.Join(s.uploads, file.key))
			if err != nil {
				writeError(w, apiError{404, "Файл недоступен"})
				return
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				writeError(w, err)
				return
			}
			w.Header().Set("Content-Type", file.mime)
			w.Header().Set("Content-Disposition", "attachment; filename=\"attachment\"")
			http.ServeContent(w, r, file.name, st.ModTime(), f)
			return
		}
		if result == nil {
			result = map[string]bool{"ok": true}
		}
		json.NewEncoder(w).Encode(result)
	}
}

func (s *Server) execute(r *http.Request, user telegramUser, fn endpoint) (result any, notes []notification, err error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, nil, err
	}
	q := &request{s: s, tx: tx, r: r, user: user.ID}
	defer func() {
		tx.Rollback()
		if err != nil {
			for _, key := range q.files {
				os.Remove(filepath.Join(s.uploads, key))
			}
		}
	}()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO users(id,name) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name`, user.ID, strings.TrimSpace(user.FirstName+" "+user.LastName))
	if err != nil {
		return nil, nil, err
	}
	result, err = fn(q)
	if err != nil {
		return nil, nil, err
	}
	err = tx.Commit()
	return result, q.notifications, err
}

func writeError(w http.ResponseWriter, err error) {
	status, text := 500, "Ошибка сервера"
	var ae apiError
	switch {
	case errors.As(err, &ae):
		status, text = ae.status, ae.text
	case errors.Is(err, sql.ErrNoRows):
		status, text = 404, "Не найдено"
	case errors.Is(err, context.DeadlineExceeded):
		status, text = 422, "Поиск занял больше двух секунд. Повторите попытку или измените настройки игры."
	case errors.Is(err, context.Canceled):
		status, text = 408, "Запрос отменён"
	default:
		slog.Error("api request failed", "error", err)
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": text})
}
func (q *request) decode(target any) error {
	decoder := json.NewDecoder(io.LimitReader(q.r.Body, 65537))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return bad("Некорректные данные")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return bad("Некорректные данные")
	}
	return nil
}
func (q *request) exec(query string, args ...any) error {
	_, err := q.tx.ExecContext(q.r.Context(), query, args...)
	return err
}
func randomKey() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (q *request) gameID() (int64, error) {
	id, err := strconv.ParseInt(q.r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, bad("Некорректная игра")
	}
	return id, nil
}
func (q *request) message(user int64, text string) {
	q.notifications = append(q.notifications, notification{user, text})
}
func (q *request) notifyMembers(game int64, text string) error {
	rows, err := q.tx.QueryContext(q.r.Context(), `SELECT person FROM members WHERE game=?`, game)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		q.message(id, text)
	}
	return rows.Err()
}
func checkedText(text string, max int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > max {
		return "", bad(fmt.Sprintf("Введите текст от 1 до %d символов", max))
	}
	return text, nil
}
