package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database/dbtest"
)

// The boss balance runs (BOSS_SIM=<matches per boss>; with TEST_POSTGRES_DSN the bots carry the
// guns of the migrated config_equipment_items, as the room's bot outfitter gives them): 4 bots
// at the boss's recommended level (gold tier aim), each a distinct character (4 of the roster at
// random; BOSS_SIM_PARTY=<id>,<id>,… fixes the party, shuffled the same way, e.g. four of the
// element that beats the boss; BOSS_SIM_NO_ELEMENTS=1 plays without the element multipliers) with
// the stat points of that level spread evenly, the best gun of that level and medkit + teleport,
// play the boss to the end. It logs the win rate per boss; it never fails on it.
func TestBossBalanceSim(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("BOSS_SIM"))
	if n <= 0 {
		t.Skip("set BOSS_SIM=<matches per boss> to run the boss balance runs")
	}
	loadConfigs(t)
	if os.Getenv("BOSS_SIM_NO_ELEMENTS") != "" { // the same runs without the element multipliers
		r := gamedata.Current().Rules.Clone()
		r.Elements = gamedata.ElementRules{}
		gamedata.UseForTest(t, gamedata.Current().WithRules(r))
	}
	weapons := simWeapons(t)
	only := os.Getenv("BOSS_SIM_BOSS")
	for _, cfg := range gamedata.Current().Bosses {
		if only != "" && cfg.BossID != only {
			continue
		}
		wins, rounds, hpLeft := 0, 0, 0
		var lines []string
		deaths := map[string]int{}
		for i := 0; i < n; i++ {
			m, watch := simBossMatch(t, cfg, weapons, int64(i))
			playBossMatch(t, m, 600)
			simDeaths(drainMsgs(watch), deaths)
			boss := m.bossPlayer()
			won := !boss.IsAlive
			if won {
				wins++
			}
			rounds += m.boss.round
			hpLeft += boss.HP
			alive, partyHP, toBoss, byMinions := 0, 0, 0, 0
			for _, id := range m.boss.party {
				p := m.State.Players[id]
				partyHP += p.MaxHP
				toBoss += p.BossDamage
				if p.IsAlive {
					alive++
				}
			}
			for _, p := range m.State.Players {
				if p.Role == RoleMinion {
					byMinions += p.DamageDealt
				}
			}
			lines = append(lines, fmt.Sprintf("won %v round %d boss hp %d party alive %d (max hp %d), damage to the boss %d, by the boss %d, by minions %d",
				won, m.boss.round, boss.HP, alive, partyHP, toBoss, boss.DamageDealt, byMinions))
		}
		t.Logf("%s (level %d, hp %d): %d / %d won (%.0f %%), round %.1f, boss hp left %.0f\n  %s", cfg.BossID, cfg.RecommendedLevel, cfg.HP,
			wins, n, 100*float64(wins)/float64(n), float64(rounds)/float64(n), float64(hpLeft)/float64(n), strings.Join(lines, "\n  "))
		t.Logf("%s party deaths: %v", cfg.BossID, deaths)
	}
}

// simWeapon is a config_equipment_items weapon (the room's botWeaponOption).
type simWeapon struct {
	ItemID, Category, Tier, CharacterID string
	RequiredLevel                       int
	HP, Damage, Defense, MoveEnergy     int
	Crit                                float64
}

