package santa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

type Game struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	CreatorName      string `json:"creator_name"`
	SingleCycle      bool   `json:"single_cycle"`
	AllowMutualPairs bool   `json:"allow_mutual_pairs"`
	Started          bool   `json:"started"`
	Finished         bool   `json:"finished"`
	Round            int    `json:"round"`
	IsOwner          bool   `json:"is_owner"`
	IsMember         bool   `json:"is_member"`
	Count            int    `json:"count"`
	InviteURL        string `json:"invite_url,omitempty"`
	// Missing lists what the actor still has to fill in to be drawn.
	Missing []string `json:"missing,omitempty"`
}

type CreateGame struct {
	Name             string `json:"name"`
	SingleCycle      bool   `json:"single_cycle"`
	AllowMutualPairs bool   `json:"allow_mutual_pairs"`
}

type UpdateGame struct {
	Name             *string `json:"name"`
	SingleCycle      *bool   `json:"single_cycle"`
	AllowMutualPairs *bool   `json:"allow_mutual_pairs"`
}

type Receiver struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Wishlist string `json:"wishlist"`
}

func (s *Service) toGame(v store.GameView, actor int64, missing []string) Game {
	g := Game{ID: v.ID, Name: v.Name, CreatorName: v.CreatorName, SingleCycle: v.SingleCycle, AllowMutualPairs: v.AllowMutualPairs, Started: v.Started, Finished: v.Finished, Round: v.Round, IsOwner: v.CreatorID == actor, IsMember: v.IsMember, Count: v.MemberCount}
	if g.IsOwner && s.botName != "" {
		g.InviteURL = "https://t.me/" + s.botName + "?startapp=" + url.QueryEscape(v.Invite)
	}
	if g.IsMember && !g.Started && !g.Finished && len(missing) > 0 {
		g.Missing = missing
	}
	return g
}

// gameFor loads a game visible to the actor: its creator or a member.
func gameFor(ctx context.Context, db *store.Store, actor, id int64) (store.GameView, error) {
	g, err := db.GameView(ctx, actor, id)
	if err == nil && g.CreatorID != actor && !g.IsMember {
		return g, forbidden()
	}
	return g, err
}

// activeGame loads a game the actor created that is not archived.
func activeGame(ctx context.Context, db *store.Store, actor, id int64) (store.GameView, error) {
	g, err := ownedGame(ctx, db, actor, id)
	if err == nil && g.Finished {
		return g, conflict("Игра завершена. Сначала верните её из архива.")
	}
	return g, err
}

// ownedGame loads a game the actor created.
func ownedGame(ctx context.Context, db *store.Store, actor, id int64) (store.GameView, error) {
	g, err := gameFor(ctx, db, actor, id)
	if err == nil && g.CreatorID != actor {
		return g, forbidden()
	}
	return g, err
}

