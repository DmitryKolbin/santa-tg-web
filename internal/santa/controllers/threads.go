package controllers

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type ThreadsController struct {
	api     *httpapi.API
	service services.ThreadService
}

func NewThreadsController(api *httpapi.API, service services.ThreadService) *ThreadsController {
	return &ThreadsController{api: api, service: service}
}

func (c *ThreadsController) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/games/{id}/threads", c.api.JSON(c.list))
	mux.Handle("GET /api/threads/{thread}/messages", c.api.JSON(c.messages))
	mux.Handle("POST /api/threads/{thread}/messages", c.api.JSON(c.send))
	mux.Handle("PUT /api/threads/{thread}/tracking", c.api.JSON(c.saveTracking))
	mux.Handle("POST /api/threads/{thread}/received", c.api.JSON(c.setReceived))
}

func (c *ThreadsController) list(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return c.service.List(r.Context(), actor, id)
}

func (c *ThreadsController) messages(r *http.Request, actor services.Actor) (any, error) {
	cursor, err := after(r)
	if err != nil {
		return nil, err
	}
	return c.service.Messages(r.Context(), actor, r.PathValue("thread"), cursor)
}

func (c *ThreadsController) send(r *http.Request, actor services.Actor) (any, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, c.service.Send(r.Context(), actor, r.PathValue("thread"), dto.SendMessage{})
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		return nil, services.Error{Status: http.StatusBadRequest, Text: "Не удалось загрузить сообщение. Максимальный размер файла — 20 МБ"}
	}
	count := 0
	for field, files := range r.MultipartForm.File {
		if field != "file" {
			return nil, services.Error{Status: http.StatusBadRequest, Text: "Неизвестное поле файла"}
		}
		count += len(files)
	}
	if count > 1 {
		return nil, services.Error{Status: http.StatusBadRequest, Text: "Можно приложить один файл"}
	}
	input := dto.SendMessage{Text: r.FormValue("text")}
	if count == 1 {
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, services.Error{Status: http.StatusBadRequest, Text: "Не удалось прочитать файл"}
		}
		defer file.Close()
		input.File = &dto.Upload{Reader: file, Name: header.Filename, Size: header.Size}
	}
	return nil, c.service.Send(r.Context(), actor, r.PathValue("thread"), input)
}

func (c *ThreadsController) saveTracking(r *http.Request, actor services.Actor) (any, error) {
	var input dto.SaveTracking
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return nil, err
	}
	return c.service.SaveTracking(r.Context(), actor, r.PathValue("thread"), input)
}

func (c *ThreadsController) setReceived(r *http.Request, actor services.Actor) (any, error) {
	var input dto.SetReceived
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return nil, err
	}
	return c.service.SetReceived(r.Context(), actor, r.PathValue("thread"), input)
}
