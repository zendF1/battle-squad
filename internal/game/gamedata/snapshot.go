package gamedata

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"battle-squad/internal/shared/ranked"
)

// Snapshot is one immutable load of every config table and game_settings row: the game data a
// match plays with from start to end, and what GET /gamedata is built from (its Version is
// that payload's version). Nothing may modify a published snapshot; a reload publishes a new
// one (Publish) and matches already running keep theirs.
type Snapshot struct {
	Data    *GameData // characters, weapons, skills, items, maps
	Bricks  map[int]BrickTypeConfig
	Physics *PhysicsConfig
	Rules   *Rules
	// Status are the config_status_effects rows in order (texts and rules), then every effect
	// the match knows that has no row (DefaultStatusEffects).
	Status     []StatusEffectConfig
	RankTiers  []RankTierConfig // one per ranked.Tiers, lowest first
	EquipTiers []EquipmentTierConfig
	Modes      []GameModeConfig
	// ModeCategories are the config_mode_categories rows in order (the mode select sections)
	ModeCategories []ModeCategoryConfig
	Tips           []string
	GunKinds       []GunKindConfig       // config_gun_kinds in order
	Slots          []EquipmentSlotConfig // config_equipment_slots in order
	TypeWords      []TypeWordConfig      // config_item_type_words in order
	Settings       map[string]string     // every game_settings row (key → value)

	// Version is GET /gamedata's version of this snapshot (set before it is published), "" for
	// a snapshot no payload was built for (tests).
	Version string
	// Revision is gamedata_revision.revision when the snapshot was read (0 from YAML).
	Revision int64
	LoadedAt time.Time
	// Source is "db" or "yaml".
	Source string

	statusByID map[string]StatusEffectConfig
	skipTurn   []string
	bands      []ranked.Band
}

// StatusEffectConfig is a config_status_effects row: the texts the client shows and how the
// match treats the effect.
type StatusEffectConfig struct {
	EffectID   string
	Name       string
	Marker     string // above the player's head while it is active
	Banner     string // the notice while it skips the turn or blocks moving
	SortOrder  int
	Bad        bool // Cleanse (the bloom) removes it
	SkipsTurn  bool // the player's whole turn is skipped
	BlocksMove bool // the player cannot walk or teleport
}

// StatusRule is how the match treats a status effect.
type StatusRule struct {
	Bad, SkipsTurn, BlocksMove bool
}

// DefaultStatusEffects are the status effects the match puts on players (and wind_stop on the
// match) with the rules they had before config_status_effects held them, in display order.
var DefaultStatusEffects = []StatusEffectConfig{
	{EffectID: "burn", Name: "Bỏng", SortOrder: 1, Bad: true},
	{EffectID: "root", Name: "Bị cầm chân", Banner: "Bị cầm chân", SortOrder: 2, Bad: true, BlocksMove: true},
	{EffectID: "stun", Name: "Choáng", Banner: "Choáng — mất lượt", SortOrder: 3, Bad: true, SkipsTurn: true, BlocksMove: true},
	{EffectID: "freeze", Name: "Bị đóng băng", Banner: "Đóng băng — mất lượt", SortOrder: 4, Bad: true, SkipsTurn: true, BlocksMove: true},
	{EffectID: "net", Name: "Bị lưới", SortOrder: 5, Bad: true},
	{EffectID: "power_shot", Name: "Tăng lực", Marker: "x1.5", SortOrder: 6},
	{EffectID: "drill_bomb", Name: "Bom khoan", Marker: "Khoan", SortOrder: 7},
	{EffectID: "spider_net", Name: "Lưới nhện", Marker: "Đạn lưới", SortOrder: 8},
	{EffectID: "freeze_bomb", Name: "Bom băng", Marker: "Đạn băng", SortOrder: 9},
	{EffectID: "air_strike", Name: "Không kích", Marker: "Không kích", SortOrder: 10},
	{EffectID: "wind_stop", Name: "Dừng gió", SortOrder: 11},
}

