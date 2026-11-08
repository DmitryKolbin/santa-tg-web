package dto

type Success struct {
	OK bool `json:"ok"`
}

type Error struct {
	Error string `json:"error"`
}
