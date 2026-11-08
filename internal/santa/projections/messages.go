package projections

type MessageDetails struct {
	ID       int64  `bun:"id"`
	Text     string `bun:"text"`
	Mine     bool   `bun:"mine"`
	FileKey  string `bun:"file_key"`
	FileName string `bun:"file_name"`
	FileType string `bun:"file_type"`
	Created  string `bun:"created"`
}

type ThreadDetails struct {
	Thread         string `bun:"thread"`
	GameID         int64  `bun:"game"`
	GameName       string `bun:"game_name"`
	Round          int    `bun:"round"`
	SenderID       int64  `bun:"sender"`
	ReceiverID     int64  `bun:"receiver"`
	CurrentRound   int    `bun:"current_round"`
	Started        bool   `bun:"started"`
	ReceiverName   string `bun:"receiver_name"`
	TrackingNumber string `bun:"tracking_number"`
	Received       bool   `bun:"received"`
	UnreadCount    int    `bun:"unread_count"`
}

type StoredFile struct {
	Key      string `bun:"key"`
	Name     string `bun:"name"`
	Type     string `bun:"type"`
	Sender   int64  `bun:"sender"`
	Receiver int64  `bun:"receiver"`
}
