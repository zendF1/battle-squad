package room

type RoomPlayer struct {
	PlayerID    string   `json:"playerId"`
	DisplayName string   `json:"displayName"`
	TeamID      int      `json:"teamId"` // 1 = Team A, 2 = Team B
	CharacterID string   `json:"characterId"`
	Items       []string `json:"items"` // max 3 items
	IsReady     bool     `json:"isReady"`
	IsHost      bool     `json:"isHost"`
}

type RoomState struct {
	RoomID       string       `json:"roomId"`
	HostPlayerID string       `json:"hostPlayerId"`
	Mode         string       `json:"mode"` // pvp_1v1, pvp_2v2
	MapID        string       `json:"mapId"`
	MaxPlayers   int          `json:"maxPlayers"`
	Players      []RoomPlayer `json:"players"`
	IsLocked     bool         `json:"isLocked"`
	PasswordHash string       `json:"-"`
	Status       string       `json:"status"` // waiting, in_match
}

type CreateRoomPayload struct {
	Mode     string  `json:"mode"`
	MapID    string  `json:"mapId"`
	Password *string `json:"password,omitempty"`
}

type JoinRoomPayload struct {
	RoomID   string  `json:"roomId"`
	Password *string `json:"password,omitempty"`
}

type ChangeTeamPayload struct {
	TeamID int `json:"teamId"`
}

type SelectCharacterPayload struct {
	CharacterID string `json:"characterId"`
}

type SelectItemsPayload struct {
	Items []string `json:"items"`
}
