package dto

type Game struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	CreatorName      string `json:"creator_name"`
	SingleCycle      bool   `json:"single_cycle"`
	AllowMutualPairs bool   `json:"allow_mutual_pairs"`
	Started          bool   `json:"started"`
	Round            int    `json:"round"`
	IsOwner          bool   `json:"is_owner"`
	IsMember         bool   `json:"is_member"`
	Count            int    `json:"count"`
	InviteURL        string `json:"invite_url,omitempty"`
}

type CreateGame struct {
	Name             string `json:"name"`
	SingleCycle      bool   `json:"single_cycle"`
	AllowMutualPairs bool   `json:"allow_mutual_pairs"`
}

type UpdateGame struct {
	Name             *string `json:"name"`
	SingleCycle      *bool   `json:"single_cycle"`
	AllowMutualPairs *bool   `json:"allow_mutual_pairs"`
}

type DrawGame struct {
	Force      bool   `json:"force"`
	Exclusions string `json:"exclusions"`
}

type Readiness struct {
	Exclusions string            `json:"exclusions"`
	Members    []MemberReadiness `json:"members"`
	Excluded   []string          `json:"excluded"`
	Problems   []string          `json:"problems"`
	Ready      bool              `json:"ready"`
	Error      string            `json:"error,omitempty"`
}

type MemberReadiness struct {
	Name          string   `json:"name"`
	Missing       []string `json:"missing"`
	WishlistEmpty bool     `json:"wishlist_empty"`
}

type Receiver struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Wishlist string `json:"wishlist"`
}

type Announcement struct {
	Text string `json:"text"`
}
