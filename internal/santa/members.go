package santa

import (
	"context"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

// GameMember is a member as the organizer sees it before the draw.
type GameMember struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Missing       []string `json:"missing"`
	WishlistEmpty bool     `json:"wishlist_empty"`
	// Blocked lists the members this one will not be assigned to give a gift to.
	Blocked []int64 `json:"blocked"`
}

type BlockedPair struct {
	Sender   int64 `json:"sender"`
	Receiver int64 `json:"receiver"`
}

func (s *Service) Members(ctx context.Context, actor Actor, id int64) ([]GameMember, error) {
	g, err := ownedGame(ctx, s.store, actor.ID, id)
	if err != nil {
		return nil, err
	}
	ids, err := s.store.MemberIDs(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	pairs, err := s.store.BlockedPairs(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	blocked := map[int64][]int64{}
	for _, p := range pairs {
		blocked[p.SenderID] = append(blocked[p.SenderID], p.ReceiverID)
	}
	result := make([]GameMember, 0, len(ids))
	for _, id := range ids {
		u, err := s.store.User(ctx, id)
		if err != nil {
			return nil, err
		}
		m := GameMember{ID: u.ID, Name: u.Name, Missing: missingProfile(u), WishlistEmpty: u.Wishlist == "", Blocked: blocked[u.ID]}
		if m.Blocked == nil {
			m.Blocked = []int64{}
		}
		result = append(result, m)
	}
	return result, nil
}

// editableGame loads a game the actor created whose draw has not happened yet.
func editableGame(ctx context.Context, db *store.Store, actor, id int64) (store.GameView, error) {
	g, err := ownedGame(ctx, db, actor, id)
	if err == nil && g.Started {
		return g, conflict("Сначала сбросьте жеребьёвку")
	}
	return g, err
}

func requireMembers(ctx context.Context, db *store.Store, game int64, people ...int64) error {
	for _, person := range people {
		ok, err := db.IsMember(ctx, game, person)
		if err != nil {
			return err
		}
		if !ok {
			return invalid("Участник не найден в игре")
		}
	}
	return nil
}

// RemoveMember excludes a member from the game and tells them via the bot.
func (s *Service) RemoveMember(ctx context.Context, actor Actor, id, person int64) error {
	return s.write(ctx, func(tx *store.Store, n *notes) error {
		g, err := editableGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if err := requireMembers(ctx, tx, g.ID, person); err != nil {
			return err
		}
		if err := tx.RemoveMember(ctx, g.ID, person); err != nil {
			return err
		}
		if person != actor.ID {
			n.add(person, "Организатор исключил вас из игры «"+g.Name+"».")
		}
		return nil
	})
}

func (s *Service) Block(ctx context.Context, actor Actor, id int64, pair BlockedPair) error {
	if pair.Sender == pair.Receiver {
		return invalid("Нельзя запретить участнику дарить самому себе")
	}
	return s.write(ctx, func(tx *store.Store, _ *notes) error {
		g, err := editableGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		if err := requireMembers(ctx, tx, g.ID, pair.Sender, pair.Receiver); err != nil {
			return err
		}
		return tx.AddBlockedPair(ctx, &store.BlockedPair{GameID: g.ID, SenderID: pair.Sender, ReceiverID: pair.Receiver})
	})
}

func (s *Service) Unblock(ctx context.Context, actor Actor, id int64, pair BlockedPair) error {
	return s.write(ctx, func(tx *store.Store, _ *notes) error {
		g, err := editableGame(ctx, tx, actor.ID, id)
		if err != nil {
			return err
		}
		return tx.DeleteBlockedPair(ctx, &store.BlockedPair{GameID: g.ID, SenderID: pair.Sender, ReceiverID: pair.Receiver})
	})
}
