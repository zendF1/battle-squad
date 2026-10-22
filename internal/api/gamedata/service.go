package gamedata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync/atomic"

	"battle-squad/internal/api/character"
	"battle-squad/internal/api/equipment"
	"battle-squad/internal/game/battlestats"
	gd "battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/ranked"
)

// Service serves GET /gamedata. With a snapshot source (the API's reloader) it builds the
// payload of the current snapshot once and serves it until the next reload; without one it
// reads the DB on every request.
type Service struct {
	repo    *Repository
	current func() *gd.Snapshot
	cache   atomic.Pointer[built]
}

// built is the payload of one snapshot.
type built struct {
	snap    *gd.Snapshot
	payload *GameData
	raw     json.RawMessage
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// WithSnapshots serves the payload of current() (the published game data snapshot).
func (s *Service) WithSnapshots(current func() *gd.Snapshot) *Service {
	s.current = current
	return s
}

// Get is the current payload.
func (s *Service) Get(ctx context.Context) (*GameData, error) {
	b, err := s.currentPayload(ctx)
	if err != nil {
		return nil, err
	}
	return b.payload, nil
}

// currentPayload is the payload of the current snapshot (built once per snapshot), or of a
// fresh DB read without a snapshot source.
func (s *Service) currentPayload(ctx context.Context) (*built, error) {
	if s.current == nil {
		snap, err := gd.LoadFromDB(ctx, s.repo.db)
		if err != nil {
			return nil, err
		}
		return newBuilt(snap), nil
	}
	snap := s.current()
	if b := s.cache.Load(); b != nil && b.snap == snap {
		return b, nil
	}
	b := newBuilt(snap)
	s.cache.Store(b)
	return b, nil
}

func newBuilt(snap *gd.Snapshot) *built {
	p := Stamped(snap)
	raw, _ := json.Marshal(p)
	return &built{snap: snap, payload: p, raw: raw}
}

// Version returns the payload for version: the current one when it is that version, else the
// one archived under it (gamedata_versions, written whenever a server publishes a snapshot).
// found is false for a version no server ever published (or one already pruned).
func (s *Service) Version(ctx context.Context, version string) (raw json.RawMessage, found bool, err error) {
	b, err := s.currentPayload(ctx)
	if err == nil && b.payload.Version == version {
		return b.raw, true, nil
	}
	return s.repo.Archived(ctx, version)
}

// Stamped is the payload of snap with its version.
func Stamped(snap *gd.Snapshot) *GameData {
	out := Build(snap)
	out.Version = version(out)
	return out
}

// VersionOf is the GET /gamedata version of snap: what a server stamps on the snapshot before
// publishing it, so a match (MatchStarted.gamedataVersion) and GET /gamedata built from the
// same snapshot carry the same version.
func VersionOf(snap *gd.Snapshot) string { return version(Build(snap)) }

// version is the first 8 bytes of the SHA-256 of the payload without its version, in hex.
// encoding/json writes struct fields in order and map keys sorted, so equal content hashes
// equal.
func version(g *GameData) string {
	c := *g
	c.Version = ""
	body, _ := json.Marshal(c)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:8])
}

