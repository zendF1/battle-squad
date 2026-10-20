// Package title serves the titles ("Danh hiệu"): cosmetic plates from config_titles that a
// player owns (player_titles, granted by milestone rewards {"kind":"title","id":...}) and
// wears one of (player_profiles.equipped_title_id), shown in the profile and in matches.
package title

// Rarities, lowest to highest: the plate colour.
const (
	RarityBlue   = "blue"
	RarityOrange = "orange"
	RarityPurple = "purple"
	RarityRed    = "red"
)

// Ref is a title as others see it: GET /player/titles equipped, POST /player/titles/equip,
// GET /player/profile equippedTitle.
type Ref struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Rarity string `json:"rarity"`
}

// Title is one active title with the player's ownership. ObtainedAt is RFC3339 (UTC,
// seconds), null when not owned.
type Title struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Rarity      string  `json:"rarity"`
	Description string  `json:"description"` // how to get it; clients show "Đạt: " + description
	Owned       bool    `json:"owned"`
	ObtainedAt  *string `json:"obtainedAt"`
}

// ListResponse answers GET /player/titles: active titles by sort order then id, and the
// equipped one (null when none, inactive or not owned).
type ListResponse struct {
	Titles   []Title `json:"titles"`
	Equipped *Ref    `json:"equipped"`
}

// EquipRequest is POST /player/titles/equip; a null, missing or "" titleId unequips.
type EquipRequest struct {
	TitleID *string `json:"titleId"`
}

type EquipResponse struct {
	Equipped *Ref `json:"equipped"`
}
