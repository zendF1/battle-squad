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
	Division    int      `json:"division"`
	Level       int      `json:"level"` // account level (player_profiles.level)
	// Ready: the member is ready to queue (SetReady); the host always is. A member
	// joins not ready and stays ready across matches and loadout changes.
	Ready bool `json:"ready"`
	// Gun: the gun CharacterID holds (gunOf), refreshed on UpdateLoadout
	Gun MemberGun `json:"gun"`
}

// MemberGun is the gun art a character holds outside a match, the pair the client's
// EquipmentModel.worn_gun gives: kind a config_gun_kinds id, tier an equipment tier id,
// or "basic" for the character's basic gun (no weapon worn).
type MemberGun struct {
	Kind string `json:"kind"`
	Tier string `json:"tier"`
}

// The statuses of an online player (OnlinePlayer.Status, see LobbyHub.Status).
const (
	StatusIdle    = "idle"
	StatusInLobby = "in_lobby"
	StatusInMatch = "in_match"
)

// OnlinePlayer is a player connected to this game node as the invite list shows it.
type OnlinePlayer struct {
	PlayerID    string `json:"playerId"`
	DisplayName string `json:"displayName"`
	Level       int    `json:"level"`
	Tier        string `json:"tier"`
	Division    int    `json:"division"`
	CharacterID string `json:"characterId"`
	Status      string `json:"status"` // idle, in_lobby, in_match
}

type UpdateLoadoutPayload struct {
	CharacterID *string  `json:"characterId,omitempty"`
	Items       []string `json:"items,omitempty"`
}

// InvitePayload is InviteToLobby {playerId}.
type InvitePayload struct {
	PlayerID string `json:"playerId"`
}

type JoinLobbyPayload struct {
	LobbyID string `json:"lobbyId"`
}

// ListOnlinePlayersPayload is ListOnlinePlayers {query?}.
type ListOnlinePlayersPayload struct {
	Query string `json:"query"`
}

// OnlinePlayersPayload answers ListOnlinePlayers.
type OnlinePlayersPayload struct {
	Players []OnlinePlayer `json:"players"`
}

// RespondInvitePayload is RespondInvite {inviteId, accept}.
type RespondInvitePayload struct {
	InviteID string `json:"inviteId"`
	Accept   bool   `json:"accept"`
}

// KickMemberPayload is KickMember {playerId}.
type KickMemberPayload struct {
	PlayerID string `json:"playerId"`
}

// SetReadyPayload is SetReady {ready}.
type SetReadyPayload struct {
	Ready bool `json:"ready"`
}

// InviteSentPayload tells the inviter its invite went out.
type InviteSentPayload struct {
	PlayerID     string `json:"playerId"`
	ExpiresInSec int    `json:"expiresInSec"`
}

// LobbyInvitePayload is the invite the invitee answers with RespondInvite.
type LobbyInvitePayload struct {
	InviteID     string       `json:"inviteId"`
	LobbyID      string       `json:"lobbyId"`
	Mode         string       `json:"mode"`
	ExpiresInSec int          `json:"expiresInSec"`
	From         OnlinePlayer `json:"from"`
}

// InviteExpiredPayload: {playerId} (the invitee) to the inviter, {inviteId} to the invitee.
type InviteExpiredPayload struct {
	PlayerID string `json:"playerId,omitempty"`
	InviteID string `json:"inviteId,omitempty"`
}

// InviteDeclinedPayload tells the inviter who declined.
type InviteDeclinedPayload struct {
	PlayerID    string `json:"playerId"`
	DisplayName string `json:"displayName"`
}

// LobbyKickedPayload tells a member the host removed them.
type LobbyKickedPayload struct {
	LobbyID string `json:"lobbyId"`
}
