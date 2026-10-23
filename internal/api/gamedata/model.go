// Package gamedata serves GET /gamedata: every gameplay rule, number and display text the
// client needs, so it hardcodes none of them (docs/gamedata-contract.md is the contract).
package gamedata

// GameData is GET /gamedata. Version is a hash of everything else (also the ETag): a client
// with it cached sends If-None-Match and gets 304 while nothing changed.
// Units: px, seconds (…Sec), percent (…Percent / …Pct, 0..100), ratios (0..1) as noted.
type GameData struct {
	Version            string         `json:"version"`
	Physics            Physics        `json:"physics"`
	Aim                Aim            `json:"aim"`
	AimAid             AimAid         `json:"aimAid"`
	Combat             Combat         `json:"combat"`
	Terrain            Terrain        `json:"terrain"`
	Movement           Movement       `json:"movement"`
	Timings            Timings        `json:"timings"`
	SkillEnergy        SkillEnergy    `json:"skillEnergy"`
	SkillRanks         SkillRanks     `json:"skillRanks"`
	Weapons            []Weapon       `json:"weapons"`
	Skills             []Skill        `json:"skills"`
	Characters         []Character    `json:"characters"`
	DefaultCharacterID string         `json:"defaultCharacterId"`
	Items              []Item         `json:"items"`
	ItemRules          ItemRules      `json:"itemRules"`
	StatusEffects      []StatusEffect `json:"statusEffects"`
	Rank               Rank           `json:"rank"`
	Levels             Levels         `json:"levels"`
	Equipment          Equipment      `json:"equipment"`
	Economy            Economy        `json:"economy"`
	Matchmaking        Matchmaking    `json:"matchmaking"`
	Modes              Modes          `json:"modes"`
	MapThemes          MapThemes      `json:"mapThemes"`
	// GunKinds (added in S2): the Vietnamese name of each gun kind (characters[].gunKind).
	GunKinds []GunKind `json:"gunKinds"`
	// Bosses (boss mode, migration 046): the enabled bosses in display order.
	Bosses []Boss `json:"bosses"`
}

// Boss is an enabled config_bosses row as the client draws and describes it: ModeID is the
// lobby mode (modes.lobby) whose queue Queue (boss:<bossId>) fights it, MapID its arena; HP and
// BodySize (× a player's body: hit radius physics.playerHitRadius × it around
// physics.shotOriginLift × it above the feet) of the boss and of each minion type; TurnLimit
// boss turns before it escapes; ShieldedDamageFactor the damage it takes while a minion lives;
// DailyBonusRuns the runs a day with the bonus reward table. Flying: the boss floats (never
// falls); Hover the band it flies to a new point of at the start of each of its turns (null:
// none). Summon (boss v3): its free summon at the start of its turns (null: none); Skills are
// its attacks only (summoning is not a skill).
type Boss struct {
	BossID               string       `json:"bossId"`
	Name                 string       `json:"name"`
	Description          string       `json:"description"`
	RecommendedLevel     int          `json:"recommendedLevel"`
	ModeID               string       `json:"modeId"`
	Queue                string       `json:"queue"`
	MapID                string       `json:"mapId"`
	HP                   int          `json:"hp"`
	BodySize             float64      `json:"bodySize"`
	TurnLimit            int          `json:"turnLimit"`
	ShieldedDamageFactor float64      `json:"shieldedDamageFactor"`
	Minions              []BossMinion `json:"minions"`
	Skills               []BossSkill  `json:"skills"`
	DailyBonusRuns       int          `json:"dailyBonusRuns"`
	Flying               bool         `json:"flying"`
	Hover                *BossHover   `json:"hover"`
	Summon               *BossSummon  `json:"summon"`
}

// BossSummon is a boss's free summon: Count minions of MinionID at the start of its turn from
// round FirstRound on, when the previous wave is dead and more than CooldownRounds rounds after
// its last summon (a ShotResolved actionMode summon before its attack).
type BossSummon struct {
	MinionID       string `json:"minionId"`
	Count          int    `json:"count"`
	CooldownRounds int    `json:"cooldownRounds"`
	FirstRound     int    `json:"firstRound"`
}

// BossHover is the band a flying boss hovers in (px, its feet): at least MinMove px from where it
// was, at FlySpeed px/s (UnitFlew).
type BossHover struct {
	MinX     float64 `json:"minX"`
	MaxX     float64 `json:"maxX"`
	MinY     float64 `json:"minY"`
	MaxY     float64 `json:"maxY"`
	MinMove  float64 `json:"minMove"`
	FlySpeed float64 `json:"flySpeed"`
}