// simWeapons are the active weapons of a migrated schema (nil without TEST_POSTGRES_DSN).
func simWeapons(t *testing.T) []simWeapon {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Log("no TEST_POSTGRES_DSN: the bots play with their basic guns")
		return nil
	}
	_, self, _, _ := runtime.Caller(0)
	src, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "..", "cmd", "migrate", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(string(src), "\n") {
		if i := strings.Index(line, `filepath.Join("migrations", "`); i >= 0 {
			rest := line[i+len(`filepath.Join("migrations", "`):]
			names = append(names, rest[:strings.Index(rest, `"`)])
		}
	}
	db := dbtest.NewSchema(t, 0, names...)
	rows, err := db.Pool.Query(context.Background(), `
		SELECT item_id, category, COALESCE(tier, ''), COALESCE(character_id, ''), required_level,
		       stat_hp, stat_damage, stat_defense, stat_move_energy, stat_crit
		FROM config_equipment_items WHERE slot = 'weapon' AND is_active = TRUE ORDER BY item_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []simWeapon
	for rows.Next() {
		var w simWeapon
		if err := rows.Scan(&w.ItemID, &w.Category, &w.Tier, &w.CharacterID, &w.RequiredLevel, &w.HP, &w.Damage, &w.Defense, &w.MoveEnergy, &w.Crit); err != nil {
			t.Fatal(err)
		}
		out = append(out, w)
	}
	return out
}

// simBossMatch is a boss match of 4 bots at the boss's recommended level (see TestBossBalanceSim).
func simBossMatch(t *testing.T, cfg gamedata.BossConfig, weapons []simWeapon, seed int64) (*Match, *ws.Client) {
	t.Helper()
	gd := gamedata.Current()
	rng := rand.New(rand.NewSource(seed))
	spawns := gamedata.BossPartySpawns(gd.Data.Maps[cfg.MapID], 4, rng.Intn)
	var chars []string
	for id, c := range gd.Data.Characters {
		if _, ok := gd.Data.Weapons[c.WeaponID]; ok {
			chars = append(chars, id)
		}
	}
	sort.Strings(chars)
	if party := os.Getenv("BOSS_SIM_PARTY"); party != "" {
		chars = strings.Split(party, ",")
	}
	rng.Shuffle(len(chars), func(i, j int) { chars[i], chars[j] = chars[j], chars[i] })
	prog := character.LoadProgressionConfig(gd.Settings[character.SettingsKeyProgression])
	level := cfg.RecommendedLevel
	var ps []*BattlePlayerState
	for i := 0; i < 4; i++ {
		ch := chars[i%len(chars)]
		total := max(level-1, 0) * prog.PointsPerLevel
		var pts [6]int
		for k := range pts {
			pts[k] = total / 6
			if k < total%6 {
				pts[k]++
			}
		}
		var eq battlestats.EquipmentPoints
		weaponID, upgrade := "", 0
		if w, ok := simBestWeapon(weapons, ch, level, rng); ok {
			hi := min(int(math.Floor(float64(w.RequiredLevel)*gd.Rules.Bots.WeaponUpgradePerLevel)), gd.Rules.Equipment.MaxUpgradeLevel)
			lo := min(max(gd.Rules.Bots.WeaponUpgradeMin, 0), hi)
			upgrade = lo + rng.Intn(hi-lo+1)
			weaponID = w.ItemID
			eq = battlestats.SumEquipment([]battlestats.EquippedItem{{ItemID: w.ItemID, Slot: "weapon", HP: w.HP, Damage: w.Damage,
				Defense: w.Defense, Crit: w.Crit, MoveEnergy: w.MoveEnergy, UpgradeLevel: upgrade, Category: w.Category, Tier: w.Tier}},
				nil, battlestats.MilestonesFromSettings(gd.Settings))
		}
		s := battlestats.Compute(ch, battlestats.Points{HP: pts[0], Damage: pts[1], Defense: pts[2], Mobility: pts[3], SkillPower: pts[4],
			TerrainDamage: pts[5]}, prog.Multipliers(), eq).Total
		ps = append(ps, &BattlePlayerState{PlayerID: fmt.Sprintf("p%d", i), TeamID: 1, CharacterID: ch, WeaponItemID: weaponID, WeaponUpgradeLevel: upgrade,
			HP: s.HP, MaxHP: s.HP, Defense: s.Defense, DamageBonus: s.DamageBonus, MoveEnergy: s.TurnMoveEnergy(), MaxMoveEnergy: s.TurnMoveEnergy(),
			MoveEnergyBonus: s.MoveEnergyBonus, CritChance: s.CritChance, SkillPowerBonus: s.SkillPowerBonus, TerrainDamageBonus: s.TerrainDamageBonus,
			Position: Vector2{X: spawns[i].X, Y: spawns[i].Y}, IsAlive: true, IsBot: true, Items: append([]string(nil), gd.Rules.Bots.DefaultItems...),
			StatusEffects: []StatusEffect{}})
	}
	ps = append(ps, NewBossSide(gd, cfg)...)
	watch := &ws.Client{PlayerID: "p0", Send: make(chan ws.Message, 1<<16)}
	m := NewMatchWithData(gd, fmt.Sprintf("sim_%s_%d", cfg.BossID, seed), "r1", cfg.Queue(), cfg.MapID, ps, map[string]*ws.Client{"p0": watch}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.SetupBoss(cfg)
	m.BotTier = matchmaker.DefaultBotDifficultyConfig().Tiers["gold"]
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	return m, watch
}

// simDeaths tallies the party's deaths in msgs by what killed them (the shooter's role and the
// cause).
func simDeaths(msgs []ws.Message, deaths map[string]int) {
	for _, msg := range msgs {
		switch msg.Event {
		case "ShotResolved":
			var s ShotResolved
			_ = json.Unmarshal(msg.Data, &s)
			who := "party"
			if strings.HasPrefix(s.ShooterID, "boss_") {
				who = "boss"
			} else if strings.HasPrefix(s.ShooterID, "minion_") {
				who = "minion"
			}
			for _, e := range s.Events {
				if e.Type == "die" && strings.HasPrefix(e.PlayerID, "p") {
					deaths[who+":"+s.SkillID+s.ActionMode+":"+e.Cause]++
				}
			}
		case "PlayerDamaged":
			var ds []map[string]any
			_ = json.Unmarshal(msg.Data, &ds)
			for _, d := range ds {
				if id, _ := d["playerId"].(string); strings.HasPrefix(id, "p") && d["isAlive"] == false {
					deaths[fmt.Sprintf("turn start:%v", d["type"])]++
				}
			}
		}
	}
}

// simBestWeapon is the gun a bot of level carries on ch (the room's outfitter: the normal and
// the crafted weapon of the highest required level at or below level, one of them at random).
func simBestWeapon(weapons []simWeapon, ch string, level int, rng *rand.Rand) (simWeapon, bool) {
	var normal, crafted *simWeapon
	for i := range weapons {
		w := &weapons[i]
		if w.RequiredLevel > level || (w.CharacterID != "" && w.CharacterID != ch) {
			continue
		}
		if w.Category == "crafted" {
			if crafted == nil || w.RequiredLevel > crafted.RequiredLevel {
				crafted = w
			}
		} else if normal == nil || w.RequiredLevel > normal.RequiredLevel || (normal.CharacterID == "" && w.CharacterID != "" && w.RequiredLevel == normal.RequiredLevel) {
			normal = w
		}
	}
	var opts []simWeapon
	for _, w := range []*simWeapon{normal, crafted} {
		if w != nil {
			opts = append(opts, *w)
		}
	}
	if len(opts) == 0 {
		return simWeapon{}, false
	}
	return opts[rng.Intn(len(opts))], true
}
