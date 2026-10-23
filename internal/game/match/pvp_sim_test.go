package match

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
)

// The PvP character balance runs (PVP_SIM=<matches per pairing>; PVP_SIM_CHAR=<id> the character
// played against every other, tide by default; PVP_SIM_ALL=1 every pairing; PVP_SIM_NO_ELEMENTS=1
// without the element multipliers; PVP_SIM_LEVEL the
// level, 30 by default; with TEST_POSTGRES_DSN the bots carry the migrated guns): 1v1 matches of
// two bots (gold tier aim) at the same level with the stat points spread evenly, the best gun of
// that level and medkit + teleport, on the PvP maps in turn, elements included. It logs each
// character's win rate per pairing; it never fails on it.
func TestPvPCharacterSim(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("PVP_SIM"))
	if n <= 0 {
		t.Skip("set PVP_SIM=<matches per pairing> to run the PvP character balance runs")
	}
	loadConfigs(t)
	if os.Getenv("PVP_SIM_NO_ELEMENTS") != "" { // the same runs without the element multipliers
		r := gamedata.Current().Rules.Clone()
		r.Elements = gamedata.ElementRules{}
		gamedata.UseForTest(t, gamedata.Current().WithRules(r))
	}
	weapons := simWeapons(t)
	gd := gamedata.Current()
	level := 30
	if l, err := strconv.Atoi(os.Getenv("PVP_SIM_LEVEL")); err == nil && l > 0 {
		level = l
	}
	var chars, maps []string
	for id, c := range gd.Data.Characters {
		if _, ok := gd.Data.Weapons[c.WeaponID]; ok {
			chars = append(chars, id)
		}
	}
	for id, m := range gd.Data.Maps {
		if a, b := m.TeamSpawns(); !gd.IsBossArena(id) && len(a) > 0 && len(b) > 0 {
			maps = append(maps, id)
		}
	}
	sort.Strings(chars)
	sort.Strings(maps)
	focus := os.Getenv("PVP_SIM_CHAR")
	if focus == "" {
		focus = "tide"
	}
	type pairing struct{ a, b string }
	var pairs []pairing
	for i, a := range chars {
		for _, b := range chars[i+1:] {
			if os.Getenv("PVP_SIM_ALL") != "" || a == focus || b == focus {
				if b == focus {
					a, b = b, a
				}
				pairs = append(pairs, pairing{a, b})
			}
		}
	}
	total := map[string][2]int{} // character → wins, matches
	for _, pr := range pairs {
		wins, draws, turns := 0, 0, 0
		for i := 0; i < n; i++ {
			m := simPvPMatch(t, gd, pr.a, pr.b, maps[i%len(maps)], level, weapons, int64(i))
			turns += playBossMatch(t, m, 400)
			a := m.State.Players["a"]
			b := m.State.Players["b"]
			switch {
			case a.IsAlive && !b.IsAlive:
				wins++
			case a.IsAlive == b.IsAlive:
				draws++
			}
		}
		losses := n - wins - draws
		ta, tb := total[pr.a], total[pr.b]
		total[pr.a] = [2]int{ta[0] + wins, ta[1] + n}
		total[pr.b] = [2]int{tb[0] + losses, tb[1] + n}
		t.Logf("%s vs %s (level %d): %d-%d (%d unfinished), %s wins %.0f %%, %.0f turns a match", pr.a, pr.b, level, wins, losses, draws, pr.a,
			100*float64(wins)/float64(n), float64(turns)/float64(n))
	}
	var lines []string
	for _, c := range chars {
		if v := total[c]; v[1] > 0 {
			lines = append(lines, fmt.Sprintf("%s %d / %d (%.0f %%)", c, v[0], v[1], 100*float64(v[0])/float64(v[1])))
		}
	}
	t.Logf("wins: %s", strings.Join(lines, ", "))
}

// simPvPMatch is a 1v1 of bots a (team 1) and b (team 2) on mapID (see TestPvPCharacterSim).
func simPvPMatch(t *testing.T, gd *gamedata.Snapshot, a, b, mapID string, level int, weapons []simWeapon, seed int64) *Match {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	t1, t2 := gd.Data.Maps[mapID].TeamSpawns()
	prog := character.LoadProgressionConfig(gd.Settings[character.SettingsKeyProgression])
	var ps []*BattlePlayerState
	for i, ch := range []string{a, b} {
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
		sp := t1[0]
		if i == 1 {
			sp = t2[0]
		}
		ps = append(ps, &BattlePlayerState{PlayerID: []string{"a", "b"}[i], TeamID: i + 1, CharacterID: ch, WeaponItemID: weaponID, WeaponUpgradeLevel: upgrade,
			HP: s.HP, MaxHP: s.HP, Defense: s.Defense, DamageBonus: s.DamageBonus, MoveEnergy: s.TurnMoveEnergy(), MaxMoveEnergy: s.TurnMoveEnergy(),
			MoveEnergyBonus: s.MoveEnergyBonus, CritChance: s.CritChance, SkillPowerBonus: s.SkillPowerBonus, TerrainDamageBonus: s.TerrainDamageBonus,
			Position: Vector2{X: sp.X, Y: sp.Y}, IsAlive: true, IsBot: true, Items: append([]string(nil), gd.Rules.Bots.DefaultItems...),
			StatusEffects: []StatusEffect{}})
	}
	watch := &ws.Client{PlayerID: "a", Send: make(chan ws.Message, 1<<16)}
	m := NewMatchWithData(gd, fmt.Sprintf("pvp_%s_%s_%d", a, b, seed), "r1", "pvp_1v1", mapID, ps, map[string]*ws.Client{"a": watch}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.BotTier = matchmaker.DefaultBotDifficultyConfig().Tiers["gold"]
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	return m
}