// BossMinion is a minion type of a boss, its defaults filled in: Behavior shooter / bomber,
// Movement none / walk / fly, a bomber detonates when it ends a move within DetonateRange px of
// a party member; it walks at most WalkRange px and flies at most FlyRange px a turn (HoverHeight
// px over the ground, FlySpeed px/s); its blast (a detonation, or its death with ExplodeOnDeath)
// deals ExplodeDamagePct % of the max HP of each one hit (ExplodeDamageOf target) or of the minion
// (self) within ExplodeRadius px, half at the edge, and carves ExplodeCrater px; its shots deal
// its damage × ShotDamageFactor.
type BossMinion struct {
	MinionID         string  `json:"minionId"`
	Name             string  `json:"name"`
	HP               int     `json:"hp"`
	BodySize         float64 `json:"bodySize"`
	Behavior         string  `json:"behavior"`
	Movement         string  `json:"movement"`
	ExplodeOnDeath   bool    `json:"explodeOnDeath"`
	ExplodeRadius    float64 `json:"explodeRadius"`
	ExplodeDamagePct float64 `json:"explodeDamagePct"`
	ExplodeDamageOf  string  `json:"explodeDamageOf"`
	ExplodeCrater    float64 `json:"explodeCrater"`
	DetonateRange    float64 `json:"detonateRange"`
	ShotDamageFactor float64 `json:"shotDamageFactor"`
	FlyRange         float64 `json:"flyRange"`
	WalkRange        float64 `json:"walkRange"`
	HoverHeight      float64 `json:"hoverHeight"`
	FlySpeed         float64 `json:"flySpeed"`
	// leaper (boss v3): LeapRange px per leap (0: the whole map), LeapDetonateNextTurn, ShootAfterLeap
	LeapRange            float64 `json:"leapRange"`
	LeapDetonateNextTurn bool    `json:"leapDetonateNextTurn"`
	ShootAfterLeap       bool    `json:"shootAfterLeap"`
	// ProjectileKind is ShotResolved projectiles[].kind of its shots ("": a normal shot's)
	ProjectileKind string `json:"projectileKind"`
}

