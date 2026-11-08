package dto

type Invite struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	CreatorName string `json:"creator_name"`
	Started     bool   `json:"started"`
	IsMember    bool   `json:"is_member"`
}

type GameID struct {
	ID int64 `json:"id"`
}
