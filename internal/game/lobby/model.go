package lobby

type LobbyState struct {
	LobbyID      string        `json:"lobbyId"`
	HostPlayerID string        `json:"hostPlayerId"`
	Members      []LobbyMember `json:"members"`
	Status       string        `json:"status"` // preparing, in_queue, in_match
	QueueEntryID string        `json:"-"`
}

type LobbyMember struct {
	PlayerID    string   `json:"playerId"`
	DisplayName string   `json:"displayName"`
	CharacterID string   `json:"characterId"`
	Items       []string `json:"items"`
	Rating      int      `json:"rating"`
	Tier        string   `json:"tier"`
}

type UpdateLoadoutPayload struct {
	CharacterID *string  `json:"characterId,omitempty"`
	Items       []string `json:"items,omitempty"`
}

type InvitePayload struct {
	PlayerID string `json:"playerId"`
}

type JoinLobbyPayload struct {
	LobbyID string `json:"lobbyId"`
}
