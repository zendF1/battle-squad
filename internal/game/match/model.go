package match

import "time"

type Vector2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type StatusEffect struct {
	EffectID       string  `json:"effectId"` // freeze, net, burn, root, stun, wind_stop, shot modifiers (power_shot...)
	TargetPlayerID string  `json:"targetPlayerId"`
	DurationTurn   int     `json:"durationTurn"`
	Value          float64 `json:"value"`
	SourcePlayerID string  `json:"sourcePlayerId"`
}

// TitleInfo is the title a player wears into a match (config_titles); clients draw the plate.
type TitleInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Rarity string `json:"rarity"`
}

type BattlePlayerState struct {
	PlayerID           string     `json:"playerId"`
	DisplayName        string     `json:"displayName"`
	TeamID             int        `json:"teamId"`
	CharacterID        string     `json:"characterId"`
	WeaponItemID       string     `json:"weaponItemId"`       // equipped weapon's item id, "" = none (bots); clients draw that gun
	WeaponUpgradeLevel int        `json:"weaponUpgradeLevel"` // equipped weapon's upgrade level, 0 = none (bots); clients draw the +14 / +16 back aura
	Title              *TitleInfo `json:"title,omitempty"`    // equipped title (active, owned); omitted for none and bots. Cosmetic
	HP                 int        `json:"hp"`
	MaxHP              int        `json:"maxHp"`
	// Shield absorbs damage before HP (Flora's bloom overflow), at most shieldCapPercent of
	// MaxHP; it lasts until broken or the match ends.
	Shield          int            `json:"shield"`
	Defense         int            `json:"defense"`
	Position        Vector2        `json:"position"`
	MoveEnergy      int            `json:"moveEnergy"`
	MaxMoveEnergy   int            `json:"maxMoveEnergy"` // refilled each turn: the character's base move energy + MoveEnergyBonus
	Items           []string       `json:"items"`         // Consumables remaining
	UsedItems       []string       `json:"-"`             // Consumables spent this match (reservations to consume)
	StatusEffects   []StatusEffect `json:"statusEffects"`
	IsAlive         bool           `json:"isAlive"`
	IsBot           bool           `json:"isBot"`
	IsIdle          bool           `json:"-"` // bot that only ends its turn (tutorial target)
	DamageBonus     int            `json:"damageBonus"`
	CritChance      float64        `json:"critChance"`
	MoveEnergyBonus int            `json:"moveEnergyBonus"`
	// SkillPowerBonus is a percent added to skill damage and healing.
	SkillPowerBonus int `json:"skillPowerBonus"`
	// TerrainDamageBonus is a percent added to the crater radius of this player's explosions.
	TerrainDamageBonus int `json:"terrainDamageBonus"`
	SkillEnergy        int `json:"skillEnergy"` // 0-100, skill usable at 100
	// SkillRank is the equipped weapon's tier rank (WeaponSkillRank: basic 0 .. dragon 4,
	// crafted silver 2 / gold 3 / titan 4 / diamond 4), set by NewMatch; it scales the skill.
	SkillRank   int `json:"skillRank"`
	DamageDealt int `json:"damageDealt"`
	KillCount   int `json:"killCount"`
	ShotsFired  int `json:"shotsFired"`
	ShotsHit    int `json:"shotsHit"`
	// SkillUses counts the skills this player fired (daily missions).
	SkillUses int `json:"-"`
	// ShotTurns counts this player's resolved actions (a shot, a skill or a teleport: one per
	// turn); Left is set when the player left the match. Both decide whether the match counts
	// for milestones (see milestoneCounts).
	ShotTurns int  `json:"-"`
	Left      bool `json:"-"`

	// Boss mode (omitted elsewhere): Role is "boss" or "minion" for the boss's side ("" for a
	// player or bot teammate), BossID the boss of the match, MinionID the minion type
	// (config_bosses.minions). BodySize scales the body (hit radius and centre height; 0 or
	// absent = 1, a player). BossDamage / MinionDamage: the damage this player dealt to the boss /
	// to minions (the rewards and the damage ranking).
	Role         string  `json:"role,omitempty"`
	BossID       string  `json:"bossId,omitempty"`
	MinionID     string  `json:"minionId,omitempty"`
	BodySize     float64 `json:"bodySize,omitempty"`
	BossDamage   int     `json:"bossDamage,omitempty"`
	MinionDamage int     `json:"minionDamage,omitempty"`
	// Boss v2 (boss and minions only): Flying units never fall (craters and falls skip them;
	// they move with UnitFlew); Behavior (shooter / bomber) and Movement (none / walk / fly) of
	// a minion (its config, defaults filled in).
	Flying   bool   `json:"flying,omitempty"`
	Behavior string `json:"behavior,omitempty"`
	Movement string `json:"movement,omitempty"`
	// Boss v3: Leapt is true on a leaper from its leap (UnitJumped) until the end of its next
	// turn, when it detonates if a party member is still within detonateRange; LeapSeq is the
	// turn it leapt on.
	Leapt   bool `json:"leapt,omitempty"`
	LeapSeq int  `json:"-"`
}

