package services

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
)

type InviteService interface {
	Get(context.Context, Actor, string) (dto.Invite, error)
	Join(context.Context, Actor, string) (dto.GameID, error)
}

type inviteService struct{ uow repos.UnitOfWork }

func NewInviteService(uow repos.UnitOfWork) InviteService { return &inviteService{uow: uow} }

func (s *inviteService) Get(ctx context.Context, actor Actor, token string) (dto.Invite, error) {
	return execute(ctx, s.uow, actor, nil, func(repositories repos.Repositories, _ *effects) (dto.Invite, error) {
		details, err := repositories.Games.GetByInvite(ctx, actor.ID, token)
		g := gameFromDetails(details, actor.ID, "")
		return dto.Invite{ID: g.ID, Name: g.Name, CreatorName: g.CreatorName, Started: g.Started, IsMember: g.IsMember}, err
	})
}

func (s *inviteService) Join(ctx context.Context, actor Actor, token string) (dto.GameID, error) {
	return execute(ctx, s.uow, actor, nil, func(repositories repos.Repositories, _ *effects) (dto.GameID, error) {
		details, err := repositories.Games.GetByInvite(ctx, actor.ID, token)
		if err != nil {
			return dto.GameID{}, err
		}
		g := gameFromDetails(details, actor.ID, "")
		if g.IsMember {
			return dto.GameID{ID: g.ID}, nil
		}
		if g.Started {
			return dto.GameID{}, Error{Status: 409, Text: "Жеребьёвка уже прошла"}
		}
		if g.Count >= 50 {
			return dto.GameID{}, bad("В игре уже 50 участников")
		}
		if err := repositories.Games.AddMember(ctx, g.ID, actor.ID); err != nil {
			return dto.GameID{}, err
		}
		return dto.GameID{ID: g.ID}, nil
	})
}
