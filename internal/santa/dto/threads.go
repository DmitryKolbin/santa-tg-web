package dto

import "io"

type Thread struct {
	Key             string `json:"key"`
	GameName        string `json:"game_name,omitempty"`
	Round           int    `json:"round"`
	Side            string `json:"side"`
	CounterpartName string `json:"counterpart_name,omitempty"`
	TrackingNumber  string `json:"tracking_number"`
	Received        bool   `json:"received"`
	UnreadCount     int    `json:"unread_count"`
	ReadOnly        bool   `json:"read_only"`
}

type Message struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Mine     bool   `json:"mine"`
	FileKey  string `json:"file_key,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileType string `json:"file_type,omitempty"`
	Created  string `json:"created"`
}

type AnnouncementSummary struct {
	Latest      *Message `json:"latest,omitempty"`
	UnreadCount int      `json:"unread_count"`
}

type Upload struct {
	Reader io.ReadSeeker
	Name   string
	Size   int64
}

type SendMessage struct {
	Text string
	File *Upload
}

type SaveTracking struct {
	TrackingNumber string `json:"tracking_number"`
}

type Tracking struct {
	TrackingNumber string `json:"tracking_number"`
	Received       bool   `json:"received"`
}

type SetReceived struct {
	Received bool `json:"received"`
}

type Received struct {
	Received bool `json:"received"`
}