// BossSkill is a boss attack as the client names it (ShotResolved.skillId of the boss's turn);
// Kind is rain, beam or shockwave; Pattern of a beam "" (aimed), radial or sweep; a rain's
// ProjectileKind is its shells' projectiles[].kind (steel_shell, magma_bomb, ice_spike,
// ice_shard; missile by default) and LaunchFrom "" (they fall from the top of the map) or boss
// (they leave a ring around the boss's body, then fall).
type BossSkill struct {
	SkillID        string `json:"skillId"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Kind           string `json:"kind"`
	Pattern        string `json:"pattern,omitempty"`
	ProjectileKind string `json:"projectileKind,omitempty"`
	LaunchFrom     string `json:"launchFrom,omitempty"`
}

// GunKind is a config_gun_kinds row: a gun art kind and its Vietnamese name ("Đại pháo").
type GunKind struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Physics is the shell flight (SimulateProjectile): launch speed power × SpeedPerPower px/s,
// gravity px/s² down (y grows down), wind W × WindScale × weapon windInfluence /
// projectileWeight px/s², stepped every TimeStep s for at most MaxFlightSec s.
type Physics struct {
	Gravity         float64     `json:"gravity"`
	SpeedPerPower   float64     `json:"speedPerPower"`
	WindScale       float64     `json:"windScale"`
	TimeStep        float64     `json:"timeStep"`
	PathRecordStep  float64     `json:"pathRecordStep"`
	MaxFlightSec    float64     `json:"maxFlightSec"`
	PlayerHitRadius float64     `json:"playerHitRadius"`
	CollisionStepPx float64     `json:"collisionStepPx"`
	ShotOriginLift  float64     `json:"shotOriginLift"`
	OutOfBounds     OutOfBounds `json:"outOfBounds"`
}

// OutOfBounds: a flight ends at x < −Left, x > map width + Right or y > map height + Below.
type OutOfBounds struct {
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
	Below float64 `json:"below"`
}

// Aim is what a Shoot may carry: power PowerMin..PowerMax (the server rejects more), angle
// AngleMin..AngleMax degrees (the client's slingshot range; the server takes any finite angle);
// a skill without aim is sent with NoAimAngle / NoAimPower.
type Aim struct {
	PowerMin   float64 `json:"powerMin"`
	PowerMax   float64 `json:"powerMax"`
	AngleMin   float64 `json:"angleMin"`
	AngleMax   float64 `json:"angleMax"`
	NoAimAngle float64 `json:"noAimAngle"`
	NoAimPower float64 `json:"noAimPower"`
}

// AimAid is the client's aiming guide: the guide arc's length along the flight (px) by the
// local player's rank tier (DefaultTier when unranked or unknown), and the "hit" tolerance
// r = Max × min(1, MinPart + (1 − MinPart) × max(D / FarPx, H / HighPx)) px.
type AimAid struct {
	GuideArcLength map[string]float64 `json:"guideArcLength"`
	DefaultTier    string             `json:"defaultTier"`
	HitTolerance   HitTolerance       `json:"hitTolerance"`
}

type HitTolerance struct {
	Max     float64 `json:"max"`
	MinPart float64 `json:"minPart"`
	FarPx   float64 `json:"farPx"`
	HighPx  float64 `json:"highPx"`
}

// Combat: damage taken = raw × DefenseK / (DefenseK + defense); power_shot multiplies a shot's
// (or skill's) damage by PowerShotFactor; a player starting its turn on lava loses LavaDamage HP;
// a critical hit deals CritMultiplier × the damage (added in S2).
type Combat struct {
	DefenseK        float64 `json:"defenseK"`
	PowerShotFactor float64 `json:"powerShotFactor"`
	LavaDamage      int     `json:"lavaDamage"`
	CritMultiplier  float64 `json:"critMultiplier"`
}

// Terrain: craters, walking and falls (match/terrain.go, damage.go).
type Terrain struct {
	PlayerHeight                 int     `json:"playerHeight"`
	MaxClimbPerStep              int     `json:"maxClimbPerStep"`
	CraterPxPerTerrainDamage     float64 `json:"craterPxPerTerrainDamage"`
	BowlDepth                    float64 `json:"bowlDepth"`
	BowlCeiling                  float64 `json:"bowlCeiling"`
	MaxTerrainDamageBonusPercent int     `json:"maxTerrainDamageBonusPercent"`
	FallDamageThresholdPx        float64 `json:"fallDamageThresholdPx"`
	FallDamagePerPx              float64 `json:"fallDamagePerPx"`
}

// Movement: a walk reaches at most moveEnergy × PxPerEnergy px and costs round(px ×
// EnergyPerPx) energy; DefaultMoveEnergy is a character's base when unconfigured; net caps the
// move energy at NetMoveEnergyCap.
type Movement struct {
	PxPerEnergy       float64 `json:"pxPerEnergy"`
	EnergyPerPx       float64 `json:"energyPerPx"`
	DefaultMoveEnergy int     `json:"defaultMoveEnergy"`
	NetMoveEnergyCap  int     `json:"netMoveEnergyCap"`
}

type Timings struct {
	TurnSec             int     `json:"turnSec"`
	ActionIntervalSec   float64 `json:"actionIntervalSec"`
	BotFollowUpDelaySec float64 `json:"botFollowUpDelaySec"`
	SkipTurnDelaySec    float64 `json:"skipTurnDelaySec"`
}

// SkillEnergy: a skill needs Max energy; +PerTurn at the end of each own turn, +OnHit once
// per enemy action that takes HP or shield.
type SkillEnergy struct {
	Max     int `json:"max"`
	PerTurn int `json:"perTurn"`
	OnHit   int `json:"onHit"`
}

// SkillRanks: a player's skill rank is ByTierWord[the part after the last "_" of its
// equipped weapon item id], Default (0) for no weapon or an unknown id; at most Max.
type SkillRanks struct {
	ByTierWord map[string]int `json:"byTierWord"`
	Default    int            `json:"default"`
	Max        int            `json:"max"`
}

// Weapon is a config_weapons row; CraterRadius is the crater (px) a normal shot carves on
// normal ground (terrainDamage × craterPxPerTerrainDamage, or explosionRadius without
// terrain damage).
type Weapon struct {
	WeaponID         string  `json:"weaponId"`
	Name             string  `json:"name"`
	Damage           int     `json:"damage"`
	ExplosionRadius  int     `json:"explosionRadius"`
	TerrainDamage    int     `json:"terrainDamage"`
	CraterRadius     float64 `json:"craterRadius"`
	ProjectileWeight float64 `json:"projectileWeight"`
	WindInfluence    float64 `json:"windInfluence"`
	MultiHit         int     `json:"multiHit"`
}

// Skill is a config_skills row with its rules: Description is a template whose {key}
// placeholders are Ranks[rank].Values[key]; Params are the effect type's tuning numbers.
type Skill struct {
	SkillID          string             `json:"skillId"`
	CharacterID      string             `json:"characterId"`
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	EffectType       string             `json:"effectType"`
	StatusEffectID   string             `json:"statusEffectId"`
	AimKind          string             `json:"aimKind"`
	CooldownTurn     int                `json:"cooldownTurn"`
	ProjectileCount  int                `json:"projectileCount"`
	DamageMultiplier float64            `json:"damageMultiplier"`
	Params           map[string]float64 `json:"params"`
	Ranks            []SkillRank        `json:"ranks"`
}

type SkillRank struct {
	Rank   int                `json:"rank"`
	Values map[string]float64 `json:"values"`
}

// Character is a config_characters row (base stats, its weapon and skill, display texts).
type Character struct {
	CharacterID   string `json:"characterId"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	RoleName      string `json:"roleName"`
	Playstyle     string `json:"playstyle"`
	WeaponID      string `json:"weaponId"`
	SkillID       string `json:"skillId"`
	GunKind       string `json:"gunKind"`
	HP            int    `json:"hp"`
	Damage        int    `json:"damage"`
	Mobility      int    `json:"mobility"`
	Defense       int    `json:"defense"`
	SkillPower    int    `json:"skillPower"`
	TerrainDamage int    `json:"terrainDamage"`
	Difficulty    int    `json:"difficulty"`
}

