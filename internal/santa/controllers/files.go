package controllers

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type FilesController struct {
	api     *httpapi.API
	service services.FileService
}

func NewFilesController(api *httpapi.API, service services.FileService) *FilesController {
	return &FilesController{api: api, service: service}
}

func (c *FilesController) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/files/{key}", c.api.JSON(c.get))
}

func (c *FilesController) get(r *http.Request, actor services.Actor) (any, error) {
	return c.service.Get(r.Context(), actor, r.PathValue("key"))
}