func (s *Service) Games(ctx context.Context, actor Actor) ([]Game, error) {
	views, err := s.store.GameViews(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	missing, err := s.missing(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	result := make([]Game, 0, len(views))
	for _, v := range views {
		result = append(result, s.toGame(v, actor.ID, missing))
	}
	return result, nil
}

func (s *Service) Game(ctx context.Context, actor Actor, id int64) (Game, error) {
	g, err := gameFor(ctx, s.store, actor.ID, id)
	if err != nil {
		return Game{}, err
	}
	missing, err := s.missing(ctx, actor.ID)
	return s.toGame(g, actor.ID, missing), err
}

func (s *Service) missing(ctx context.Context, actor int64) ([]string, error) {
	u, err := s.store.User(ctx, actor)
	return missingProfile(u), err
}

func (s *Service) CreateGame(ctx context.Context, actor Actor, input CreateGame) (Game, error) {
	name, err := checkedText(input.Name, MaxGameName)
	if err != nil {
		return Game{}, err
	}
	var result Game
	err = s.write(ctx, func(tx *store.Store, _ *notes) error {
		model := &store.Game{Name: name, CreatorID: actor.ID, Invite: randomKey(), SingleCycle: input.SingleCycle, AllowMutualPairs: input.AllowMutualPairs, Round: 1}
		if err := tx.CreateGame(ctx, model); err != nil {
			return err
		}
		g, err := tx.GameView(ctx, actor.ID, model.ID)
		result = s.toGame(g, actor.ID, nil)
		return err
	})
	return result, err
}

func (s *Service) UpdateGame(ctx context.Context, actor Actor, id int64, input UpdateGame) (Game, error) {
	var result Game
	err := s.write(ctx, func(tx *store.Store, _ *notes) error {
		g, err := activeGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if g.Started {
			return conflict("Сначала сбросьте жеребьёвку")
		}
		if input.Name != nil {
			g.Name = *input.Name
		}
		if g.Name, err = checkedText(g.Name, MaxGameName); err != nil {
			return err
		}
		if input.SingleCycle != nil {
			g.SingleCycle = *input.SingleCycle
		}
		if input.AllowMutualPairs != nil {
			g.AllowMutualPairs = *input.AllowMutualPairs
		}
		if err := tx.UpdateGame(ctx, &g.Game, "name", "single_cycle", "allow_mutual_pairs"); err != nil {
			return err
		}
		result = s.toGame(g, actor.ID, nil)
		return nil
	})
	return result, err
}

// ResetGame cancels the current draw. Its threads stay readable but become
// read-only because the game moves to the next round.
func (s *Service) ResetGame(ctx context.Context, actor Actor, id int64) error {
	return s.write(ctx, func(tx *store.Store, n *notes) error {
		g, err := activeGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if !g.Started {
			return conflict("Игра ещё не разыграна")
		}
		if err := tx.UpdateGame(ctx, &store.Game{ID: g.ID, Started: false, Round: g.Round + 1}, "started", "round"); err != nil {
			return err
		}
		return notifyMembers(ctx, tx, n, g.ID, fmt.Sprintf("Жеребьёвка в игре «%s» сброшена. Прежние назначения больше не действуют.", g.Name))
	})
}

// FinishGame moves the game to the archive of every member. Chats and
// deliveries keep working; the draw setup is frozen until the game is reopened.
func (s *Service) FinishGame(ctx context.Context, actor Actor, id int64) error {
	return s.write(ctx, func(tx *store.Store, n *notes) error {
		g, err := activeGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if err := tx.UpdateGame(ctx, &store.Game{ID: g.ID, Finished: true}, "finished"); err != nil {
			return err
		}
		return notifyMembers(ctx, tx, n, g.ID, fmt.Sprintf("Организатор завершил игру «%s». Она перенесена в архив.", g.Name))
	})
}

// ReopenGame returns an archived game to the active list without notifying anyone.
func (s *Service) ReopenGame(ctx context.Context, actor Actor, id int64) error {
	return s.write(ctx, func(tx *store.Store, _ *notes) error {
		g, err := ownedGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if !g.Finished {
			return conflict("Игра не в архиве")
		}
		return tx.UpdateGame(ctx, &store.Game{ID: g.ID, Finished: false}, "finished")
	})
}

func (s *Service) Receiver(ctx context.Context, actor Actor, id int64) (Receiver, error) {
	g, err := gameFor(ctx, s.store, actor.ID, id)
	if err != nil {
		return Receiver{}, err
	}
	if !g.Started {
		return Receiver{}, conflict("Жеребьёвка ещё не прошла")
	}
	userID, err := s.store.ReceiverOf(ctx, g.ID, g.Round, actor.ID)
	if err != nil {
		return Receiver{}, err
	}
	user, err := s.store.User(ctx, userID)
	return Receiver{Name: user.Name, Address: user.Address, Wishlist: user.Wishlist}, err
}

type Invite struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	CreatorName string `json:"creator_name"`
	Started     bool   `json:"started"`
	Finished    bool   `json:"finished"`
	IsMember    bool   `json:"is_member"`
}

type GameID struct {
	ID int64 `json:"id"`
}

func (s *Service) Invite(ctx context.Context, actor Actor, token string) (Invite, error) {
	g, err := s.store.GameViewByInvite(ctx, actor.ID, token)
	return Invite{ID: g.ID, Name: g.Name, CreatorName: g.CreatorName, Started: g.Started, Finished: g.Finished, IsMember: g.IsMember}, err
}

func (s *Service) Join(ctx context.Context, actor Actor, token string) (GameID, error) {
	var result GameID
	err := s.write(ctx, func(tx *store.Store, _ *notes) error {
		g, err := tx.GameViewByInvite(ctx, actor.ID, token)
		if err != nil {
			return err
		}
		result.ID = g.ID
		if g.IsMember {
			return nil
		}
		if g.Finished {
			return conflict("Игра завершена")
		}
		if g.Started {
			return conflict("Жеребьёвка уже прошла")
		}
		if g.MemberCount >= MaxMembers {
			return invalid(fmt.Sprintf("В игре уже %d участников", MaxMembers))
		}
		return tx.AddMember(ctx, g.ID, actor.ID)
	})
	return result, err
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
