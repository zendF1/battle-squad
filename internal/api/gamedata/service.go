package gamedata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"battle-squad/internal/api/character"
	"battle-squad/internal/api/equipment"
	gd "battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Get builds GET /gamedata from the config rows (read now, so an admin edit shows on the next
// request) and the accessor layer (rules.go), with its version.
func (s *Service) Get(ctx context.Context) (*GameData, error) {
	src, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	out := build(src)
	out.Version = version(out)
	return out, nil
}

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

func build(src *source) *GameData {
	r := matchRules()
	ct := clientRules()
	p := gd.PhysicsFromSettings(src.Settings)
	maxItems, defaultChar := loadoutRules()
	out := &GameData{
		Physics: Physics{
			Gravity: p.Gravity, SpeedPerPower: p.ProjectileSpeedMultiplier, WindScale: p.WindScale,
			TimeStep: p.TimeStep, PathRecordStep: p.PathRecordStep, MaxFlightSec: p.MaxFlightSeconds,
			PlayerHitRadius: p.PlayerHitRadius, CollisionStepPx: r.CollisionStepPx, ShotOriginLift: r.ShotOriginLift,
			OutOfBounds: OutOfBounds{Left: r.OutOfBoundsLeft, Right: r.OutOfBoundsRight, Below: r.OutOfBoundsBelow},
		},
		Aim: Aim{PowerMin: 0, PowerMax: r.MaxShotPower, AngleMin: ct.AngleMin, AngleMax: ct.AngleMax,
			NoAimAngle: r.NoAimAngle, NoAimPower: r.NoAimPower},
		AimAid: AimAid{GuideArcLength: ct.GuideArcLength, DefaultTier: rankBands()[0].Tier, HitTolerance: ct.HitTolerance},
		Combat: Combat{DefenseK: p.DefenseK, PowerShotFactor: r.PowerShotFactor, LavaDamage: r.LavaDamage},
		Terrain: Terrain{
			PlayerHeight: r.PlayerHeight, MaxClimbPerStep: r.MaxClimbPerStep, CraterPxPerTerrainDamage: r.CraterPxPerTD,
			BowlDepth: r.BowlDepth, BowlCeiling: r.BowlCeiling, MaxTerrainDamageBonusPercent: r.MaxTerrainBonus,
			FallDamageThresholdPx: r.FallThresholdPx, FallDamagePerPx: r.FallDamagePerPx,
		},
		Movement: Movement{PxPerEnergy: r.MovePxPerEnergy, EnergyPerPx: r.MoveEnergyPerPx,
			DefaultMoveEnergy: defaultMoveEnergy(), NetMoveEnergyCap: r.NetMoveEnergyCap},
		Timings: Timings{TurnSec: r.TurnSeconds, ActionIntervalSec: r.ActionIntervalSec,
			BotFollowUpDelaySec: r.BotFollowUpSec, SkipTurnDelaySec: r.SkipTurnDelaySec},
		SkillEnergy:        SkillEnergy{Max: r.SkillEnergyMax, PerTurn: r.SkillEnergyTurn, OnHit: r.SkillEnergyOnHit},
		SkillRanks:         SkillRanks{ByTierWord: r.WeaponTierRanks, Default: 0, Max: r.MaxSkillRank},
		Weapons:            make([]Weapon, 0, len(src.Weapons)),
		Skills:             make([]Skill, 0, len(src.Skills)),
		Characters:         nonNil(src.Characters),
		DefaultCharacterID: defaultChar,
		Items:              make([]Item, 0, len(src.Items)),
		ItemRules:          ItemRules{MaxLoadout: maxItems, AirStrike: AirStrike{Angle: r.AirStrikeAngle, Power: r.AirStrikePower}},
		Economy:            Economy{CoinsPerGem: ct.CoinsPerGem},
	}
	for _, w := range src.Weapons {
		w.CraterRadius = match.WeaponCraterRadius(w.ExplosionRadius, w.TerrainDamage)
		out.Weapons = append(out.Weapons, w)
	}
	for _, sk := range src.Skills {
		rules := skillRules(sk.EffectType)
		sk.AimKind, sk.Params = rules.AimKind, rules.Params
		sk.Ranks = make([]SkillRank, 0, len(rules.Ranks))
		for rank, values := range rules.Ranks {
			sk.Ranks = append(sk.Ranks, SkillRank{Rank: rank, Values: values})
		}
		out.Skills = append(out.Skills, sk)
	}
	for _, it := range src.Items {
		it.Instant, it.AimKind = itemAim(it.ItemID)
		out.Items = append(out.Items, it)
	}
	out.StatusEffects = statusEffects(src.StatusEffects)
	out.Rank = rank(src)
	out.Levels = levels(src.Settings[character.SettingsKeyLevels])
	out.Equipment = equipmentData(src)
	slots := teamSlots()
	out.Matchmaking = Matchmaking{Queue: rankedQueue(), Slots: 2 * slots, TeamSize: slots,
		BotFillAfterSec: src.Matchmaking.BotFillAfter, MatchStartDelaySec: src.Matchmaking.MatchStartDelay,
		Tips: nonNil(src.Tips)}
	out.Modes = Modes{Lobby: []LobbyMode{}, Match: []MatchMode{}}
	for _, m := range src.Modes {
		if m.Kind == "lobby" {
			out.Modes.Lobby = append(out.Modes.Lobby, LobbyMode{ModeID: m.ModeID, Name: m.Name, Description: m.Description, Queue: m.Queue, Bot: m.Bot})
		} else {
			out.Modes.Match = append(out.Modes.Match, MatchMode{ModeID: m.ModeID, Name: m.Name})
		}
	}
	def, themes := mapThemes()
	out.MapThemes = MapThemes{Default: def, Themes: themes}
	return out
}

