package services

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type LocationService interface {
	List(context.Context, Actor) (dto.Locations, error)
}

type locationService struct {
	uow       repos.UnitOfWork
	locations map[string]string
}

func NewLocationService(uow repos.UnitOfWork, locations map[string]string) LocationService {
	return &locationService{uow: uow, locations: locations}
}

func (s *locationService) List(ctx context.Context, actor Actor) (dto.Locations, error) {
	return execute(ctx, s.uow, actor, nil, func(_ repos.Repositories, _ *effects) (dto.Locations, error) {
		return dto.Locations(s.locations), nil
	})
}