// Item is a config_items row. Instant items are used with UseItem and act at once; the others
// arm the next shot (Shoot actionMode "item"), aimed as AimKind says.
type Item struct {
	ItemID         string  `json:"itemId"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	TargetType     string  `json:"targetType"`
	Value          float64 `json:"value"`
	MaxUsePerMatch int     `json:"maxUsePerMatch"`
	Cooldown       int     `json:"cooldown"`
	Instant        bool    `json:"instant"`
	AimKind        string  `json:"aimKind"`
}

// Item aim kinds: none (instant), the slingshot arc, a map column (Shoot.targetX).
const (
	ItemAimNone    = "none"
	ItemAimArc     = "arc"
	ItemAimTargetX = "targetX"
)

type ItemRules struct {
	MaxLoadout int       `json:"maxLoadout"`
	AirStrike  AirStrike `json:"airStrike"`
}

// AirStrike: the strike falls from (targetX, 0) at Angle / Power whatever the Shoot carries
// (it still sends them, validated).
type AirStrike struct {
	Angle float64 `json:"angle"`
	Power float64 `json:"power"`
}

// StatusEffect: Name labels it when it lands, Marker shows above the player while it is
// active, Banner while it skips the turn or blocks moving ("" none).
type StatusEffect struct {
	EffectID   string `json:"effectId"`
	Name       string `json:"name"`
	Marker     string `json:"marker"`
	Banner     string `json:"banner"`
	Bad        bool   `json:"bad"`
	SkipsTurn  bool   `json:"skipsTurn"`
	BlocksMove bool   `json:"blocksMove"`
}

type Rank struct {
	Tiers         []RankTier `json:"tiers"`
	DefaultRating int        `json:"defaultRating"`
	RankedMode    string     `json:"rankedMode"`
}

// RankTier: ratings from MinRating up to the next tier's; division = clamp(Divisions −
// floor((rating − MinRating) / DivisionSize), 1, Divisions) (1 when DivisionSize is 0).
type RankTier struct {
	Tier         string `json:"tier"`
	Name         string `json:"name"`
	MinRating    int    `json:"minRating"`
	Divisions    int    `json:"divisions"`
	DivisionSize int    `json:"divisionSize"`
}

type Levels struct {
	Character CharacterLevels `json:"character"`
	Player    PlayerLevels    `json:"player"`
}

// CharacterLevels: Levels[i].ExpRequired is the total EXP a character needs for Levels[i].Level.
type CharacterLevels struct {
	Levels   []LevelEntry `json:"levels"`
	MaxLevel int          `json:"maxLevel"`
}

type LevelEntry struct {
	Level       int `json:"level"`
	ExpRequired int `json:"expRequired"`
}

// PlayerLevels: a player at level N needs N × ExpPerLevel EXP (counted from 0 at N) for N + 1.
type PlayerLevels struct {
	ExpPerLevel int `json:"expPerLevel"`
}

// Equipment: a piece's stat multiplier is 1 + level × UpgradeStepPerLevel + every reached
// milestone's bonus.
type Equipment struct {
	UpgradeStepPerLevel         float64         `json:"upgradeStepPerLevel"`
	MaxUpgradeLevel             int             `json:"maxUpgradeLevel"`
	Milestones                  []Milestone     `json:"milestones"`
	MaxStoneLevel               int             `json:"maxStoneLevel"`
	MergeMinCount               int             `json:"mergeMinCount"`
	MergeMaxCount               int             `json:"mergeMaxCount"`
	MergeRatePerItem            float64         `json:"mergeRatePerItem"`
	SellRate                    float64         `json:"sellRate"`
	WeaponUpgradeCostMultiplier float64         `json:"weaponUpgradeCostMultiplier"`
	Tiers                       []EquipmentTier `json:"tiers"`
	// Slots and TypeWords (added in S2): the slot names in display order, and the Vietnamese
	// words of the type part of "<Tier> <Type>" equipment item names.
	Slots     []EquipmentSlot `json:"slots"`
	TypeWords []TypeWord      `json:"typeWords"`
}

// EquipmentSlot is a config_equipment_slots row: a slot id and its Vietnamese name ("Găng tay").
type EquipmentSlot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TypeWord is a config_item_type_words row: the lower-case type part of a "<Tier> <Type>"
// catalog name ("shock rifle"), its Vietnamese word ("Súng điện") and, for a gun, its gun kind
// ("shock_rifle"; "" for armour).
type TypeWord struct {
	Word    string `json:"word"`
	Name    string `json:"name"`
	GunKind string `json:"gunKind"`
}

type Milestone struct {
	Level int     `json:"level"`
	Bonus float64 `json:"bonus"`
}

// EquipmentTier: a normal tier's items end in IDSuffix (lv10 …), a crafted tier's in the tier
// itself; SkillRank is the rank a weapon of the tier gives.
type EquipmentTier struct {
	Tier      string `json:"tier"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	IDSuffix  string `json:"idSuffix"`
	SkillRank int    `json:"skillRank"`
}

