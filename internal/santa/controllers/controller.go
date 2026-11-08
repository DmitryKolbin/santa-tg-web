package controllers

import "net/http"

import (
	"strconv"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type Controller interface {
	RegisterRoutes(*http.ServeMux)
}

func gameID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, services.Error{Status: http.StatusBadRequest, Text: "Некорректная игра"}
	}
	return id, nil
}

func after(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, services.Error{Status: http.StatusBadRequest, Text: "Некорректный курсор"}
	}
	return value, nil
}
