package match

import "time"

type Vector2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type StatusEffect struct {
	EffectID       string `json:"effectId"` // freeze, net, heal, wind_stop
	TargetPlayerID string `json:"targetPlayerId"`
	DurationTurn   int    `json:"durationTurn"`
	Value          float64 `json:"value"`
	SourcePlayerID string `json:"sourcePlayerId"`
}

type BattlePlayerState struct {
	PlayerID      string         `json:"playerId"`
	DisplayName   string         `json:"displayName"`
	TeamID        int            `json:"teamId"`
	CharacterID   string         `json:"characterId"`
	HP            int            `json:"hp"`
	MaxHP         int            `json:"maxHp"`
	Defense       int            `json:"defense"`
	Position      Vector2        `json:"position"`
	MoveEnergy    int            `json:"moveEnergy"`
	Items         []string       `json:"items"` // Consumables remaining
	StatusEffects []StatusEffect `json:"statusEffects"`
	IsAlive       bool           `json:"isAlive"`
	IsBot         bool           `json:"isBot"`
	SkillCooldown int            `json:"skillCooldown"`
	DamageDealt   int            `json:"damageDealt"`
	KillCount     int            `json:"killCount"`
	ShotsFired    int            `json:"shotsFired"`
	ShotsHit      int            `json:"shotsHit"`
}

type WindState struct {
	Direction int `json:"direction"` // -1: left, 0: no wind, 1: right
	Power     int `json:"power"`     // 0 to 4
}

type MatchState struct {
	MatchID         string                       `json:"matchId"`
	RoomID          string                       `json:"roomId"`
	Mode            string                       `json:"mode"`
	MapID           string                       `json:"mapId"`
	TurnIndex       int                          `json:"turnIndex"`
	CurrentPlayerID string                       `json:"currentPlayerId"`
	Wind            WindState                    `json:"wind"`
	Players         map[string]*BattlePlayerState `json:"players"`
	Status          string                       `json:"status"` // in_progress, ended, recovering
	TurnOrder       []string                     `json:"turnOrder"`
	TurnTimeLeft    int                          `json:"turnTimeLeft"` // in seconds
	ActiveEffects   []StatusEffect               `json:"activeEffects"` // global active effects (like WindStop)
}

type ShootAction struct {
	Angle           float64  `json:"angle"`
	Power           float64  `json:"power"`
	ActionMode      string   `json:"actionMode"` // weapon, skill, item
	ItemID          *string  `json:"itemId,omitempty"`
	TargetX         float64  `json:"targetX,omitempty"` // used by air_strike to specify drop column
	ClientTimestamp int64    `json:"clientTimestamp"`
}

type MoveAction struct {
	Direction       string  `json:"direction"` // left, right
	TargetX         float64 `json:"targetX"`
	ClientTimestamp int64   `json:"clientTimestamp"`
}

type UseItemAction struct {
	ItemID          string   `json:"itemId"`
	TargetPosition  *Vector2 `json:"targetPosition,omitempty"`
	ClientTimestamp int64    `json:"clientTimestamp"`
}

type ProjectileStep struct {
	Position Vector2 `json:"position"`
	Velocity Vector2 `json:"velocity"`
	Time     float64 `json:"time"`
}

type ProjectileResult struct {
	ProjectileID    string           `json:"projectileId"`
	OwnerPlayerID   string           `json:"ownerPlayerId"`
	SkillID         string           `json:"skillId,omitempty"` // set when fired via skill action
	Path            []ProjectileStep `json:"path"`
	HitPlayerID     string           `json:"hitPlayerId,omitempty"` // empty if missed or hit terrain
	ExplosionPoint  *Vector2         `json:"explosionPoint,omitempty"`
	ExplosionRadius float64          `json:"explosionRadius"`
	TerrainDestroyed bool            `json:"terrainDestroyed"`
}

type MatchHistoryRecord struct {
	MatchID     string    `json:"matchId"`
	PlayerID    string    `json:"playerId"`
	Mode        string    `json:"mode"`
	MapID       string    `json:"mapId"`
	Result      string    `json:"result"` // win, loss, draw, no_contest
	Damage      int       `json:"damage"`
	Kills       int       `json:"kills"`
	Accuracy    float64   `json:"accuracy"`
	EXPGained   int       `json:"expGained"`
	CoinGained  int       `json:"coinGained"`
	PlayedAt    time.Time `json:"playedAt"`
}
