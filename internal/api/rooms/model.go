package rooms

type RoomSummary struct {
	RoomID       string `json:"roomId"`
	HostPlayerID string `json:"hostPlayerId"`
	Mode         string `json:"mode"`
	MapID        string `json:"mapId"`
	MaxPlayers   int    `json:"maxPlayers"`
	PlayerCount  int    `json:"playerCount"`
	IsLocked     bool   `json:"isLocked"`
	Status       string `json:"status"`
}
