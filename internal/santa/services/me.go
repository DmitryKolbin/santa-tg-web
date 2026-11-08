package services

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type MeService interface {
	Get(context.Context, Actor) (dto.Profile, error)
	Save(context.Context, Actor, dto.SaveProfile) (dto.Profile, error)
}

type meService struct {
	uow       repos.UnitOfWork
	locations map[string]string
	notifier  Notifier
}

func NewMeService(uow repos.UnitOfWork, locations map[string]string, notifier Notifier) MeService {
	return &meService{uow: uow, locations: locations, notifier: notifier}
}

func (s *meService) Get(ctx context.Context, actor Actor) (dto.Profile, error) {
	return execute(ctx, s.uow, actor, s.notifier, "", func(repositories repos.Repositories, _ *effects) (dto.Profile, error) {
		_, profile, err := readProfile(ctx, repositories.Users, actor.ID)
		return profile, err
	})
}

func (s *meService) Save(ctx context.Context, actor Actor, input dto.SaveProfile) (dto.Profile, error) {
	return execute(ctx, s.uow, actor, s.notifier, "", func(repositories repos.Repositories, e *effects) (dto.Profile, error) {
		user, message, err := NormalizeProfile(actor.ID, input, s.locations)
		if err != nil {
			return dto.Profile{}, bad(message)
		}
		old, _, err := readProfile(ctx, repositories.Users, actor.ID)
		if err != nil {
			return dto.Profile{}, err
		}
		if err := repositories.Users.Update(ctx, &user); err != nil {
			return dto.Profile{}, err
		}
		if old.Address != user.Address || old.Wishlist != user.Wishlist {
			senders, err := repositories.Users.ActiveSenders(ctx, actor.ID)
			if err != nil {
				return dto.Profile{}, err
			}
			for _, sender := range senders {
				e.notify(sender.ID, "Получатель обновил анкету в игре «"+sender.GameName+"».")
			}
		}
		_, profile, err := readProfile(ctx, repositories.Users, actor.ID)
		return profile, err
	})
}
