package santa

import (
	"context"
	"fmt"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

type Profile struct {
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	Wishlist     string   `json:"wishlist"`
	Location     string   `json:"location"`
	Destinations []string `json:"destinations"`
}

type SaveProfile struct {
	Address      string   `json:"address"`
	Wishlist     string   `json:"wishlist"`
	Location     string   `json:"location"`
	Destinations []string `json:"destinations"`
}

func toProfile(u store.User) Profile {
	return Profile{Name: u.Name, Address: u.Address, Wishlist: u.Wishlist, Location: u.Location, Destinations: []string(u.Destinations)}
}

func (s *Service) Profile(ctx context.Context, actor Actor) (Profile, error) {
	user, err := s.store.User(ctx, actor.ID)
	return toProfile(user), err
}

// SaveProfile updates the questionnaire and tells the actor's santas when the
// address or wishlist they rely on changes.
func (s *Service) SaveProfile(ctx context.Context, actor Actor, input SaveProfile) (Profile, error) {
	user, err := s.normalizeProfile(actor.ID, input)
	if err != nil {
		return Profile{}, err
	}
	var result Profile
	err = s.write(ctx, func(tx *store.Store, n *notes) error {
		old, err := tx.User(ctx, actor.ID)
		if err != nil {
			return err
		}
		if err := tx.UpdateProfile(ctx, &user); err != nil {
			return err
		}
		if old.Address != user.Address || old.Wishlist != user.Wishlist {
			senders, err := tx.ActiveSenders(ctx, actor.ID)
			if err != nil {
				return err
			}
			for _, sender := range senders {
				n.add(sender.ID, "Получатель обновил анкету в игре «"+sender.GameName+"».")
			}
		}
		user.Name = old.Name
		result = toProfile(user)
		return nil
	})
	return result, err
}

func (s *Service) normalizeProfile(userID int64, input SaveProfile) (store.User, error) {
	if len([]rune(input.Address)) > MaxAddress || len([]rune(input.Wishlist)) > MaxText {
		return store.User{}, invalid(fmt.Sprintf("Адрес — до %d, wishlist — до %d символов", MaxAddress, MaxText))
	}
	if input.Location != "" {
		if _, ok := s.locations[input.Location]; !ok {
			return store.User{}, invalid("Неизвестная локация")
		}
	}
	destinations := make(store.StringList, 0, len(input.Destinations))
	seen := map[string]bool{}
	for _, code := range input.Destinations {
		if _, ok := s.locations[code]; !ok {
			return store.User{}, invalid("Неизвестное направление")
		}
		if !seen[code] {
			seen[code] = true
			destinations = append(destinations, code)
		}
	}
	return store.User{ID: userID, Address: strings.TrimSpace(input.Address), Wishlist: strings.TrimSpace(input.Wishlist), Location: input.Location, Destinations: destinations}, nil
}