// Build is GET /gamedata of a game data snapshot (without its version): every number comes
// from snap, so the payload is exactly what a match playing with snap plays by.
func Build(snap *gd.Snapshot) *GameData {
	r, p, settings := snap.Rules, snap.Physics, snap.Settings
	bands := snap.RankBands()
	arcs := make(map[string]float64, len(r.AimAid.GuideArcLength))
	for k, v := range r.AimAid.GuideArcLength {
		arcs[k] = v
	}
	tierRanks := make(map[string]int, len(r.SkillRanks.ByTierWord))
	for k, v := range r.SkillRanks.ByTierWord {
		tierRanks[k] = v
	}
	out := &GameData{
		Physics: Physics{
			Gravity: p.Gravity, SpeedPerPower: p.ProjectileSpeedMultiplier, WindScale: p.WindScale,
			TimeStep: p.TimeStep, PathRecordStep: p.PathRecordStep, MaxFlightSec: p.MaxFlightSeconds,
			PlayerHitRadius: p.PlayerHitRadius, CollisionStepPx: r.Physics.CollisionStepPx, ShotOriginLift: float64(r.Physics.ShotOriginLift),
			OutOfBounds: OutOfBounds{Left: r.Physics.OutOfBoundsLeft, Right: r.Physics.OutOfBoundsRight, Below: r.Physics.OutOfBoundsBelow},
		},
		Aim: Aim{PowerMin: 0, PowerMax: r.Aim.PowerMax, AngleMin: r.Aim.AngleMin, AngleMax: r.Aim.AngleMax,
			NoAimAngle: r.Aim.NoAimAngle, NoAimPower: r.Aim.NoAimPower},
		AimAid: AimAid{GuideArcLength: arcs, DefaultTier: bands[0].Tier, HitTolerance: HitTolerance{
			Max: r.AimAid.HitToleranceMax, MinPart: r.AimAid.HitToleranceMinPart, FarPx: r.AimAid.HitToleranceFarPx, HighPx: r.AimAid.HitToleranceHighPx}},
		Combat: Combat{DefenseK: p.DefenseKOrDefault(), PowerShotFactor: snap.PowerShotFactor(), LavaDamage: r.Combat.LavaDamage,
			CritMultiplier: r.Combat.CritMultiplier},
		Terrain: Terrain{
			PlayerHeight: r.Terrain.PlayerHeight, MaxClimbPerStep: r.Terrain.MaxClimbPerStep, CraterPxPerTerrainDamage: r.Terrain.CraterPxPerTerrainDamage,
			BowlDepth: r.Terrain.BowlDepth, BowlCeiling: r.Terrain.BowlCeiling, MaxTerrainDamageBonusPercent: r.Terrain.MaxTerrainDamageBonusPercent,
			FallDamageThresholdPx: p.FallDamageThreshold, FallDamagePerPx: p.FallDamagePerPixel,
		},
		Movement: Movement{PxPerEnergy: p.PxPerEnergy(), EnergyPerPx: p.EnergyPerPx(),
			DefaultMoveEnergy: r.Movement.DefaultMoveEnergy, NetMoveEnergyCap: snap.NetMoveEnergyCap()},
		Timings: Timings{TurnSec: p.TurnTimeSeconds, ActionIntervalSec: r.Timings.ActionIntervalSec,
			BotFollowUpDelaySec: r.Timings.BotFollowUpDelaySec, SkipTurnDelaySec: r.Timings.SkipTurnDelaySec},
		SkillEnergy:        SkillEnergy{Max: r.SkillEnergy.Max, PerTurn: r.SkillEnergy.PerTurn, OnHit: r.SkillEnergy.OnHit},
		SkillRanks:         SkillRanks{ByTierWord: tierRanks, Default: r.SkillRanks.Default, Max: r.MaxSkillRank()},
		Weapons:            weapons(snap),
		Skills:             skills(snap),
		Characters:         characters(snap),
		DefaultCharacterID: r.Player.DefaultCharacterID,
		Items:              items(snap),
		ItemRules:          ItemRules{MaxLoadout: r.Items.MaxLoadout, AirStrike: AirStrike{Angle: r.Items.AirStrikeAngle, Power: r.Items.AirStrikePower}},
		StatusEffects:      statusEffects(snap),
		Rank:               rank(snap, bands),
		Levels:             levels(settings[character.SettingsKeyLevels], r.Player.ExpPerLevel),
		Equipment:          equipmentData(snap),
		Economy:            Economy{CoinsPerGem: r.Economy.CoinsPerGem},
	}
	mm := matchmaker.DefaultMatchmakingConfig()
	if raw, ok := settings["matchmaking"]; ok {
		mm = matchmaker.ParseMatchmakingConfig(raw)
	}
	slots := matchmaker.TeamSlots
	out.Matchmaking = Matchmaking{Queue: match.RankedMode, Slots: 2 * slots, TeamSize: slots,
		BotFillAfterSec: mm.BotFillAfter, MatchStartDelaySec: mm.MatchStartDelay, Tips: nonNil(append([]string(nil), snap.Tips...))}
	out.Modes = Modes{Lobby: []LobbyMode{}, Match: []MatchMode{}}
	for _, m := range snap.Modes {
		if m.Kind == "lobby" {
			out.Modes.Lobby = append(out.Modes.Lobby, LobbyMode{ModeID: m.ModeID, Name: m.Name, Description: m.Description, Queue: m.Queue, Bot: m.Bot})
		} else {
			out.Modes.Match = append(out.Modes.Match, MatchMode{ModeID: m.ModeID, Name: m.Name})
		}
	}
	out.MapThemes = MapThemes{Default: gd.DefaultMapTheme, Themes: append([]string(nil), gd.MapThemes...)}
	out.GunKinds = []GunKind{}
	for _, g := range snap.GunKinds {
		out.GunKinds = append(out.GunKinds, GunKind{ID: g.ID, Name: g.Name})
	}
	return out
}

