package controllers

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type LocationsController struct {
	api     *httpapi.API
	service services.LocationService
}

func NewLocationsController(api *httpapi.API, service services.LocationService) *LocationsController {
	return &LocationsController{api: api, service: service}
}

func (c *LocationsController) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/locations", c.api.JSON(c.list))
}

func (c *LocationsController) list(r *http.Request, actor services.Actor) (any, error) {
	return c.service.List(r.Context(), actor)
}
