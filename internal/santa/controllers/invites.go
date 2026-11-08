package controllers

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type InvitesController struct {
	api     *httpapi.API
	service services.InviteService
}

func NewInvitesController(api *httpapi.API, service services.InviteService) *InvitesController {
	return &InvitesController{api: api, service: service}
}

func (c *InvitesController) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/invites/{token}", c.api.JSON(c.get))
	mux.Handle("POST /api/invites/{token}/join", c.api.JSON(c.join))
}

func (c *InvitesController) get(r *http.Request, actor services.Actor) (any, error) {
	return c.service.Get(r.Context(), actor, r.PathValue("token"))
}

func (c *InvitesController) join(r *http.Request, actor services.Actor) (any, error) {
	return c.service.Join(r.Context(), actor, r.PathValue("token"))
}
