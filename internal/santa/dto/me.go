package dto

type Profile struct {
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	Wishlist     string   `json:"wishlist"`
	Location     string   `json:"location"`
	Destinations []string `json:"destinations"`
}

type SaveProfile struct {
	Address      string   `json:"address"`
	Wishlist     string   `json:"wishlist"`
	Location     string   `json:"location"`
	Destinations []string `json:"destinations"`
}