// RankTierConfig is a config_rank_tiers row: the tier's Vietnamese name and rating band.
type RankTierConfig struct {
	Tier         string
	Name         string
	SortOrder    int
	MinRating    int
	Divisions    int
	DivisionSize int
}

// EquipmentTierConfig is a config_equipment_tiers row.
type EquipmentTierConfig struct {
	Tier, Name, Kind, IDSuffix string
	SortOrder                  int
}

// GunKindConfig is a config_gun_kinds row: a gun art kind and its Vietnamese name.
type GunKindConfig struct {
	ID, Name  string
	SortOrder int
}

// EquipmentSlotConfig is a config_equipment_slots row: a slot and its Vietnamese name.
type EquipmentSlotConfig struct {
	ID, Name  string
	SortOrder int
}

// TypeWordConfig is a config_item_type_words row: the type part of "<Tier> <Type>" equipment
// item names, its Vietnamese word and its gun kind ("" for armour).
type TypeWordConfig struct {
	Word, Name, GunKind string
	SortOrder           int
}

// GameModeConfig is a config_game_modes row (kind lobby or match). A lobby mode sits in
// Category (a ModeCategoryConfig), shows Icon (a client icon key), takes up to PartySize
// lobby members and changes rating when Ranked; a disabled one is not offered.
type GameModeConfig struct {
	ModeID, Kind, Name, Description, Queue, Bot string
	Category, Icon                              string
	PartySize                                   int
	Ranked, Enabled                             bool
	SortOrder                                   int
}

// ModeCategoryConfig is a config_mode_categories row: a section of the mode select screen.
type ModeCategoryConfig struct {
	ID, Name  string
	SortOrder int
}

var current atomic.Pointer[Snapshot]

// Current is the snapshot new matches start with (and the API serves): the last one
// published, or the defaults (no characters, weapons, … ; default physics and rules) before
// any. It is safe to call from any goroutine; never modify what it returns.
func Current() *Snapshot {
	if s := current.Load(); s != nil {
		return s
	}
	return emptySnapshot()
}

// Publish makes s the current snapshot, atomically: a reader sees the old or the new one,
// whole. s must not be modified afterwards.
func Publish(s *Snapshot) {
	s.index()
	current.Store(s)
}

// Cleaner is what UseForTest needs of a *testing.T.
type Cleaner interface{ Cleanup(func()) }

// UseForTest publishes s for a test and restores the previous snapshot when it ends.
func UseForTest(t Cleaner, s *Snapshot) {
	prev := current.Load()
	Publish(s)
	t.Cleanup(func() { current.Store(prev) })
}

var emptyOnce sync.Once
var empty *Snapshot

func emptySnapshot() *Snapshot {
	emptyOnce.Do(func() {
		empty = NewSnapshot(nil)
	})
	return empty
}

// NewSnapshot is a snapshot of data (nil: no configs) with the default physics, rules, status
// effects and rank bands, and no texts.
func NewSnapshot(data *GameData) *Snapshot {
	if data == nil {
		data = &GameData{}
	}
	data.fill()
	s := &Snapshot{
		Data: data, Bricks: map[int]BrickTypeConfig{}, Physics: DefaultPhysics(), Rules: DefaultRules(),
		Settings: map[string]string{}, LoadedAt: time.Now(),
	}
	s.Status = withDefaultStatus(nil)
	s.RankTiers = rankTiersWithDefaults(nil)
	s.index()
	return s
}

func (d *GameData) fill() {
	if d.Characters == nil {
		d.Characters = map[string]CharacterConfig{}
	}
	if d.Weapons == nil {
		d.Weapons = map[string]WeaponConfig{}
	}
	if d.Skills == nil {
		d.Skills = map[string]SkillConfig{}
	}
	if d.Items == nil {
		d.Items = map[string]ItemConfig{}
	}
	if d.Maps == nil {
		d.Maps = map[string]MapConfig{}
	}
}

// clone is a shallow copy of s without its indexes (for the With* builders).
func (s *Snapshot) clone() *Snapshot {
	c := *s
	c.statusByID, c.skipTurn, c.bands = nil, nil, nil
	c.Version = ""
	return &c
}

