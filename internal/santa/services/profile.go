package services

import (
	"context"
	"errors"
	"strings"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/dto"
)

var ErrInvalidProfile = errors.New("invalid profile")

type userReader interface {
	Get(context.Context, int64) (santa.User, error)
}

func NormalizeProfile(userID int64, input dto.SaveProfile, locations map[string]string) (santa.User, string, error) {
	if len([]rune(input.Address)) > 2000 || len([]rune(input.Wishlist)) > 4000 {
		return santa.User{}, "Адрес — до 2000, wishlist — до 4000 символов", ErrInvalidProfile
	}
	if input.Location != "" {
		if _, ok := locations[input.Location]; !ok {
			return santa.User{}, "Неизвестная локация", ErrInvalidProfile
		}
	}
	destinations := make(santa.StringList, 0, len(input.Destinations))
	seen := map[string]bool{}
	for _, code := range input.Destinations {
		if _, ok := locations[code]; !ok {
			return santa.User{}, "Неизвестное направление", ErrInvalidProfile
		}
		if !seen[code] {
			seen[code] = true
			destinations = append(destinations, code)
		}
	}
	return santa.User{ID: userID, Address: strings.TrimSpace(input.Address), Wishlist: strings.TrimSpace(input.Wishlist), Location: input.Location, Destinations: destinations}, "", nil
}

func readProfile(ctx context.Context, repo userReader, id int64) (santa.User, dto.Profile, error) {
	user, err := repo.Get(ctx, id)
	return user, dto.Profile{Name: user.Name, Address: user.Address, Wishlist: user.Wishlist, Location: user.Location, Destinations: []string(user.Destinations)}, err
}
