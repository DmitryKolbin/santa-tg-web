package projections

type ActiveSender struct {
	ID       int64  `bun:"sender"`
	GameName string `bun:"name"`
}