// statusEffects are the config_status_effects rows (their texts) in order, then any effect the
// match knows that has no row (named by its id), each with the match's rules.
func statusEffects(rows []statusText) []StatusEffect {
	out := make([]StatusEffect, 0, len(rows))
	seen := map[string]bool{}
	add := func(t statusText) {
		rule := statusRule(t.EffectID)
		out = append(out, StatusEffect{EffectID: t.EffectID, Name: t.Name, Marker: t.Marker, Banner: t.Banner,
			Bad: rule.Bad, SkipsTurn: rule.SkipsTurn, BlocksMove: rule.BlocksMove})
		seen[t.EffectID] = true
	}
	for _, t := range rows {
		add(t)
	}
	for _, id := range statusEffectIDs() {
		if !seen[id] {
			add(statusText{EffectID: id, Name: id})
		}
	}
	return out
}

// rank is ranked.Bands with the config_rank_tiers names (the tier id when it has none).
func rank(src *source) Rank {
	out := Rank{Tiers: []RankTier{}, DefaultRating: src.Elo.DefaultRating, RankedMode: rankedQueue()}
	for _, b := range rankBands() {
		name := src.RankTiers[b.Tier]
		if name == "" {
			name = b.Tier
		}
		out.Tiers = append(out.Tiers, RankTier{Tier: b.Tier, Name: name, MinRating: b.MinRating,
			Divisions: b.Divisions, DivisionSize: b.DivisionSize})
	}
	return out
}

// levels is the character_levels curve, from level 1 (0 EXP) up, and the player EXP rule.
func levels(raw string) Levels {
	entries := []LevelEntry{{Level: 1, ExpRequired: 0}}
	for _, e := range character.LoadLevelsConfig(raw).Levels {
		if e.Level > 1 {
			entries = append(entries, LevelEntry{Level: e.Level, ExpRequired: e.ExpRequired})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Level < entries[j].Level })
	return Levels{
		Character: CharacterLevels{Levels: entries, MaxLevel: entries[len(entries)-1].Level},
		Player:    PlayerLevels{ExpPerLevel: playerExpPerLevel()},
	}
}

// equipmentData is the upgrade rules, the game_settings equipment rows (a missing row is ""
// and the parser's default) and the tiers.
func equipmentData(src *source) Equipment {
	step, maxLevel, maxStone, mergeMin, mergeMax := equipmentRules()
	ms := src.Milestones
	ranks := matchRules().WeaponTierRanks
	tiers := make([]EquipmentTier, 0, len(src.EquipTiers))
	for _, t := range src.EquipTiers {
		t.SkillRank = ranks[t.Tier]
		tiers = append(tiers, t)
	}
	return Equipment{
		UpgradeStepPerLevel: step, MaxUpgradeLevel: maxLevel,
		Milestones:    []Milestone{{6, ms.Bonus6}, {10, ms.Bonus10}, {14, ms.Bonus14}, {16, ms.Bonus16}},
		MaxStoneLevel: maxStone, MergeMinCount: mergeMin, MergeMaxCount: mergeMax,
		MergeRatePerItem:            equipment.ParseMergeRatePerItem(src.Settings["equipment.merge_rate_per_item"]),
		SellRate:                    equipment.ParseSellRate(src.Settings["equipment.sell_rate"]),
		WeaponUpgradeCostMultiplier: equipment.ParseWeaponUpgradeCostMultiplier(src.Settings["equipment.weapon_upgrade_cost_multiplier"]),
		Tiers:                       tiers,
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