// WithData is a copy of s playing with data (tests).
func (s *Snapshot) WithData(data *GameData) *Snapshot {
	c := s.clone()
	if data == nil {
		data = &GameData{}
	}
	data.fill()
	c.Data = data
	c.index()
	return c
}

// WithPhysics is a copy of s with physics p (tests).
func (s *Snapshot) WithPhysics(p *PhysicsConfig) *Snapshot {
	c := s.clone()
	if p == nil {
		p = DefaultPhysics()
	}
	c.Physics = p
	c.index()
	return c
}

// WithBricks is a copy of s with the brick types (tests).
func (s *Snapshot) WithBricks(b map[int]BrickTypeConfig) *Snapshot {
	c := s.clone()
	c.Bricks = b
	c.index()
	return c
}

// WithRules is a copy of s with the rules (tests).
func (s *Snapshot) WithRules(r *Rules) *Snapshot {
	c := s.clone()
	c.Rules = r
	c.index()
	return c
}

// index builds the lookups of the status rules and rank bands. Every constructor calls it
// before the snapshot is shared, so readers never write.
func (s *Snapshot) index() {
	if s.statusByID != nil {
		return
	}
	s.statusByID = make(map[string]StatusEffectConfig, len(s.Status))
	s.skipTurn = nil
	var skips []StatusEffectConfig
	for _, e := range s.Status {
		s.statusByID[e.EffectID] = e
		if e.SkipsTurn {
			skips = append(skips, e)
		}
	}
	for _, e := range skips {
		s.skipTurn = append(s.skipTurn, e.EffectID)
	}
	s.bands = bandsOf(s.RankTiers)
}

// StatusRule is how the match treats effectID (all false for an unknown one).
func (s *Snapshot) StatusRule(effectID string) StatusRule {
	e := s.statusByID[effectID]
	return StatusRule{Bad: e.Bad, SkipsTurn: e.SkipsTurn, BlocksMove: e.BlocksMove || e.SkipsTurn}
}

// SkipTurnEffects are the effects that skip a turn, in the order the match checks them (the
// display order: when stun and freeze are both active the skip counts as stun).
func (s *Snapshot) SkipTurnEffects() []string {
	return s.skipTurn
}

// RankBands are the rank tiers' rating ranges, lowest first (ranked.Bands with the
// config_rank_tiers numbers).
func (s *Snapshot) RankBands() []ranked.Band {
	return s.bands
}

// BaseMoveEnergy is a character's move energy per turn before stat points and equipment: its
// config mobility, or rules.movement.defaultMoveEnergy.
func (s *Snapshot) BaseMoveEnergy(characterID string) int {
	if c, ok := s.Data.Characters[characterID]; ok && c.Mobility > 0 {
		return c.Mobility
	}
	return s.Rules.Movement.DefaultMoveEnergy
}

// ItemValue is an item's config value when it is positive, else fallback.
func (s *Snapshot) ItemValue(itemID string, fallback float64) float64 {
	if it, ok := s.Data.Items[itemID]; ok && it.Value > 0 {
		return it.Value
	}
	return fallback
}

// Item value fallbacks: what each item did before its config value drove it.
const (
	DefaultMedkitHeal       = 450.0 // medkit: HP healed
	DefaultWindStopTurns    = 2.0   // wind_stopper: turns without wind
	DefaultDrillPierce      = 50.0  // drill_bomb: px of terrain the shot drills through
	DefaultFreezeTurns      = 1.0   // freeze_bomb: turns frozen
	DefaultPowerShotFactor  = 1.5   // power_shot: damage factor of the armed shot or skill
	DefaultNetMoveEnergyCap = 50.0  // spider_net: move energy a netted player keeps at most
)

// PowerShotFactor is the damage factor of a shot or skill armed with power_shot (its item value).
func (s *Snapshot) PowerShotFactor() float64 {
	return s.ItemValue("power_shot", DefaultPowerShotFactor)
}

