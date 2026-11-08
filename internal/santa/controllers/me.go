package controllers

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type MeController struct {
	api     *httpapi.API
	service services.MeService
}

func NewMeController(api *httpapi.API, service services.MeService) *MeController {
	return &MeController{api: api, service: service}
}

func (c *MeController) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/me", c.api.JSON(c.get))
	mux.Handle("PUT /api/me", c.api.JSON(c.save))
}

func (c *MeController) get(r *http.Request, actor services.Actor) (any, error) {
	return c.service.Get(r.Context(), actor)
}

func (c *MeController) save(r *http.Request, actor services.Actor) (any, error) {
	var input dto.SaveProfile
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return nil, err
	}
	return c.service.Save(r.Context(), actor, input)
}
