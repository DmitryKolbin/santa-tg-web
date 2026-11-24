package store

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

type Game struct {
	bun.BaseModel    `bun:"table:games,alias:g"`
	ID               int64  `bun:"id,pk,autoincrement"`
	Name             string `bun:"name,notnull"`
	CreatorID        int64  `bun:"creator,notnull"`
	Invite           string `bun:"invite,notnull"`
	SingleCycle      bool   `bun:"single_cycle,notnull"`
	AllowMutualPairs bool   `bun:"allow_mutual_pairs,notnull"`
	Started          bool   `bun:"started,notnull"`
	Finished         bool   `bun:"finished,notnull"`
	Round            int    `bun:"round,notnull"`
}

type Member struct {
	bun.BaseModel `bun:"table:members,alias:mb"`
	GameID        int64 `bun:"game,pk"`
	PersonID      int64 `bun:"person,pk"`
}

// BlockedPair forbids the draw to assign Sender as the santa of Receiver.
type BlockedPair struct {
	bun.BaseModel `bun:"table:blocked_pairs,alias:bp"`
	GameID        int64 `bun:"game,pk"`
	SenderID      int64 `bun:"sender,pk"`
	ReceiverID    int64 `bun:"receiver,pk"`
}

// Assignment is one sender→receiver pair of a draw round; its Thread key
// identifies the anonymous conversation and the delivery of that pair.
type Assignment struct {
	bun.BaseModel `bun:"table:assignments,alias:a"`
	Thread        string `bun:"thread,unique,notnull"`
	GameID        int64  `bun:"game,pk"`
	Round         int    `bun:"round,pk"`
	SenderID      int64  `bun:"sender,pk"`
	ReceiverID    int64  `bun:"receiver,notnull"`
}

// Message is a thread message, or a game announcement when Thread is nil.
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

// MessageRead stores the last read message per conversation. Announcements
// use an empty Thread.
type MessageRead struct {
	bun.BaseModel `bun:"table:message_reads,alias:mr"`
	PersonID      int64  `bun:"person,pk"`
	GameID        int64  `bun:"game,pk"`
	Thread        string `bun:"thread,pk"`
	MessageID     int64  `bun:"message,notnull"`
}

// GameView is a game as seen by one user.
type GameView struct {
	Game
	CreatorName string `bun:"creator_name"`
	IsMember    bool   `bun:"is_member"`
	MemberCount int    `bun:"member_count"`
}

type ThreadView struct {
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

type MessageView struct {
	ID       int64  `bun:"id"`
	Text     string `bun:"text"`
	Mine     bool   `bun:"mine"`
	FileKey  string `bun:"file_key"`
	FileName string `bun:"file_name"`
	FileType string `bun:"file_type"`
	Created  string `bun:"created"`
}

type File struct {
	Key      string `bun:"key"`
	Name     string `bun:"name"`
	Type     string `bun:"type"`
	Sender   int64  `bun:"sender"`
	Receiver int64  `bun:"receiver"`
}

type ActiveSender struct {
	ID       int64  `bun:"sender"`
	GameName string `bun:"name"`
}