// NetMoveEnergyCap is the most move energy a netted player has (the spider_net item value).
func (s *Snapshot) NetMoveEnergyCap() int {
	return int(s.ItemValue("spider_net", DefaultNetMoveEnergyCap))
}

// MapDisplay is the name and theme clients show for mapID (MatchStarted mapName / mapTheme,
// MatchFound): the map's, else the id itself and DefaultMapTheme.
func (s *Snapshot) MapDisplay(mapID string) (name, theme string) {
	if m, ok := s.Data.Maps[mapID]; ok {
		name = m.Name
		if name == "" {
			name = mapID
		}
		return name, m.ThemeOrDefault()
	}
	return mapID, DefaultMapTheme
}

// MapDisplay is Current().MapDisplay.
func MapDisplay(mapID string) (name, theme string) { return Current().MapDisplay(mapID) }

// BaseMoveEnergy is Current().BaseMoveEnergy.
func BaseMoveEnergy(characterID string) int { return Current().BaseMoveEnergy(characterID) }

// DefenseK is the current combat.defense_k.
func DefenseK() float64 { return Current().Physics.DefenseKOrDefault() }

// DefenseKOrDefault is DefenseK, or DefaultDefenseK when it is not positive.
func (p *PhysicsConfig) DefenseKOrDefault() float64 {
	if p != nil && p.DefenseK > 0 {
		return p.DefenseK
	}
	return DefaultDefenseK
}

// withDefaultStatus is rows in (sort_order, id) order, then every DefaultStatusEffects entry
// without a row.
func withDefaultStatus(rows []StatusEffectConfig) []StatusEffectConfig {
	out := append([]StatusEffectConfig(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].EffectID < out[j].EffectID
	})
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.EffectID] = true
	}
	for _, d := range DefaultStatusEffects {
		if !seen[d.EffectID] {
			out = append(out, StatusEffectConfig{EffectID: d.EffectID, Name: d.EffectID, Bad: d.Bad, SkipsTurn: d.SkipsTurn,
				BlocksMove: d.BlocksMove, SortOrder: d.SortOrder})
		}
	}
	return out
}

// DefaultRankBand is the band ranked.Bands gives tier.
func DefaultRankBand(tier string) ranked.Band {
	for _, b := range ranked.DefaultBands {
		if b.Tier == tier {
			return b
		}
	}
	return ranked.Band{Tier: tier, Divisions: 1}
}

// rankTiersWithDefaults is one entry per ranked.Tiers: its row, else the default band named
// by the tier id.
func rankTiersWithDefaults(rows map[string]RankTierConfig) []RankTierConfig {
	out := make([]RankTierConfig, 0, len(ranked.Tiers))
	for i, tier := range ranked.Tiers {
		if r, ok := rows[tier]; ok {
			out = append(out, r)
			continue
		}
		b := DefaultRankBand(tier)
		out = append(out, RankTierConfig{Tier: tier, Name: tier, SortOrder: i + 1, MinRating: b.MinRating,
			Divisions: b.Divisions, DivisionSize: b.DivisionSize})
	}
	return out
}

// bandsOf are the rating bands of the tiers, or ranked.DefaultBands when they are not valid
// (ranked.ValidateBands).
func bandsOf(tiers []RankTierConfig) []ranked.Band {
	bands := make([]ranked.Band, 0, len(tiers))
	for _, t := range tiers {
		bands = append(bands, ranked.Band{Tier: t.Tier, MinRating: t.MinRating, Divisions: t.Divisions, DivisionSize: t.DivisionSize})
	}
	if ranked.ValidateBands(bands) != nil {
		return append([]ranked.Band(nil), ranked.DefaultBands...)
	}
	return bands
}

// UseData publishes the current snapshot with data for a test (restored when it ends).
func UseData(t Cleaner, data *GameData) { UseForTest(t, Current().WithData(data)) }

// UseYAML publishes the YAML game data of configDir for a test.
func UseYAML(t interface {
	Cleaner
	Fatal(...any)
}, configDir string) *Snapshot {
	s, err := LoadYAML(configDir)
	if err != nil {
		t.Fatal(err)
	}
	UseForTest(t, s)
	return s
}