// weapons are the config_weapons rows by id, with the crater a normal shot carves.
func weapons(snap *gd.Snapshot) []Weapon {
	out := make([]Weapon, 0, len(snap.Data.Weapons))
	for _, w := range snap.Data.Weapons {
		out = append(out, Weapon{WeaponID: w.WeaponID, Name: w.Name, Damage: w.Damage, ExplosionRadius: w.ExplosionRadius,
			TerrainDamage: w.TerrainDamage, CraterRadius: match.WeaponCraterRadius(snap, w.ExplosionRadius, w.TerrainDamage),
			ProjectileWeight: w.ProjectileWeight, WindInfluence: w.WindInfluence, MultiHit: w.MultiHit})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WeaponID < out[j].WeaponID })
	return out
}

// characters are the config_characters rows in display order (sort_order, then id).
func characters(snap *gd.Snapshot) []Character {
	out := make([]Character, 0, len(snap.Data.Characters))
	order := map[string]int{}
	for _, c := range snap.Data.Characters {
		order[c.CharacterID] = c.SortOrder
		out = append(out, Character{CharacterID: c.CharacterID, Name: c.Name, Role: c.Role, RoleName: c.RoleName, Playstyle: c.Playstyle,
			WeaponID: c.WeaponID, SkillID: c.SkillID, GunKind: c.GunKind, HP: c.HP, Damage: c.Damage, Mobility: c.Mobility,
			Defense: c.Defense, SkillPower: c.SkillPower, TerrainDamage: c.TerrainDamage, Difficulty: c.Difficulty})
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := order[out[i].CharacterID], order[out[j].CharacterID]; a != b {
			return a < b
		}
		return out[i].CharacterID < out[j].CharacterID
	})
	return out
}

// skills are the config_skills rows in their character's order (a skill whose character has no
// row last), then by id, with their rules.
func skills(snap *gd.Snapshot) []Skill {
	type keyed struct {
		missing bool
		order   int
		s       Skill
	}
	var rows []keyed
	for _, s := range snap.Data.Skills {
		c, ok := snap.Data.Characters[s.CharacterID]
		desc := s.DescriptionTemplate
		if desc == "" {
			desc = s.Description
		}
		rules := match.SkillRulesOf(snap, s)
		sk := Skill{SkillID: s.SkillID, CharacterID: s.CharacterID, Name: s.Name, Description: desc, EffectType: s.EffectType,
			StatusEffectID: s.StatusEffectID, AimKind: rules.AimKind, CooldownTurn: s.CooldownTurn, ProjectileCount: s.ProjectileCount,
			DamageMultiplier: s.DamageMultiplier, Params: rules.Params, Ranks: make([]SkillRank, 0, len(rules.Ranks))}
		for rank, values := range rules.Ranks {
			sk.Ranks = append(sk.Ranks, SkillRank{Rank: rank, Values: values})
		}
		rows = append(rows, keyed{missing: !ok, order: c.SortOrder, s: sk})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.missing != b.missing {
			return !a.missing
		}
		if a.order != b.order {
			return a.order < b.order
		}
		return a.s.SkillID < b.s.SkillID
	})
	out := make([]Skill, 0, len(rows))
	for _, k := range rows {
		out = append(out, k.s)
	}
	return out
}

// items are the config_items rows in loadout order (sort_order, then id): the Vietnamese name
// (the config name without one) and how each is used.
func items(snap *gd.Snapshot) []Item {
	type keyed struct {
		order int
		it    Item
	}
	var rows []keyed
	for _, it := range snap.Data.Items {
		name := it.DisplayName
		if name == "" {
			name = it.Name
		}
		out := Item{ItemID: it.ItemID, Name: name, Type: it.Type, TargetType: it.TargetType, Value: it.Value,
			MaxUsePerMatch: it.MaxUsePerMatch, Cooldown: it.Cooldown}
		out.Instant, out.AimKind = itemAim(it.ItemID)
		rows = append(rows, keyed{order: it.SortOrder, it: out})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return rows[i].it.ItemID < rows[j].it.ItemID
	})
	out := make([]Item, 0, len(rows))
	for _, k := range rows {
		out = append(out, k.it)
	}
	return out
}

