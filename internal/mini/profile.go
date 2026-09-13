package mini

import (
	"encoding/json"
	"strings"
)

type profile struct {
	ID           int64    `json:"-"`
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	Wishlist     string   `json:"wishlist"`
	Location     string   `json:"location"`
	Destinations []string `json:"destinations"`
}

func (q *request) profile(id int64) (profile, error) {
	var p profile
	var destinations string
	p.ID = id
	err := q.tx.QueryRowContext(q.r.Context(), `SELECT name,address,wishlist,location,destinations FROM users WHERE id=?`, id).Scan(&p.Name, &p.Address, &p.Wishlist, &p.Location, &destinations)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(destinations), &p.Destinations)
	return p, err
}
func getMe(q *request) (any, error)        { return q.profile(q.user) }
func getLocations(q *request) (any, error) { return q.s.locations, nil }
func missing(p profile) []string {
	result := []string{}
	if strings.TrimSpace(p.Address) == "" {
		result = append(result, "Адрес")
	}
	if p.Location == "" {
		result = append(result, "Местоположение")
	}
	if len(p.Destinations) == 0 {
		result = append(result, "Направления отправки")
	}
	return result
}
func saveMe(q *request) (any, error) {
	var input struct {
		Address      string   `json:"address"`
		Wishlist     string   `json:"wishlist"`
		Location     string   `json:"location"`
		Destinations []string `json:"destinations"`
	}
	if err := q.decode(&input); err != nil {
		return nil, err
	}
	if len([]rune(input.Address)) > 2000 || len([]rune(input.Wishlist)) > 4000 {
		return nil, bad("Адрес — до 2000, wishlist — до 4000 символов")
	}
	if input.Location != "" {
		if _, ok := q.s.locations[input.Location]; !ok {
			return nil, bad("Неизвестная локация")
		}
	}
	destinations := []string{}
	seen := map[string]bool{}
	for _, code := range input.Destinations {
		if _, ok := q.s.locations[code]; !ok {
			return nil, bad("Неизвестное направление")
		}
		if !seen[code] {
			seen[code] = true
			destinations = append(destinations, code)
		}
	}
	old, err := q.profile(q.user)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(destinations)
	if err != nil {
		return nil, err
	}
	input.Address = strings.TrimSpace(input.Address)
	input.Wishlist = strings.TrimSpace(input.Wishlist)
	if err := q.exec(`UPDATE users SET address=?,wishlist=?,location=?,destinations=? WHERE id=?`, input.Address, input.Wishlist, input.Location, string(encoded), q.user); err != nil {
		return nil, err
	}
	if old.Address != input.Address || old.Wishlist != input.Wishlist {
		rows, err := q.tx.QueryContext(q.r.Context(), `SELECT a.sender,g.name FROM assignments a JOIN games g ON g.id=a.game AND g.round=a.round WHERE a.receiver=? AND g.started=1`, q.user)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return nil, err
			}
			q.message(id, "Получатель обновил анкету в игре «"+name+"».")
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return q.profile(q.user)
}
