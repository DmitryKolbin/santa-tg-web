package projections

import "github.com/DmitryKolbin/santa-tg-web/internal/santa"

type GameDetails struct {
	santa.Game
	CreatorName string `bun:"creator_name"`
	IsMember    bool   `bun:"is_member"`
	MemberCount int    `bun:"member_count"`
}
