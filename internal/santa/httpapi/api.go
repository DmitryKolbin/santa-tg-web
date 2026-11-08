package httpapi

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
	"strings"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type API struct {
	token string
}

func New(token string) *API { return &API{token: token} }

type Handler func(*http.Request, services.Actor) (any, error)

func (a *API) JSON(endpoint Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		user, err := validateInitData(strings.TrimPrefix(r.Header.Get("Authorization"), "tma "), a.token, time.Now())
		if err != nil {
			writeError(w, services.Error{Status: http.StatusUnauthorized, Text: err.Error()})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 21<<20)
		actor := services.Actor{ID: user.ID, Name: strings.TrimSpace(user.FirstName + " " + user.LastName)}
		result, err := endpoint(r, actor)
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if file, ok := result.(dto.Download); ok {
			serveFile(w, r, file)
			return
		}
		if result == nil {
			result = dto.Success{OK: true}
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func DecodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 65537))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return services.Error{Status: http.StatusBadRequest, Text: "Некорректные данные"}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return services.Error{Status: http.StatusBadRequest, Text: "Некорректные данные"}
	}
	return nil
}

func serveFile(w http.ResponseWriter, r *http.Request, file dto.Download) {
	f, err := os.Open(file.Path)
	if err != nil {
		writeError(w, services.Error{Status: http.StatusNotFound, Text: "Файл недоступен"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Disposition", "attachment; filename=\"attachment\"")
	http.ServeContent(w, r, file.Name, info.ModTime(), f)
}

func writeError(w http.ResponseWriter, err error) {
	status, text := http.StatusInternalServerError, "Ошибка сервера"
	var serviceError services.Error
	switch {
	case errors.As(err, &serviceError):
		status, text = serviceError.Status, serviceError.Text
	case errors.Is(err, sql.ErrNoRows):
		status, text = http.StatusNotFound, "Не найдено"
	case errors.Is(err, context.DeadlineExceeded):
		status, text = 422, "Поиск занял больше двух секунд. Повторите попытку или измените настройки игры."
	case errors.Is(err, context.Canceled):
		status, text = http.StatusRequestTimeout, "Запрос отменён"
	default:
		slog.Error("api request failed", "error", err)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dto.Error{Error: text})
}

func Security(appURL string, next http.Handler) http.Handler {
	parsed, _ := url.Parse(appURL)
	origin := parsed.Scheme + "://" + parsed.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Add("Vary", "Origin")
			if r.Header.Get("Origin") == origin {
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
			}
		}
		if r.Method == http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
