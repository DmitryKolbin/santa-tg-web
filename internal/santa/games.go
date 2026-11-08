package santa

import "github.com/uptrace/bun"

type Game struct {
	bun.BaseModel    `bun:"table:games,alias:g"`
	ID               int64  `bun:"id,pk,autoincrement"`
	Name             string `bun:"name,notnull"`
	CreatorID        int64  `bun:"creator,notnull"`
	Invite           string `bun:"invite,notnull"`
	SingleCycle      bool   `bun:"single_cycle,notnull"`
	AllowMutualPairs bool   `bun:"allow_mutual_pairs,notnull"`
	Started          bool   `bun:"started,notnull"`
	Round            int    `bun:"round,notnull"`
}

type Member struct {
	bun.BaseModel `bun:"table:members,alias:mb"`
	GameID        int64 `bun:"game,pk"`
	PersonID      int64 `bun:"person,pk"`
}

type Assignment struct {
	bun.BaseModel `bun:"table:assignments,alias:a"`
	Thread        string `bun:"thread,unique,notnull"`
	GameID        int64  `bun:"game,pk"`
	Round         int    `bun:"round,pk"`
	SenderID      int64  `bun:"sender,pk"`
	ReceiverID    int64  `bun:"receiver,notnull"`
}