// The roles of the boss's side (BattlePlayerState.Role).
const (
	RoleBoss   = "boss"
	RoleMinion = "minion"
)

type WindState struct {
	Direction int     `json:"direction"` // -1: left, 0: no wind, 1: right
	Power     float64 `json:"power"`     // 0.0 to 4.0
}

type MatchState struct {
	MatchID         string                        `json:"matchId"`
	RoomID          string                        `json:"roomId"`
	Mode            string                        `json:"mode"`
	MapID           string                        `json:"mapId"`
	MapName         string                        `json:"mapName"`  // the map's Vietnamese name (config_maps.name)
	MapTheme        string                        `json:"mapTheme"` // how the client draws it (gamedata.MapThemes)
	TurnIndex       int                           `json:"turnIndex"`
	CurrentPlayerID string                        `json:"currentPlayerId"`
	Wind            WindState                     `json:"wind"`
	Players         map[string]*BattlePlayerState `json:"players"`
	Status          string                        `json:"status"` // in_progress, ended, recovering
	TurnOrder       []string                      `json:"turnOrder"`
	TurnTimeLeft    int                           `json:"turnTimeLeft"`  // in seconds
	ActiveEffects   []StatusEffect                `json:"activeEffects"` // global active effects (like WindStop)

	// Boss mode only (omitted elsewhere): the boss fight's state, whether the boss is shielded
	// (any minion alive) and the lava pools its skills left.
	Boss         *BossState `json:"boss,omitempty"`
	BossShielded *bool      `json:"bossShielded,omitempty"`
	LavaZones    []LavaZone `json:"lavaZones,omitempty"`
}

// BossState is MatchState.boss: the boss, its player id, how many of its turns have started
// (Round, 0 before its first) and after how many it escapes (TurnLimit).
type BossState struct {
	BossID    string `json:"bossId"`
	PlayerID  string `json:"playerId"`
	Round     int    `json:"round"`
	TurnLimit int    `json:"turnLimit"`
}

// LavaZone is a lava pool a boss skill left: a player standing within Radius px of Position.x
// (and near its height) at the start of its turn loses Damage HP (no defense, shield first);
// it is gone after the boss's turn number ExpiresRound.
type LavaZone struct {
	ZoneID       string  `json:"zoneId"`
	Position     Vector2 `json:"position"`
	Radius       float64 `json:"radius"`
	Damage       int     `json:"damage"`
	ExpiresRound int     `json:"expiresRound"`
}

type ShootAction struct {
	Angle           float64  `json:"angle"`
	Power           float64  `json:"power"`
	ActionMode      string   `json:"actionMode"` // weapon, skill, item
	ItemID          *string  `json:"itemId,omitempty"`
	TargetX         *float64 `json:"targetX,omitempty"` // used by air_strike to specify drop column
	ClientTimestamp int64    `json:"clientTimestamp"`
}

type MoveAction struct {
	Direction       string  `json:"direction"` // left, right
	TargetX         float64 `json:"targetX"`
	Seq             int     `json:"seq,omitempty"` // client command number, echoed in PlayerMoved
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
	ExplosionRadius float64          `json:"explosionRadius"` // blast radius: damage falls off to 0 at it
	// CraterRadius is the crater the explosion carves before the shooter's terrain damage
	// bonus (the explode event's radius includes it): weaponCraterRadius, scaled by skills
	// and fragile terrain.
	CraterRadius float64 `json:"craterRadius"`
	// Kind is what a skill projectile is: flare (missile rain signal, lands without exploding),
	// missile (falls from the top of the map), bomb (bomb chain), seed (bloom, lands on an ally
	// without exploding); "" for a normal shot. Boss mode: a boss rain's projectileKind
	// (steel_shell, magma_bomb, ice_spike, ice_shard; missile by default) and a minion's shots'
	// (drone_bolt, crab_spit, snowball).
	Kind string `json:"kind,omitempty"`
	// LaunchDelay is when the projectile leaves (s since the shot), the time of its first path
	// sample: path times are always seconds since the shot was fired.
	LaunchDelay float64 `json:"launchDelay,omitempty"`
}