type Economy struct {
	CoinsPerGem int `json:"coinsPerGem"`
}

type Matchmaking struct {
	Queue              string   `json:"queue"`
	Slots              int      `json:"slots"`
	TeamSize           int      `json:"teamSize"`
	BotFillAfterSec    int      `json:"botFillAfterSec"`
	MatchStartDelaySec int      `json:"matchStartDelaySec"`
	InviteTimeoutSec   int      `json:"inviteTimeoutSec"`
	Tips               []string `json:"tips"`
}

type Modes struct {
	Categories []ModeCategory `json:"categories"`
	Lobby      []LobbyMode    `json:"lobby"`
	Match      []MatchMode    `json:"match"`
}

// ModeCategory is a section of the mode select screen (PvP, Boss, …), in order.
type ModeCategory struct {
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
}

// LobbyMode is an enabled mode choice of the mode select screen: Queue joins that matchmaking
// queue, Bot plays QuickPlay against that bot, neither is practice. Category is a
// ModeCategory id, Icon a client icon key ("" = the generic one), PartySize the most lobby
// members (2: the teammate slot), Ranked whether its matches change rating.
type LobbyMode struct {
	ModeID      string `json:"modeId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Queue       string `json:"queue"`
	Bot         string `json:"bot"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
	PartySize   int    `json:"partySize"`
	Ranked      bool   `json:"ranked"`
	// MaxItems (052) is how many battle items a player brings into its matches: the mode's
	// config_game_modes max_items, itemRules.maxLoadout when NULL (6 for the boss modes).
	MaxItems int `json:"maxItems"`
}

// MatchMode is a match's mode (MatchStarted.mode, match history).
type MatchMode struct {
	ModeID string `json:"modeId"`
	Name   string `json:"name"`
}

type MapThemes struct {
	Default string   `json:"default"`
	Themes  []string `json:"themes"`
}
