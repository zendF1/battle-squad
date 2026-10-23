package gamedata

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Boss mode (config_bosses, migration 046): a party of up to BossPartySize players (humans and
// bot teammates, team 1) fights one boss and its minions (team 2). A boss row holds its stats,
// its skills, its minion types and waves, the rewards and the daily bonus runs; each enabled
// boss has a lobby mode whose queue is BossQueue(bossId) and a match mode of the same id.

// BossPartySize is the most players (humans + bot teammates) of a boss match.
const BossPartySize = 4

// BossQueuePrefix + boss id is a boss's matchmaking queue and the mode of its matches.
const BossQueuePrefix = "boss:"

// BossQueue is the queue (and match mode) of boss bossID.
func BossQueue(bossID string) string { return BossQueuePrefix + bossID }

// BossIDOfQueue is the boss of queue q ("", false when q is not a boss queue).
func BossIDOfQueue(q string) (string, bool) {
	if !strings.HasPrefix(q, BossQueuePrefix) || len(q) == len(BossQueuePrefix) {
		return "", false
	}
	return q[len(BossQueuePrefix):], true
}

// Boss skill kinds (BossSkill.Kind).
const (
	// BossSkillRain: Count shells fall straight down from the top of the map around the
	// target (± Spread px), each Damage × the boss damage with a Blast px blast and a Crater
	// px crater, at most Cap × the boss damage per target (0: no cap); Status (Turns) on every
	// enemy damaged; with LavaTurns > 0 each impact leaves a lava pool (LavaRadius px, LavaDamage
	// HP at the start of the turn of a player standing in it) for LavaTurns rounds. Count 1,
	// Spread 0 is a single slam on the target. Target map (boss v3) rains over the whole map
	// (Spread 0: half the map width), target party around the party's mean x.
	BossSkillRain = "rain"
	// BossSkillBeam: straight beams through terrain; every enemy within HalfWidth px of one takes
	// Damage × the boss damage and Status (Turns), once per cast. Pattern "" (aimed): one beam
	// from the boss's body centre through the target's body to the map edge (Decay × the
	// previous damage for each next enemy; 0 = 1). Pattern radial (boss v3): Count beams evenly
	// spaced around the boss's body centre (360° / Count apart, the whole fan turned by a random
	// angle) out to the map edges. Pattern sweep (boss v3): Count horizontal beams across the
	// whole map width at the party members' heights (the rest evenly spaced over their y range).
	BossSkillBeam = "beam"
	// BossSkillShockwave (boss v3): a slam at the boss's feet, then a wave along the ground both
	// ways to the map edges (an explode every Step px, Interval s apart); every grounded enemy
	// takes Damage × the boss damage × (1 − its distance / the farther edge's), at least
	// MinFactor × it, when the wave reaches it. Blast is the slam's blast radius (the clients'
	// effect), Crater the bowl each explode carves.
	BossSkillShockwave = "shockwave"
	// BossSkillSummon (legacy, boss v2): summoning is the boss-level BossSummon now; a summon
	// skill is never cast as an attack (a boss without a summon config summons with its first
	// summon skill's minion, count and cooldown: BossConfig.SummonConfig).
	BossSkillSummon = "summon"
)

// BossSkillKinds are the skill kinds a boss may use.
var BossSkillKinds = []string{BossSkillRain, BossSkillBeam, BossSkillShockwave, BossSkillSummon}

// Beam patterns (BossSkill.Pattern).
const (
	BeamAimed  = ""
	BeamRadial = "radial"
	BeamSweep  = "sweep"
)

// Boss skill targets (BossSkill.Target).
const (
	BossTargetRandom   = "random"    // a random living enemy
	BossTargetNearest  = "nearest"   // the living enemy nearest the boss
	BossTargetLowestHP = "lowest_hp" // the living enemy with the least HP
	BossTargetParty    = "party"     // rain: the mean x of the living enemies
	BossTargetMap      = "map"       // rain: the centre of the map (Spread 0: half the map width)
)

// BossSkill is one skill of a boss (config_bosses.skills, a JSON array).
type BossSkill struct {
	SkillID     string  `json:"skillId"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Kind        string  `json:"kind"`     // rain, beam, shockwave (summon: legacy, never cast)
	Weight      float64 `json:"weight"`   // relative pick chance among the skills off cooldown
	Cooldown    int     `json:"cooldown"` // boss turns before it may be used again (0: next turn)
	Target      string  `json:"target,omitempty"`

	Count       int     `json:"count,omitempty"` // rain: shells; beam radial / sweep: beams
	Spread      float64 `json:"spread,omitempty"`
	Damage      float64 `json:"damage,omitempty"` // × the boss damage
	Blast       float64 `json:"blast,omitempty"`
	Crater      float64 `json:"crater,omitempty"`
	Cap         float64 `json:"cap,omitempty"`
	Decay       float64 `json:"decay,omitempty"`
	HalfWidth   float64 `json:"halfWidth,omitempty"`
	Pattern     string  `json:"pattern,omitempty"` // beam: "" aimed, radial, sweep
	Status      string  `json:"status,omitempty"`
	StatusTurns int     `json:"statusTurns,omitempty"`
	// StatusChance (052) is the chance (0..1) that the status lands on a party member the cast
	// hits (rolled once per member and cast); 0 is DefaultBossStatusChance.
	StatusChance float64 `json:"statusChance,omitempty"`
	LavaRadius   float64 `json:"lavaRadius,omitempty"`
	LavaTurns    int     `json:"lavaTurns,omitempty"`
	LavaDamage   int     `json:"lavaDamage,omitempty"`
	Step         float64 `json:"step,omitempty"`      // shockwave: px between two explodes (0: 150)
	Interval     float64 `json:"interval,omitempty"`  // shockwave: s between two explodes (0: 0.08)
	MinFactor    float64 `json:"minFactor,omitempty"` // shockwave: the least share of the damage far away (0: 0.4)
	MinionID     string  `json:"minionId,omitempty"`
	// ProjectileKind (rain) is ShotResolved projectiles[].kind of its shells, the art the
	// clients draw ("": missile); LaunchFrom (rain) "" drops them from the top of the map, boss
	// launches each from a ring around the boss's body (radius bodySize × RainRingPerBodySize px)
	// on an arc up to its column, from where it falls.
	ProjectileKind string `json:"projectileKind,omitempty"`
	LaunchFrom     string `json:"launchFrom,omitempty"`
}

// Rain launch points (BossSkill.LaunchFrom).
const (
	RainFromTop  = ""
	RainFromBoss = "boss"
	// RainRingPerBodySize × the boss's bodySize is the radius of the ring a boss-launched rain
	// leaves from, around its body centre.
	RainRingPerBodySize = 30.0
	// DefaultRainProjectileKind is a boss rain shell's kind without a projectileKind.
	DefaultRainProjectileKind = "missile"
)

// validKind reports whether k is a projectile kind ("" or up to 32 of a-z, 0-9, _).
func validKind(k string) bool {
	if len(k) > 32 {
		return false
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// DefaultBossStatusChance is a boss skill's status chance without a statusChance (052: a hit
// party member gets the status half of the time).
const DefaultBossStatusChance = 0.5

// StatusChanceOrDefault is the chance the skill's status lands on a member it hits.
func (s BossSkill) StatusChanceOrDefault() float64 {
	if s.StatusChance > 0 {
		return s.StatusChance
	}
	return DefaultBossStatusChance
}

// Shockwave defaults (zero fields of a shockwave skill).
const (
	DefaultShockwaveStep      = 150.0
	DefaultShockwaveInterval  = 0.08
	DefaultShockwaveMinFactor = 0.4
)

// BossSummon is how a boss summons (config_bosses.summon, boss v3): at the start of each of its
// turns from round FirstRound on, when the previous wave (the initial minions first) is dead and
// more than CooldownRounds boss turns have passed since its last summon, Count minions of
// MinionID appear (at most waves.maxAlive alive) in a ShotResolved of its own (actionMode
// summon) before its attack; a stunned or frozen boss still summons.
type BossSummon struct {
	MinionID       string `json:"minionId"`
	Count          int    `json:"count"`
	CooldownRounds int    `json:"cooldownRounds"`
	FirstRound     int    `json:"firstRound"`
}

// SummonConfig is how b summons: its summon config, else (a boss v2 row) its first summon
// skill's minion, count and cooldown from round 1; nil when it never summons.
func (b BossConfig) SummonConfig() *BossSummon {
	if b.Summon != nil {
		s := *b.Summon
		return &s
	}
	for _, s := range b.Skills {
		if s.Kind == BossSkillSummon {
			return &BossSummon{MinionID: s.MinionID, Count: s.Count, CooldownRounds: s.Cooldown, FirstRound: 1}
		}
	}
	return nil
}

// AttackSkills are the skills b may cast as its turn's attack (every kind but summon).
func (b BossConfig) AttackSkills() []BossSkill {
	var out []BossSkill
	for _, s := range b.Skills {
		if s.Kind != BossSkillSummon {
			out = append(out, s)
		}
	}
	return out
}

// ParseBossSummon reads config_bosses.summon into b.Summon (NULL / empty / null: none).
func ParseBossSummon(b *BossConfig, raw []byte) error {
	b.Summon = nil
	if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
		return nil
	}
	var v BossSummon
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("summon: %w", err)
	}
	b.Summon = &v
	return nil
}

// SummonJSON is config_bosses.summon of b (nil: SQL NULL).
func (b BossConfig) SummonJSON() []byte {
	if b.Summon == nil {
		return nil
	}
	out, _ := json.Marshal(b.Summon)
	return out
}

// MinionConfig is a minion type of a boss (config_bosses.minions, a JSON array): its stats, the
// character whose weapon and walking it plays with (the bot AI aims it like that character's
// gun, Damage × ShotDamageFactor replacing the gun's damage) and the bot hit chance (0: the
// match's bot tier).
//
// Behavior and movement (boss v2): a shooter stays put (movement none) or walks with the bot AI
// (walk) and shoots; a bomber moves toward the nearest party player each turn (walk: WalkTo with
// its move energy, at most WalkRange px, never into a hole to the bottom; fly: a straight flight
// of up to FlyRange px, HoverHeight px over the ground under it) and detonates when it ends its
// move within
// DetonateRange px of one, else shoots. A detonation, and the blast of a minion with
// ExplodeOnDeath when it dies, hits everyone of the other side within ExplodeRadius px (a death
// blast also the other minions, never the boss) for ExplodeDamagePct % of the max HP of the one
// hit (ExplodeDamageOf target) or of the minion (self), full at the centre down to half at the
// edge, no defense, shield first, and carves an ExplodeCrater px crater. Zero values are the
// defaults (MinionDefaults); Normalized fills them in.
type MinionConfig struct {
	MinionID    string  `json:"minionId"`
	Name        string  `json:"name"`
	CharacterID string  `json:"characterId"`
	HP          int     `json:"hp"`
	Defense     int     `json:"defense"`
	Damage      int     `json:"damage"`
	MoveEnergy  int     `json:"moveEnergy"`
	BodySize    float64 `json:"bodySize"`
	HitChance   float64 `json:"hitChance,omitempty"`

	Behavior         string  `json:"behavior,omitempty"`         // shooter (""), bomber, leaper
	Movement         string  `json:"movement,omitempty"`         // walk (""), none, fly
	ShotDamageFactor float64 `json:"shotDamageFactor,omitempty"` // × Damage for its shots (0: bomber 0.8, shooter / leaper 1)
	DetonateRange    float64 `json:"detonateRange,omitempty"`    // bomber: px from a party player's feet (0: 70)
	FlyRange         float64 `json:"flyRange,omitempty"`         // fly: px per turn (0: 90)
	WalkRange        float64 `json:"walkRange,omitempty"`        // walk: px per turn at most, whatever its move energy (0: 80)
	HoverHeight      float64 `json:"hoverHeight,omitempty"`      // fly: px over the ground under it (0: 60)
	FlySpeed         float64 `json:"flySpeed,omitempty"`         // fly: px/s of UnitFlew (0: 220)
	ExplodeOnDeath   bool    `json:"explodeOnDeath,omitempty"`
	ExplodeDamageOf  string  `json:"explodeDamageOf,omitempty"`  // target (""), self
	ExplodeDamagePct float64 `json:"explodeDamagePct,omitempty"` // % of the max HP (0: 50)
	ExplodeRadius    float64 `json:"explodeRadius,omitempty"`    // px (0: 80)
	ExplodeCrater    float64 `json:"explodeCrater,omitempty"`    // px (0: 20)
	// Leaper (boss v3): LeapRange px at most per leap (0: the whole map); LeapDetonateNextTurn
	// (nil: true) detonates on the turn after a leap when still within detonateRange;
	// ShootAfterLeap also shoots on the turn it leaps.
	LeapRange            float64 `json:"leapRange,omitempty"`
	LeapDetonateNextTurn *bool   `json:"leapDetonateNextTurn,omitempty"`
	ShootAfterLeap       bool    `json:"shootAfterLeap,omitempty"`
	// ProjectileKind is ShotResolved projectiles[].kind of its shots ("": a normal shot's).
	ProjectileKind string `json:"projectileKind,omitempty"`
}

// LeapDetonates reports whether a leaper detonates on the turn after a leap (default true).
func (m MinionConfig) LeapDetonates() bool {
	return m.LeapDetonateNextTurn == nil || *m.LeapDetonateNextTurn
}

// Minion behaviors (MinionConfig.Behavior), movements (Movement) and blast bases
// (ExplodeDamageOf).
const (
	MinionShooter = "shooter"
	MinionBomber  = "bomber"
	// MinionLeaper (boss v3): leaps (UnitJumped) next to the nearest party member when none is
	// within detonateRange, detonates on its next turn when one still is, else shoots.
	MinionLeaper = "leaper"

	MinionMoveNone = "none"
	MinionMoveWalk = "walk"
	MinionMoveFly  = "fly"

	ExplodeOfTarget = "target"
	ExplodeOfSelf   = "self"
)

// MinionDefaults are the values a zero MinionConfig field stands for (the shot damage factor
// depends on the behavior: BomberShotDamageFactor or 1).
var MinionDefaults = MinionConfig{
	Behavior: MinionShooter, Movement: MinionMoveWalk, DetonateRange: 70, FlyRange: 90, WalkRange: 80, HoverHeight: 60, FlySpeed: 220,
	ExplodeDamageOf: ExplodeOfTarget, ExplodeDamagePct: 50, ExplodeRadius: 80, ExplodeCrater: 20,
}

// BomberShotDamageFactor is a bomber's shot damage factor unless ShotDamageFactor is set (boss
// v3: 0.35 → 0.8).
const BomberShotDamageFactor = 0.8

// Normalized is m with its zero fields set to their defaults.
func (m MinionConfig) Normalized() MinionConfig {
	d := MinionDefaults
	str := func(v *string, def string) {
		if *v == "" {
			*v = def
		}
	}
	num := func(v *float64, def float64) {
		if *v == 0 {
			*v = def
		}
	}
	str(&m.Behavior, d.Behavior)
	str(&m.Movement, d.Movement)
	str(&m.ExplodeDamageOf, d.ExplodeDamageOf)
	if m.ShotDamageFactor == 0 {
		m.ShotDamageFactor = 1
		if m.Behavior == MinionBomber {
			m.ShotDamageFactor = BomberShotDamageFactor
		}
	}
	num(&m.DetonateRange, d.DetonateRange)
	num(&m.FlyRange, d.FlyRange)
	num(&m.WalkRange, d.WalkRange)
	num(&m.HoverHeight, d.HoverHeight)
	num(&m.FlySpeed, d.FlySpeed)
	num(&m.ExplodeDamagePct, d.ExplodeDamagePct)
	num(&m.ExplodeRadius, d.ExplodeRadius)
	num(&m.ExplodeCrater, d.ExplodeCrater)
	return m
}

// BossHover is the band a flying boss hovers in (config_bosses.hover): at the start of each of
// its turns it flies to a random point (its feet) of MinX..MaxX × MinY..MaxY px, at least
// MinMove px (0: 200) from where it was, at FlySpeed px/s (0: 220), then casts.
type BossHover struct {
	MinX     float64 `json:"minX"`
	MaxX     float64 `json:"maxX"`
	MinY     float64 `json:"minY"`
	MaxY     float64 `json:"maxY"`
	MinMove  float64 `json:"minMove,omitempty"`
	FlySpeed float64 `json:"flySpeed,omitempty"`
}

// Hover defaults (BossHover zero fields).
const (
	DefaultHoverMinMove = 200.0
	DefaultFlySpeed     = 220.0
)

// Normalized is h with its zero MinMove / FlySpeed set to the defaults.
func (h BossHover) Normalized() BossHover {
	if h.MinMove == 0 {
		h.MinMove = DefaultHoverMinMove
	}
	if h.FlySpeed == 0 {
		h.FlySpeed = DefaultFlySpeed
	}
	return h
}

// ParseBossHover reads config_bosses.hover into b.Hover (NULL / empty / null: no band).
func ParseBossHover(b *BossConfig, raw []byte) error {
	b.Hover = nil
	if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
		return nil
	}
	var h BossHover
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return fmt.Errorf("hover: %w", err)
	}
	b.Hover = &h
	return nil
}

// HoverJSON is config_bosses.hover of b (nil: SQL NULL).
func (b BossConfig) HoverJSON() []byte {
	if b.Hover == nil {
		return nil
	}
	out, _ := json.Marshal(b.Hover)
	return out
}

// BossWaveEntry is Count minions of MinionID.
type BossWaveEntry struct {
	MinionID string `json:"minionId"`
	Count    int    `json:"count"`
}

// BossWaves are the minions at the start of the match and the most alive at once
// (config_bosses.waves).
type BossWaves struct {
	Initial  []BossWaveEntry `json:"initial"`
	MaxAlive int             `json:"maxAlive"`
}

// BossDrop is a weighted entry of a reward table's drop rolls: Min..Max of config_items ItemID.
type BossDrop struct {
	ItemID string  `json:"itemId"`
	Weight float64 `json:"weight"`
	Min    int     `json:"min"`
	Max    int     `json:"max"`
}

// BossRewardTable turns a player's effective damage (damage to the boss + minionDamageFactor ×
// damage to minions) into gold (coin), EXP and drop rolls:
//
//	gold = min(baseGold + effective / 1000 × goldPer1000Damage, maxGold) + winGold on a win
//	exp  = min(baseExp + effective / 1000 × expPer1000Damage, maxExp) + winExp on a win
//	rolls = min(floor(effective / damagePerDropRoll), maxDropRolls) + winDropRolls on a win
//
// Each roll draws one of the drops by weight, or nothing with emptyWeight.
type BossRewardTable struct {
	BaseGold          int        `json:"baseGold"`
	GoldPer1000Damage float64    `json:"goldPer1000Damage"`
	MaxGold           int        `json:"maxGold"`
	WinGold           int        `json:"winGold"`
	BaseExp           int        `json:"baseExp"`
	ExpPer1000Damage  float64    `json:"expPer1000Damage"`
	MaxExp            int        `json:"maxExp"`
	WinExp            int        `json:"winExp"`
	DamagePerDropRoll int        `json:"damagePerDropRoll"`
	MaxDropRolls      int        `json:"maxDropRolls"`
	WinDropRolls      int        `json:"winDropRolls"`
	EmptyWeight       float64    `json:"emptyWeight"`
	Drops             []BossDrop `json:"drops"`
}

// BossRewards are a boss's reward tables (config_bosses.rewards): Bonus for a player's first
// daily_bonus_runs runs of the day of this boss, Normal after.
type BossRewards struct {
	MinionDamageFactor float64         `json:"minionDamageFactor"`
	Bonus              BossRewardTable `json:"bonus"`
	Normal             BossRewardTable `json:"normal"`
}

// Boss out-of-map rules (BossConfig.OutOfMap): a blast that would drop the boss off the bottom
// of the map leaves it where it stands (clamp) or kills it (kill).
const (
	BossOutOfMapClamp = "clamp"
	BossOutOfMapKill  = "kill"
)

// BossConfig is a config_bosses row.
type BossConfig struct {
	BossID           string
	Name             string
	Description      string
	RecommendedLevel int
	Enabled          bool
	SortOrder        int
	MapID            string // the arena (config_maps): spawn points 0..3 the party, 4 the boss, 5.. the minions
	HP               int
	Defense          int
	Damage           int     // the base of its skills' damage
	BodySize         float64 // × a player's body: hit radius physics.player_hit_radius × it around shotOriginLift × it above the feet
	TurnLimit        int     // boss turns; after the last one the boss escapes (the party loses)
	// ShieldedDamageFactor scales the damage the boss takes while any of its minions lives.
	ShieldedDamageFactor float64
	OutOfMap             string // clamp, kill
	Skills               []BossSkill
	Minions              []MinionConfig
	Waves                BossWaves
	Rewards              BossRewards
	DailyBonusRuns       int
	// Flying: the boss never falls and craters do not move it; with a Hover band it flies to a
	// new point of it at the start of each of its turns (UnitFlew), then casts.
	Flying bool
	Hover  *BossHover
	// Summon (boss v3, config_bosses.summon): the free summon at the start of the boss's turns
	// (nil: SummonConfig falls back to a legacy summon skill).
	Summon *BossSummon
}

// Minion is the boss's minion type id.
func (b BossConfig) Minion(id string) (MinionConfig, bool) {
	for _, m := range b.Minions {
		if m.MinionID == id {
			return m, true
		}
	}
	return MinionConfig{}, false
}

// Skill is the boss's skill id.
func (b BossConfig) Skill(id string) (BossSkill, bool) {
	for _, s := range b.Skills {
		if s.SkillID == id {
			return s, true
		}
	}
	return BossSkill{}, false
}

// Queue is the boss's queue and match mode.
func (b BossConfig) Queue() string { return BossQueue(b.BossID) }

// ParseBossJSON reads the JSON columns of a boss row (skills, minions, waves, rewards).
func ParseBossJSON(b *BossConfig, skills, minions, waves, rewards []byte) error {
	parse := func(name string, raw []byte, dst any) error {
		if len(raw) == 0 {
			return nil
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
	if err := parse("skills", skills, &b.Skills); err != nil {
		return err
	}
	if err := parse("minions", minions, &b.Minions); err != nil {
		return err
	}
	if err := parse("waves", waves, &b.Waves); err != nil {
		return err
	}
	return parse("rewards", rewards, &b.Rewards)
}

// ValidateBoss reports what is wrong with a boss config, nil when the match can run it: the
// numbers in range, skills of a known kind with their numbers, minions of a known character
// (characters nil: not checked), waves and summons of its own minions, rewards with known
// items (items nil: not checked).
func ValidateBoss(b BossConfig, characters map[string]CharacterConfig, items map[string]ItemConfig) error {
	bad := func(format string, args ...any) error { return fmt.Errorf(format, args...) }
	finite := func(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
	switch {
	case b.BossID == "" || strings.ContainsAny(b.BossID, " :"):
		return bad("boss id %q: required, no spaces or ':'", b.BossID)
	case strings.TrimSpace(b.Name) == "":
		return bad("name: required")
	case b.MapID == "":
		return bad("map id: required")
	case b.HP < 1:
		return bad("hp %d < 1", b.HP)
	case b.Defense < 0, b.Damage < 0:
		return bad("defense / damage must be >= 0")
	case !finite(b.BodySize) || b.BodySize < 0.5 || b.BodySize > 6:
		return bad("bodySize %v outside 0.5..6", b.BodySize)
	case b.TurnLimit < 1 || b.TurnLimit > 200:
		return bad("turnLimit %d outside 1..200", b.TurnLimit)
	case !finite(b.ShieldedDamageFactor) || b.ShieldedDamageFactor < 0 || b.ShieldedDamageFactor > 1:
		return bad("shieldedDamageFactor %v outside 0..1", b.ShieldedDamageFactor)
	case b.OutOfMap != BossOutOfMapClamp && b.OutOfMap != BossOutOfMapKill:
		return bad("outOfMap %q: clamp or kill", b.OutOfMap)
	case b.DailyBonusRuns < 0 || b.DailyBonusRuns > 100:
		return bad("dailyBonusRuns %d outside 0..100", b.DailyBonusRuns)
	case b.RecommendedLevel < 1:
		return bad("recommendedLevel %d < 1", b.RecommendedLevel)
	case len(b.AttackSkills()) == 0:
		return bad("skills: at least one attack (not a summon)")
	}
	if h := b.Hover; h != nil {
		switch {
		case !b.Flying:
			return bad("hover: only for a flying boss")
		case !inRange(h.MinX, 0, 20000), !inRange(h.MaxX, 0, 20000), !inRange(h.MinY, 0, 20000), !inRange(h.MaxY, 0, 20000),
			h.MinX > h.MaxX, h.MinY > h.MaxY:
			return bad("hover: 0 <= minX <= maxX, 0 <= minY <= maxY")
		case !inRange(h.MinMove, 0, 5000), !inRange(h.FlySpeed, 0, 3000), h.FlySpeed > 0 && h.FlySpeed < 20:
			return bad("hover: minMove 0..5000, flySpeed 0 or 20..3000")
		}
	}

	minions := map[string]bool{}
	for i, m := range b.Minions {
		switch {
		case m.MinionID == "" || minions[m.MinionID]:
			return bad("minions[%d]: minionId %q missing or repeated", i, m.MinionID)
		case strings.TrimSpace(m.Name) == "":
			return bad("minion %s: name required", m.MinionID)
		case m.HP < 1 || m.Defense < 0 || m.Damage < 0 || m.MoveEnergy < 0:
			return bad("minion %s: hp >= 1, defense / damage / moveEnergy >= 0", m.MinionID)
		case !finite(m.BodySize) || m.BodySize < 0.3 || m.BodySize > 3:
			return bad("minion %s: bodySize %v outside 0.3..3", m.MinionID, m.BodySize)
		case !finite(m.HitChance) || m.HitChance < 0 || m.HitChance > 100:
			return bad("minion %s: hitChance %v outside 0..100", m.MinionID, m.HitChance)
		case m.Behavior != "" && m.Behavior != MinionShooter && m.Behavior != MinionBomber && m.Behavior != MinionLeaper:
			return bad("minion %s: behavior %q: shooter, bomber or leaper", m.MinionID, m.Behavior)
		case !inRange(m.LeapRange, 0, 5000):
			return bad("minion %s: leapRange 0..5000", m.MinionID)
		case !validKind(m.ProjectileKind):
			return bad("minion %s: projectileKind %q: up to 32 of a-z, 0-9, _", m.MinionID, m.ProjectileKind)
		case m.Movement != "" && m.Movement != MinionMoveNone && m.Movement != MinionMoveWalk && m.Movement != MinionMoveFly:
			return bad("minion %s: movement %q: none, walk or fly", m.MinionID, m.Movement)
		case m.ExplodeDamageOf != "" && m.ExplodeDamageOf != ExplodeOfTarget && m.ExplodeDamageOf != ExplodeOfSelf:
			return bad("minion %s: explodeDamageOf %q: target or self", m.MinionID, m.ExplodeDamageOf)
		case !inRange(m.ShotDamageFactor, 0, 5), !inRange(m.DetonateRange, 0, 500), !inRange(m.FlyRange, 0, 2400), !inRange(m.WalkRange, 0, 2400),
			!inRange(m.HoverHeight, 0, 400), !inRange(m.FlySpeed, 0, 3000), m.FlySpeed > 0 && m.FlySpeed < 20:
			return bad("minion %s: shotDamageFactor 0..5, detonateRange 0..500, flyRange / walkRange 0..2400, hoverHeight 0..400, flySpeed 0 or 20..3000", m.MinionID)
		case !inRange(m.ExplodeDamagePct, 0, 100), !inRange(m.ExplodeRadius, 0, 400), !inRange(m.ExplodeCrater, 0, 200):
			return bad("minion %s: explodeDamagePct 0..100, explodeRadius 0..400, explodeCrater 0..200", m.MinionID)
		}
		if characters != nil {
			if _, ok := characters[m.CharacterID]; !ok {
				return bad("minion %s: character %q is not in config_characters", m.MinionID, m.CharacterID)
			}
		}
		minions[m.MinionID] = true
	}
	if b.Waves.MaxAlive < 0 || b.Waves.MaxAlive > 12 {
		return bad("waves.maxAlive %d outside 0..12", b.Waves.MaxAlive)
	}
	initial := 0
	for _, w := range b.Waves.Initial {
		if !minions[w.MinionID] || w.Count < 1 {
			return bad("waves.initial: minion %q unknown or count < 1", w.MinionID)
		}
		initial += w.Count
	}
	if initial > b.Waves.MaxAlive {
		return bad("waves.initial: %d minions > maxAlive %d", initial, b.Waves.MaxAlive)
	}
	if s := b.Summon; s != nil {
		switch {
		case !minions[s.MinionID] || s.Count < 1 || s.Count > 12:
			return bad("summon: minion %q unknown or count outside 1..12", s.MinionID)
		case s.CooldownRounds < 0 || s.CooldownRounds > 50 || s.FirstRound < 0 || s.FirstRound > 200:
			return bad("summon: cooldownRounds 0..50, firstRound 0..200")
		}
	}

	skillIDs := map[string]bool{}
	for i, s := range b.Skills {
		if s.SkillID == "" || skillIDs[s.SkillID] {
			return bad("skills[%d]: skillId %q missing or repeated", i, s.SkillID)
		}
		skillIDs[s.SkillID] = true
		in := func(f, lo, hi float64) bool { return finite(f) && f >= lo && f <= hi }
		switch {
		case strings.TrimSpace(s.Name) == "":
			return bad("skill %s: name required", s.SkillID)
		case !in(s.Weight, 0, 1000) || s.Weight == 0:
			return bad("skill %s: weight %v outside (0, 1000]", s.SkillID, s.Weight)
		case s.Cooldown < 0 || s.Cooldown > 50:
			return bad("skill %s: cooldown %d outside 0..50", s.SkillID, s.Cooldown)
		case s.Target != "" && s.Target != BossTargetRandom && s.Target != BossTargetNearest && s.Target != BossTargetLowestHP &&
			!(s.Kind == BossSkillRain && (s.Target == BossTargetParty || s.Target == BossTargetMap)):
			return bad("skill %s: target %q: random, nearest or lowest_hp (rain also party, map)", s.SkillID, s.Target)
		case s.StatusTurns < 0 || s.StatusTurns > 5 || (s.Status != "" && s.StatusTurns < 1):
			return bad("skill %s: statusTurns %d (1..5 with a status)", s.SkillID, s.StatusTurns)
		case !in(s.StatusChance, 0, 1):
			return bad("skill %s: statusChance %v outside 0..1 (0: %v)", s.SkillID, s.StatusChance, DefaultBossStatusChance)
		}
		switch s.Kind {
		case BossSkillRain:
			switch {
			case s.Count < 1 || s.Count > 60:
				return bad("skill %s: count %d outside 1..60", s.SkillID, s.Count)
			case !in(s.Spread, 0, 3000), !in(s.Damage, 0, 20), !in(s.Blast, 1, 400), !in(s.Crater, 0, 200), !in(s.Cap, 0, 50):
				return bad("skill %s: spread 0..3000, damage 0..20, blast 1..400, crater 0..200, cap 0..50", s.SkillID)
			case s.LavaTurns < 0 || s.LavaTurns > 10 || s.LavaDamage < 0 || !in(s.LavaRadius, 0, 400) || (s.LavaTurns > 0 && s.LavaRadius <= 0):
				return bad("skill %s: lavaTurns 0..10, lavaDamage >= 0, lavaRadius 0..400 (> 0 with lava)", s.SkillID)
			case !validKind(s.ProjectileKind):
				return bad("skill %s: projectileKind %q: up to 32 of a-z, 0-9, _", s.SkillID, s.ProjectileKind)
			case s.LaunchFrom != RainFromTop && s.LaunchFrom != RainFromBoss:
				return bad("skill %s: launchFrom %q: \"\" (the top of the map) or boss", s.SkillID, s.LaunchFrom)
			}
		case BossSkillBeam:
			switch {
			case !in(s.Damage, 0, 20) || !in(s.HalfWidth, 1, 200) || !in(s.Decay, 0, 1):
				return bad("skill %s: damage 0..20, halfWidth 1..200, decay 0..1", s.SkillID)
			case s.Pattern != BeamAimed && s.Pattern != BeamRadial && s.Pattern != BeamSweep:
				return bad("skill %s: pattern %q: \"\", radial or sweep", s.SkillID, s.Pattern)
			case s.Pattern != BeamAimed && (s.Count < 1 || s.Count > 60):
				return bad("skill %s: count %d outside 1..60 (beams of a %s)", s.SkillID, s.Count, s.Pattern)
			}
		case BossSkillShockwave:
			if !in(s.Damage, 0, 20) || !in(s.Blast, 0, 400) || !in(s.Crater, 0, 200) || !in(s.Step, 0, 1200) || (s.Step > 0 && s.Step < 20) ||
				!in(s.Interval, 0, 2) || !in(s.MinFactor, 0, 1) {
				return bad("skill %s: damage 0..20, blast 0..400, crater 0..200, step 0 or 20..1200, interval 0..2, minFactor 0..1", s.SkillID)
			}
		case BossSkillSummon:
			if !minions[s.MinionID] || s.Count < 1 || s.Count > 12 {
				return bad("skill %s: minion %q unknown or count outside 1..12", s.SkillID, s.MinionID)
			}
		default:
			return bad("skill %s: kind %q: %s", s.SkillID, s.Kind, strings.Join(BossSkillKinds, ", "))
		}
	}

	r := b.Rewards
	if !finite(r.MinionDamageFactor) || r.MinionDamageFactor < 0 || r.MinionDamageFactor > 10 {
		return bad("rewards.minionDamageFactor %v outside 0..10", r.MinionDamageFactor)
	}
	for name, t := range map[string]BossRewardTable{"bonus": r.Bonus, "normal": r.Normal} {
		if t.BaseGold < 0 || t.MaxGold < 0 || t.WinGold < 0 || t.BaseExp < 0 || t.MaxExp < 0 || t.WinExp < 0 ||
			t.DamagePerDropRoll < 0 || t.MaxDropRolls < 0 || t.MaxDropRolls > 20 || t.WinDropRolls < 0 || t.WinDropRolls > 20 ||
			!finite(t.GoldPer1000Damage) || t.GoldPer1000Damage < 0 || !finite(t.ExpPer1000Damage) || t.ExpPer1000Damage < 0 ||
			!finite(t.EmptyWeight) || t.EmptyWeight < 0 {
			return bad("rewards.%s: negative or too large numbers (rolls at most 20)", name)
		}
		for _, d := range t.Drops {
			if d.ItemID == "" || !finite(d.Weight) || d.Weight <= 0 || d.Min < 1 || d.Max < d.Min || d.Max > 100 {
				return bad("rewards.%s: drop %q needs weight > 0 and 1 <= min <= max <= 100", name, d.ItemID)
			}
			if items != nil {
				if _, ok := items[d.ItemID]; !ok {
					return bad("rewards.%s: item %q is not in config_items", name, d.ItemID)
				}
			}
		}
	}
	return nil
}

// inRange reports whether f is a finite number within lo..hi.
func inRange(f, lo, hi float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && f >= lo && f <= hi
}

// SpawnSetPillar marks the spawn points of an arena's pillars or islets (SpawnPoint.Set; boss v3:
// the sky islets of arena_ice_witch_islands carry the same tag): the party spawns on random
// distinct ones and the minions appear on free ones (ArenaPillars).
const SpawnSetPillar = "pillar"

// ArenaPillars are the arena's pillar spawn points (Set pillar), nil for an arena without.
func ArenaPillars(m MapConfig) []SpawnPoint {
	var out []SpawnPoint
	for _, sp := range m.SpawnPoints {
		if sp.Set == SpawnSetPillar {
			out = append(out, sp)
		}
	}
	return out
}

// BossPartySpawns are the spawn points of the n party members of a match on arena m: n random
// distinct pillars on a pillar arena (intn(k) draws in [0, k); with fewer pillars than members
// they repeat), the party spawn points (BossSpawns) in order otherwise.
func BossPartySpawns(m MapConfig, n int, intn func(int) int) []SpawnPoint {
	out := make([]SpawnPoint, 0, n)
	if pillars := ArenaPillars(m); len(pillars) > 0 {
		pool := append([]SpawnPoint(nil), pillars...)
		for i := len(pool) - 1; i > 0; i-- {
			j := intn(i + 1)
			pool[i], pool[j] = pool[j], pool[i]
		}
		for i := 0; i < n; i++ {
			out = append(out, pool[i%len(pool)])
		}
		return out
	}
	party, _, _ := BossSpawns(m)
	for i := 0; i < n; i++ {
		out = append(out, party[i%len(party)])
	}
	return out
}

// BossSpawns splits an arena's spawn points: the party's (0..3), the boss's (4) and the
// minions' (5..). Missing points fall back to points spread over the map's width.
func BossSpawns(m MapConfig) (party []SpawnPoint, boss SpawnPoint, minions []SpawnPoint) {
	w, _ := m.PixelSize()
	if w <= 0 {
		w = DefaultMapWidth
	}
	sp := m.SpawnPoints
	for i := 0; i < BossPartySize; i++ {
		if i < len(sp) {
			party = append(party, sp[i])
		} else {
			party = append(party, SpawnPoint{X: float64(w) * (0.08 + 0.07*float64(i)), Y: 0})
		}
	}
	if len(sp) > BossPartySize {
		boss = sp[BossPartySize]
	} else {
		boss = SpawnPoint{X: float64(w) * 0.85, Y: 0}
	}
	if len(sp) > BossPartySize+1 {
		minions = append(minions, sp[BossPartySize+1:]...)
	} else {
		minions = []SpawnPoint{{X: float64(w) * 0.6, Y: 0}, {X: float64(w) * 0.68, Y: 0}}
	}
	return party, boss, minions
}

// Boss is the snapshot's boss id, enabled or not.
func (s *Snapshot) Boss(id string) (BossConfig, bool) {
	for _, b := range s.Bosses {
		if b.BossID == id {
			return b, true
		}
	}
	return BossConfig{}, false
}

// BossOfQueue is the enabled boss whose queue is q.
func (s *Snapshot) BossOfQueue(q string) (BossConfig, bool) {
	id, ok := BossIDOfQueue(q)
	if !ok {
		return BossConfig{}, false
	}
	b, ok := s.Boss(id)
	if !ok || !b.Enabled {
		return BossConfig{}, false
	}
	return b, true
}

// BossQueues are the queues of the enabled bosses, in display order.
func (s *Snapshot) BossQueues() []string {
	var out []string
	for _, b := range s.Bosses {
		if b.Enabled {
			out = append(out, b.Queue())
		}
	}
	return out
}

// BossArenaPrefix starts the id of every boss arena (mapgen arenas), used by a boss or not.
const BossArenaPrefix = "arena_"

// IsBossArena reports whether mapID is a boss arena (kept out of the PvP map rotation): the arena
// of a boss, or any map whose id starts with BossArenaPrefix (an arena no boss uses any more).
func (s *Snapshot) IsBossArena(mapID string) bool {
	if strings.HasPrefix(mapID, BossArenaPrefix) {
		return true
	}
	for _, b := range s.Bosses {
		if b.MapID == mapID {
			return true
		}
	}
	return false
}

// LobbyPartySize is the most members of a lobby queueing for q: the party size of the enabled
// lobby mode with that queue, at most max (and max when no such mode).
func (s *Snapshot) LobbyPartySize(q string, maxSize int) int {
	for _, m := range s.Modes {
		if m.Kind == "lobby" && m.Enabled && m.Queue == q && m.PartySize >= 1 {
			return min(m.PartySize, maxSize)
		}
	}
	return maxSize
}
