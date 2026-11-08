package santa

import "github.com/uptrace/bun"

type Message struct {
	bun.BaseModel `bun:"table:messages,alias:m"`
	ID            int64   `bun:"id,pk,autoincrement"`
	Thread        *string `bun:"thread"`
	GameID        int64   `bun:"game,notnull"`
	Round         int     `bun:"round,notnull"`
	SenderID      int64   `bun:"sender,notnull"`
	ReceiverID    *int64  `bun:"receiver"`
	Text          string  `bun:"text,notnull"`
	FileKey       string  `bun:"file_key,notnull"`
	FileName      string  `bun:"file_name,notnull"`
	FileType      string  `bun:"file_type,notnull"`
	Created       string  `bun:"created,notnull"`
}

type Delivery struct {
	bun.BaseModel  `bun:"table:deliveries,alias:d"`
	Thread         string `bun:"thread,pk"`
	TrackingNumber string `bun:"tracking_number,notnull"`
	Received       bool   `bun:"received,notnull"`
}

type MessageRead struct {
	bun.BaseModel `bun:"table:message_reads,alias:mr"`
	PersonID      int64  `bun:"person,pk"`
	GameID        int64  `bun:"game,pk"`
	Thread        string `bun:"thread,pk"`
	MessageID     int64  `bun:"message,notnull"`
}
