package room

import "encoding/json"

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
	RoomID       string            `json:"roomId"`
	HostPlayerID string            `json:"hostPlayerId"`
	Mode         string            `json:"mode"` // pvp_1v1, pvp_2v2
	MapID        string            `json:"mapId"`
	MaxPlayers   int               `json:"maxPlayers"`
	Players      []RoomPlayer      `json:"players"`
	IsLocked     bool              `json:"isLocked"`
	PasswordHash string            `json:"-"`
	Status       string            `json:"status"` // waiting, in_match
	IsTutorial   bool              `json:"isTutorial"`
	LobbyMapping map[string]string `json:"-"` // playerID → lobbyID for return-to-lobby
	HasBot       bool              `json:"hasBot"`
}

type CreateRoomPayload struct {
	Mode     string  `json:"mode"`
	MapID    string  `json:"mapId"`
	Password *string `json:"password,omitempty"`
}

// QuickPlayPayload selects the tutorial opponent and the player's loadout.
// Bot "ai" plays with the bot AI; absent, empty, "idle" or any other value gets
// the idle target bot. CharacterID defaults to "rookie" and Items to none.
type QuickPlayPayload struct {
	Bot         string   `json:"bot,omitempty"`
	CharacterID string   `json:"characterId,omitempty"`
	Items       []string `json:"items"`
}

// AIBot reports whether the quick-play bot should play with the AI.
func (p QuickPlayPayload) AIBot() bool {
	return p.Bot == "ai"
}

// parseQuickPlayPayload reads an optional QuickPlay payload; missing or
// malformed fields fall back to the idle bot, rookie and no items.
func parseQuickPlayPayload(data []byte) QuickPlayPayload {
	var p QuickPlayPayload
	if len(data) > 0 {
		_ = json.Unmarshal(data, &p)
	}
	if !p.AIBot() {
		p.Bot = "idle"
	}
	if p.CharacterID == "" {
		p.CharacterID = "rookie"
	}
	if p.Items == nil {
		p.Items = []string{}
	}
	return p
}

// LoadoutError is a character or item selection the player is not allowed to
// bring. Other errors from loadout checks are infrastructure failures.
type LoadoutError struct {
	PlayerID string // set when the check covers several players
	Reason   string
}

func (e *LoadoutError) Error() string {
	if e.PlayerID != "" {
		return "player " + e.PlayerID + ": " + e.Reason
	}
	return e.Reason
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

// MatchFound is sent to every human of a ranked match (same payload) before the
// match starts: the room and match ids, map, mode and both teams.
type MatchFound struct {
	RoomID  string           `json:"roomId"`
	MatchID string           `json:"matchId"`
	MapID   string           `json:"mapId"`
	Mode    string           `json:"mode"`
	Teams   []MatchFoundTeam `json:"teams"`
}

// MatchFoundTeam is one side of a MatchFound, humans first, then bots.
type MatchFoundTeam struct {
	TeamID  int                `json:"teamId"`
	Players []MatchFoundPlayer `json:"players"`
}

// MatchFoundPlayer is a player card of the MatchFound reveal. Level is the
// account level; a bot has the humans' average level and counts as their
// average rating; TitleID "" = no title equipped.
type MatchFoundPlayer struct {
	PlayerID    string `json:"playerId"`
	DisplayName string `json:"displayName"`
	CharacterID string `json:"characterId"`
	IsBot       bool   `json:"isBot"`
	Level       int    `json:"level"`
	Rating      int    `json:"rating"`
	Tier        string `json:"tier"`
	Division    int    `json:"division"`
	TitleID     string `json:"titleId"`
}
