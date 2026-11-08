package santa

import "github.com/uptrace/bun"

type StringList []string

type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	ID            int64      `bun:"id,pk"`
	Name          string     `bun:"name,notnull"`
	Address       string     `bun:"address,notnull"`
	Wishlist      string     `bun:"wishlist,notnull"`
	Location      string     `bun:"location,notnull"`
	Destinations  StringList `bun:"destinations,type:json,notnull"`
}