// ShotEvent is one entry of a ShotResolved timeline. T is seconds since the shot was fired;
// fields not relevant to Type are omitted.
type ShotEvent struct {
	T              float64  `json:"t"`
	Type           string   `json:"type"` // explode, damage, heal, shield, status, cleanse, fall, die, teleport, flare, rain, beam, summon, lava
	PlayerID       string   `json:"playerId,omitempty"`
	ProjectileID   string   `json:"projectileId,omitempty"`
	Position       *Vector2 `json:"position,omitempty"`       // explode, flare, rain (the flare point)
	Radius         float64  `json:"radius,omitempty"`         // explode; rain: half the spread of the drop columns
	Crater         string   `json:"crater,omitempty"`         // explode: the crater's shape, always bowl (Terrain.CarveBowl; circle: Terrain.DestroyCircle, no longer sent)
	TerrainVersion uint64   `json:"terrainVersion,omitempty"` // explode: terrain version after the crater
	Amount         int      `json:"amount,omitempty"`         // damage (HP + shield lost), heal (HP gained), shield (shield gained)
	HP             *int     `json:"hp,omitempty"`             // damage, heal: HP after the change
	Shield         *int     `json:"shield,omitempty"`         // damage (when the player had a shield), shield: shield after the change
	Critical       bool     `json:"critical,omitempty"`       // damage
	Cause          string   `json:"cause,omitempty"`          // damage, die: explosion, laser, fall, out_of_map
	EffectID       string   `json:"effectId,omitempty"`       // status
	EffectIDs      []string `json:"effectIds,omitempty"`      // cleanse: the effects removed
	Turns          int      `json:"turns,omitempty"`          // status
	Count          int      `json:"count,omitempty"`          // rain: missiles falling
	From           *Vector2 `json:"from,omitempty"`           // fall, teleport, beam
	To             *Vector2 `json:"to,omitempty"`             // fall, teleport, beam
	Duration       float64  `json:"duration,omitempty"`       // fall: seconds until landing; beam: seconds the beam shows
	MinionID       string   `json:"minionId,omitempty"`       // summon: the minion type
	ZoneID         string   `json:"zoneId,omitempty"`         // lava: the pool (MatchState.lavaZones)
	// explode of a minion's detonation / death blast: the blast's damage radius (radius stays
	// the crater, as in every explode; playerId is the minion that blew up)
	BlastRadius float64 `json:"blastRadius,omitempty"`
	// summon: the new unit as MatchStateSync shows it
	MaxHP       *int    `json:"maxHp,omitempty"`
	DisplayName string  `json:"displayName,omitempty"`
	TeamID      int     `json:"teamId,omitempty"`
	BodySize    float64 `json:"bodySize,omitempty"`
	Flying      *bool   `json:"flying,omitempty"`
	Behavior    string  `json:"behavior,omitempty"`
	Movement    string  `json:"movement,omitempty"`
}

// ShotResolved is the single message describing a whole shot: every projectile's flight plus
// the time-ordered effects. The server state already reflects the outcome; clients replay it.
type ShotResolved struct {
	ShooterID   string `json:"shooterId"`
	ActionMode  string `json:"actionMode"` // weapon, skill, item; boss mode: self_destruct, death_blast, summon
	SkillID     string `json:"skillId,omitempty"`
	ItemID      string `json:"itemId,omitempty"`
	SkillEnergy int    `json:"skillEnergy"` // shooter's skill energy after the shot
	// SkillEnergies is every player's skill energy after the shot (players hit by an enemy
	// gain skillEnergyOnHit once per shot).
	SkillEnergies map[string]int      `json:"skillEnergies"`
	Projectiles   []*ProjectileResult `json:"projectiles"`
	Events        []ShotEvent         `json:"events"`
	Duration      float64             `json:"duration"` // seconds until the last flight or event ends
}

// PlayerAiming is broadcast while a player aims, before its shot. Angle uses the
// Shoot action convention and is the angle the shot will be fired with; the
// ShotResolved follows about DurationMs later. Sent for bots only for now.
type PlayerAiming struct {
	PlayerID   string  `json:"playerId"`
	Angle      float64 `json:"angle"`
	DurationMs int     `json:"durationMs"`
}

type MatchHistoryRecord struct {
	MatchID    string    `json:"matchId"`
	PlayerID   string    `json:"playerId"`
	Mode       string    `json:"mode"`
	MapID      string    `json:"mapId"`
	Result     string    `json:"result"` // win, loss, draw, no_contest
	Damage     int       `json:"damage"`
	Kills      int       `json:"kills"`
	Accuracy   float64   `json:"accuracy"`
	EXPGained  int       `json:"expGained"`
	CoinGained int       `json:"coinGained"`
	PlayedAt   time.Time `json:"playedAt"`
}
