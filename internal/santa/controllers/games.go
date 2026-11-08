package controllers

import (
	"net/http"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
)

type GamesController struct {
	api     *httpapi.API
	service services.GameService
}

func NewGamesController(api *httpapi.API, service services.GameService) *GamesController {
	return &GamesController{api: api, service: service}
}

func (c *GamesController) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/games", c.api.JSON(c.list))
	mux.Handle("POST /api/games", c.api.JSON(c.create))
	mux.Handle("GET /api/games/{id}", c.api.JSON(c.get))
	mux.Handle("PATCH /api/games/{id}", c.api.JSON(c.update))
	mux.Handle("POST /api/games/{id}/readiness", c.api.JSON(c.readiness))
	mux.Handle("POST /api/games/{id}/draw", c.api.JSON(c.draw))
	mux.Handle("POST /api/games/{id}/reset", c.api.JSON(c.reset))
	mux.Handle("GET /api/games/{id}/receiver", c.api.JSON(c.receiver))
	mux.Handle("GET /api/games/{id}/announcements/latest", c.api.JSON(c.latestAnnouncement))
	mux.Handle("GET /api/games/{id}/announcements", c.api.JSON(c.announcements))
	mux.Handle("POST /api/games/{id}/announcements", c.api.JSON(c.announce))
}

func (c *GamesController) list(r *http.Request, actor services.Actor) (any, error) {
	return c.service.List(r.Context(), actor)
}

func (c *GamesController) create(r *http.Request, actor services.Actor) (any, error) {
	input := dto.CreateGame{SingleCycle: true, AllowMutualPairs: true}
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return nil, err
	}
	return c.service.Create(r.Context(), actor, input)
}

func (c *GamesController) get(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return c.service.Get(r.Context(), actor, id)
}

func (c *GamesController) update(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	var input dto.UpdateGame
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return nil, err
	}
	return c.service.Update(r.Context(), actor, id, input)
}

func (c *GamesController) drawInput(r *http.Request) (int64, dto.DrawGame, error) {
	id, err := gameID(r)
	if err != nil {
		return 0, dto.DrawGame{}, err
	}
	var input dto.DrawGame
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return 0, dto.DrawGame{}, err
	}
	return id, input, nil
}

func (c *GamesController) readiness(r *http.Request, actor services.Actor) (any, error) {
	id, input, err := c.drawInput(r)
	if err != nil {
		return nil, err
	}
	return c.service.CheckReadiness(r.Context(), actor, id, input)
}

func (c *GamesController) draw(r *http.Request, actor services.Actor) (any, error) {
	id, input, err := c.drawInput(r)
	if err != nil {
		return nil, err
	}
	return c.service.Draw(r.Context(), actor, id, input)
}

func (c *GamesController) reset(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return nil, c.service.Reset(r.Context(), actor, id)
}

func (c *GamesController) receiver(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return c.service.Receiver(r.Context(), actor, id)
}

func (c *GamesController) latestAnnouncement(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	return c.service.LatestAnnouncement(r.Context(), actor, id)
}

func (c *GamesController) announcements(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	cursor, err := after(r)
	if err != nil {
		return nil, err
	}
	return c.service.Announcements(r.Context(), actor, id, cursor)
}

func (c *GamesController) announce(r *http.Request, actor services.Actor) (any, error) {
	id, err := gameID(r)
	if err != nil {
		return nil, err
	}
	var input dto.Announcement
	if err := httpapi.DecodeJSON(r, &input); err != nil {
		return nil, err
	}
	return nil, c.service.Announce(r.Context(), actor, id, input)
}