// itemAim is how an item is used: instant, an arc shot or a map column.
func itemAim(itemID string) (instant bool, aimKind string) {
	switch {
	case match.InstantItem(itemID):
		return true, ItemAimNone
	case match.TargetXItem(itemID):
		return false, ItemAimTargetX
	}
	return false, ItemAimArc
}

// statusEffects are the config_status_effects rows (texts and rules) in order, then any effect
// the match knows that has no row (named by its id).
func statusEffects(snap *gd.Snapshot) []StatusEffect {
	out := make([]StatusEffect, 0, len(snap.Status))
	for _, e := range snap.Status {
		rule := snap.StatusRule(e.EffectID)
		out = append(out, StatusEffect{EffectID: e.EffectID, Name: e.Name, Marker: e.Marker, Banner: e.Banner,
			Bad: rule.Bad, SkipsTurn: rule.SkipsTurn, BlocksMove: rule.BlocksMove})
	}
	return out
}

// rank is the rating bands with the config_rank_tiers names (the tier id when it has none).
func rank(snap *gd.Snapshot, bands []ranked.Band) Rank {
	elo, _ := ranked.ParseEloConfig(snap.Settings["elo"])
	names := map[string]string{}
	for _, t := range snap.RankTiers {
		names[t.Tier] = t.Name
	}
	out := Rank{Tiers: []RankTier{}, DefaultRating: elo.DefaultRating, RankedMode: match.RankedMode}
	for _, b := range bands {
		name := names[b.Tier]
		if name == "" {
			name = b.Tier
		}
		out.Tiers = append(out.Tiers, RankTier{Tier: b.Tier, Name: name, MinRating: b.MinRating,
			Divisions: b.Divisions, DivisionSize: b.DivisionSize})
	}
	return out
}

// levels is the character_levels curve, from level 1 (0 EXP) up, and the player EXP rule.
func levels(raw string, expPerLevel int) Levels {
	entries := []LevelEntry{{Level: 1, ExpRequired: 0}}
	for _, e := range character.LoadLevelsConfig(raw).Levels {
		if e.Level > 1 {
			entries = append(entries, LevelEntry{Level: e.Level, ExpRequired: e.ExpRequired})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Level < entries[j].Level })
	return Levels{
		Character: CharacterLevels{Levels: entries, MaxLevel: entries[len(entries)-1].Level},
		Player:    PlayerLevels{ExpPerLevel: expPerLevel},
	}
}

// equipmentData is the upgrade rules, the game_settings equipment rows (a missing row is ""
// and the parser's default) and the tiers.
func equipmentData(snap *gd.Snapshot) Equipment {
	e, settings := snap.Rules.Equipment, snap.Settings
	ms := battlestats.MilestonesFromSettings(settings)
	ranks := snap.Rules.SkillRanks.ByTierWord
	tiers := make([]EquipmentTier, 0, len(snap.EquipTiers))
	for _, t := range snap.EquipTiers {
		tiers = append(tiers, EquipmentTier{Tier: t.Tier, Name: t.Name, Kind: t.Kind, IDSuffix: t.IDSuffix, SkillRank: ranks[t.Tier]})
	}
	slots := make([]EquipmentSlot, 0, len(snap.Slots))
	for _, sl := range snap.Slots {
		slots = append(slots, EquipmentSlot{ID: sl.ID, Name: sl.Name})
	}
	words := make([]TypeWord, 0, len(snap.TypeWords))
	for _, w := range snap.TypeWords {
		words = append(words, TypeWord{Word: w.Word, Name: w.Name, GunKind: w.GunKind})
	}
	return Equipment{
		UpgradeStepPerLevel: e.UpgradeStepPerLevel, MaxUpgradeLevel: e.MaxUpgradeLevel,
		Milestones:    []Milestone{{6, ms.Bonus6}, {10, ms.Bonus10}, {14, ms.Bonus14}, {16, ms.Bonus16}},
		MaxStoneLevel: e.MaxStoneLevel, MergeMinCount: e.MergeMinCount, MergeMaxCount: e.MergeMaxCount,
		MergeRatePerItem:            equipment.ParseMergeRatePerItem(settings["equipment.merge_rate_per_item"]),
		SellRate:                    equipment.ParseSellRate(settings["equipment.sell_rate"]),
		WeaponUpgradeCostMultiplier: equipment.ParseWeaponUpgradeCostMultiplier(settings["equipment.weapon_upgrade_cost_multiplier"]),
		Tiers:                       tiers,
		Slots:                       slots,
		TypeWords:                   words,
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
