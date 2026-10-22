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
	// Spread 0 is a single slam on the target.
	BossSkillRain = "rain"
	// BossSkillBeam: a straight beam from the boss's body centre through the target's body to
	// the map edge, through terrain; every enemy within HalfWidth px of it takes Damage × the
	// boss damage (Decay × the previous one for each next enemy; 0 = 1) and Status (Turns).
	BossSkillBeam = "beam"
	// BossSkillSummon: Count minions of MinionID appear on the minion spawn points, at most
	// waves.maxAlive living minions.
	BossSkillSummon = "summon"
)

// BossSkillKinds are the skill kinds a boss may use.
var BossSkillKinds = []string{BossSkillRain, BossSkillBeam, BossSkillSummon}

// Boss skill targets (BossSkill.Target).
const (
	BossTargetRandom   = "random"    // a random living enemy
	BossTargetNearest  = "nearest"   // the living enemy nearest the boss
	BossTargetLowestHP = "lowest_hp" // the living enemy with the least HP
)

// BossSkill is one skill of a boss (config_bosses.skills, a JSON array).
type BossSkill struct {
	SkillID     string  `json:"skillId"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Kind        string  `json:"kind"`     // rain, beam, summon
	Weight      float64 `json:"weight"`   // relative pick chance among the skills off cooldown
	Cooldown    int     `json:"cooldown"` // boss turns before it may be used again (0: next turn)
	Target      string  `json:"target,omitempty"`

	Count       int     `json:"count,omitempty"`
	Spread      float64 `json:"spread,omitempty"`
	Damage      float64 `json:"damage,omitempty"` // × the boss damage
	Blast       float64 `json:"blast,omitempty"`
	Crater      float64 `json:"crater,omitempty"`
	Cap         float64 `json:"cap,omitempty"`
	Decay       float64 `json:"decay,omitempty"`
	HalfWidth   float64 `json:"halfWidth,omitempty"`
	Status      string  `json:"status,omitempty"`
	StatusTurns int     `json:"statusTurns,omitempty"`
	LavaRadius  float64 `json:"lavaRadius,omitempty"`
	LavaTurns   int     `json:"lavaTurns,omitempty"`
	LavaDamage  int     `json:"lavaDamage,omitempty"`
	MinionID    string  `json:"minionId,omitempty"`
}

// MinionConfig is a minion type of a boss (config_bosses.minions, a JSON array): its stats, the
// character whose weapon and walking it plays with (the bot AI aims it like that character's
// gun, Damage replacing the gun's damage) and the bot hit chance (0: the match's bot tier).
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
	case len(b.Skills) == 0:
		return bad("skills: at least one")
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
		case s.Target != "" && s.Target != BossTargetRandom && s.Target != BossTargetNearest && s.Target != BossTargetLowestHP:
			return bad("skill %s: target %q: random, nearest or lowest_hp", s.SkillID, s.Target)
		case s.StatusTurns < 0 || s.StatusTurns > 5 || (s.Status != "" && s.StatusTurns < 1):
			return bad("skill %s: statusTurns %d (1..5 with a status)", s.SkillID, s.StatusTurns)
		}
		switch s.Kind {
		case BossSkillRain:
			switch {
			case s.Count < 1 || s.Count > 40:
				return bad("skill %s: count %d outside 1..40", s.SkillID, s.Count)
			case !in(s.Spread, 0, 1200), !in(s.Damage, 0, 20), !in(s.Blast, 1, 400), !in(s.Crater, 0, 200), !in(s.Cap, 0, 50):
				return bad("skill %s: spread 0..1200, damage 0..20, blast 1..400, crater 0..200, cap 0..50", s.SkillID)
			case s.LavaTurns < 0 || s.LavaTurns > 10 || s.LavaDamage < 0 || !in(s.LavaRadius, 0, 400) || (s.LavaTurns > 0 && s.LavaRadius <= 0):
				return bad("skill %s: lavaTurns 0..10, lavaDamage >= 0, lavaRadius 0..400 (> 0 with lava)", s.SkillID)
			}
		case BossSkillBeam:
			if !in(s.Damage, 0, 20) || !in(s.HalfWidth, 1, 200) || !in(s.Decay, 0, 1) {
				return bad("skill %s: damage 0..20, halfWidth 1..200, decay 0..1", s.SkillID)
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

// IsBossArena reports whether mapID is the arena of a boss (kept out of the PvP map rotation).
func (s *Snapshot) IsBossArena(mapID string) bool {
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
