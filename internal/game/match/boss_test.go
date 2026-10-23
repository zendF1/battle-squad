package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
)

// bossConfig is a default boss (the YAML game data carries gamedata.DefaultBosses).
func bossConfig(t *testing.T, id string) gamedata.BossConfig {
	t.Helper()
	b, ok := gamedata.Current().Boss(id)
	if !ok {
		t.Fatalf("boss %s not loaded", id)
	}
	return b
}

// bossMatch is a match against boss id on its arena: party bot teammates p0..p(n-1) (rookie,
// tanko, spark, flora) at the party spawns, one connected client "watch" for p0, and the
// boss's side; rewards are stubbed.
func bossMatch(t *testing.T, id string, party int) (*Match, *ws.Client) {
	t.Helper()
	cfg := bossConfig(t, id)
	gd := gamedata.Current()
	spawns := gamedata.BossPartySpawns(gd.Data.Maps[cfg.MapID], max(party, 1), rand.New(rand.NewSource(7)).Intn)
	chars := []string{"rookie", "tanko", "spark", "flora"}
	var ps []*BattlePlayerState
	for i := 0; i < party; i++ {
		ch := gd.Data.Characters[chars[i]]
		ps = append(ps, &BattlePlayerState{PlayerID: fmt.Sprintf("p%d", i), TeamID: 1, CharacterID: ch.CharacterID, HP: ch.HP, MaxHP: ch.HP,
			Defense: ch.Defense, Position: Vector2{X: spawns[i].X, Y: spawns[i].Y}, IsAlive: true, IsBot: true, Items: []string{},
			StatusEffects: []StatusEffect{}})
	}
	ps = append(ps, NewBossSide(gd, cfg)...)
	watch := &ws.Client{PlayerID: "p0", Send: make(chan ws.Message, 4096)}
	m := NewMatchWithData(gd, "m_"+id, "r1", cfg.Queue(), cfg.MapID, ps, map[string]*ws.Client{"p0": watch}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.SetupBoss(cfg)
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	return m, watch
}

// drain returns the events sent to c so far, by name, in order.
func drainMsgs(c *ws.Client) []ws.Message {
	var out []ws.Message
	for {
		select {
		case msg := <-c.Send:
			out = append(out, msg)
		default:
			return out
		}
	}
}

func lastEvent(t *testing.T, msgs []ws.Message, name string) json.RawMessage {
	t.Helper()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Event == name {
			return msgs[i].Data
		}
	}
	t.Fatalf("no %s in %d events", name, len(msgs))
	return nil
}

func hasEvent(msgs []ws.Message, name string) bool {
	for _, m := range msgs {
		if m.Event == name {
			return true
		}
	}
	return false
}

// The boss side: the boss with its config stats, role and body, the initial minions with
// theirs (damage replacing the gun's), all bots on team 2 at the arena's spawns.
func TestNewBossSide(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 4)
	boss := m.State.Players["boss_steel_robot"]
	if boss == nil || boss.Role != RoleBoss || boss.BossID != "steel_robot" || boss.HP != 10000 || boss.MaxHP != 10000 ||
		boss.Defense != 60 || boss.BodySize != 3 || boss.TeamID != 2 || !boss.IsBot || boss.DisplayName != "Rô-bốt Thép" {
		t.Fatalf("boss %+v", boss)
	}
	if !m.Terrain.CanStandAt(boss.Position.X, boss.Position.Y) {
		t.Fatalf("boss does not stand at %v", boss.Position)
	}
	drone := m.State.Players["minion_drone_1"]
	spark := gamedata.Current().Data.Weapons[gamedata.Current().Data.Characters["spark"].WeaponID]
	if drone == nil || drone.Role != RoleMinion || drone.MinionID != "drone" || drone.HP != 650 || drone.CharacterID != "spark" ||
		drone.Damage() != 128 || drone.BodySize != 0.7 || drone.TeamID != 2 || drone.DamageBonus != 128-spark.Damage || // 160 × 0.8
		!drone.Flying || drone.Behavior != "bomber" || drone.Movement != "fly" {
		t.Fatalf("drone %+v", drone)
	}
	if m.State.Players["minion_drone_2"] == nil || len(m.State.Players) != 7 {
		t.Fatalf("%d players", len(m.State.Players))
	}
	var snap struct {
		Boss         *BossState `json:"boss"`
		BossShielded *bool      `json:"bossShielded"`
		Players      map[string]struct {
			Role     string  `json:"role"`
			BossID   string  `json:"bossId"`
			MinionID string  `json:"minionId"`
			BodySize float64 `json:"bodySize"`
		} `json:"players"`
	}
	if err := json.Unmarshal(m.snapshotPayload(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Boss == nil || *snap.Boss != (BossState{BossID: "steel_robot", PlayerID: "boss_steel_robot", TurnLimit: 15}) ||
		snap.BossShielded == nil || !*snap.BossShielded || snap.Players["boss_steel_robot"].Role != "boss" ||
		snap.Players["minion_drone_1"].MinionID != "drone" || snap.Players["minion_drone_1"].BodySize != 0.7 || snap.Players["p0"].Role != "" {
		t.Fatalf("snapshot %+v", snap)
	}
}

// Damage is a player's or minion's weapon damage (test helper).
func (p *BattlePlayerState) Damage() int {
	gd := gamedata.Current()
	return gd.Data.Weapons[gd.Data.Characters[p.CharacterID].WeaponID].Damage + p.DamageBonus
}

// Turn order: the party and the minions alternate, the boss last; the boss's turn is a new
// round; summoned minions join before the boss.
func TestBossTurnOrder(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 4)
	order := m.State.TurnOrder
	if len(order) != 7 || order[6] != "boss_steel_robot" || order[1] != "minion_drone_1" || order[3] != "minion_drone_2" ||
		m.State.CurrentPlayerID != order[0] {
		t.Fatalf("order %v current %s", order, m.State.CurrentPlayerID)
	}
	for _, i := range []int{0, 2, 4, 5} {
		if m.State.Players[order[i]].Role != "" {
			t.Fatalf("order %v", order)
		}
	}
	ctx := context.Background()
	m.startTurn(ctx)
	for i := 0; i < 6; i++ {
		m.endTurn(ctx)
	}
	if m.State.CurrentPlayerID != "boss_steel_robot" || m.boss.round != 1 || m.State.Boss.Round != 1 {
		t.Fatalf("current %s round %d", m.State.CurrentPlayerID, m.boss.round)
	}
	if boss := m.State.Players["boss_steel_robot"]; boss.MoveEnergy != 0 || boss.SkillEnergy != 0 {
		t.Fatalf("boss energy %d skill %d", boss.MoveEnergy, boss.SkillEnergy)
	}
	// a minion dies: it is left out once the order is laid out again (a summon)
	killPlayer(m.State.Players["minion_drone_1"])
	m.bossSummon("drone", 3, m.newTimeline()) // 1 alive of maxAlive 3: two more
	order = m.State.TurnOrder
	if len(order) != 8 || order[7] != "boss_steel_robot" || order[1] != "minion_drone_2" || order[3] != "minion_drone_3" || order[5] != "minion_drone_4" {
		t.Fatalf("order after summon %v", order)
	}
	m.endTurn(ctx)
	if m.State.CurrentPlayerID != order[0] || m.State.Players["boss_steel_robot"].SkillEnergy != 0 {
		t.Fatalf("after the boss: %s", m.State.CurrentPlayerID)
	}
}

// While a minion lives the boss takes shieldedDamageFactor × the damage; the last minion's death
// drops the shield (BossShieldChanged) and the boss takes full damage; a summon raises it again.
func TestBossShield(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "steel_robot", 4)
	boss := m.State.Players["boss_steel_robot"]
	if got := m.shieldedDamage(boss, 1000); got != 100 {
		t.Fatalf("shielded damage %d, want 100", got)
	}
	if got := m.shieldedDamage(m.State.Players["minion_drone_1"], 1000); got != 1000 {
		t.Fatalf("minion damage %d", got)
	}

	shooter := m.State.Players["p0"]
	hit := func() int {
		tl := m.newTimeline()
		before := boss.HP
		r := &ProjectileResult{ProjectileID: "x", HitPlayerID: boss.PlayerID, ExplosionPoint: vecPtr(bodyCentre(boss, 20)), ExplosionRadius: 40,
			Path: []ProjectileStep{{Time: 0}, {Time: 1}}}
		m.explodeProjectile(shooter, r, 600, tl)
		m.broadcastShot(&ShotResolved{ShooterID: shooter.PlayerID, ActionMode: "weapon", Projectiles: []*ProjectileResult{r}}, tl)
		return before - boss.HP
	}
	shielded := hit()
	full := int(math.Round(reduceByDefense(600, boss.Defense, gamedata.DefenseK())))
	if shielded != int(math.Round(float64(full)*0.1)) || shooter.BossDamage != shielded {
		t.Fatalf("shielded hit %d (full %d), boss damage %d", shielded, full, shooter.BossDamage)
	}
	drainMsgs(watch)
	killPlayer(m.State.Players["minion_drone_1"])
	killPlayer(m.State.Players["minion_drone_2"])
	if got := hit(); got != full {
		t.Fatalf("unshielded hit %d, want %d", got, full)
	}
	var ev BossShieldChanged
	if err := json.Unmarshal(lastEvent(t, drainMsgs(watch), "BossShieldChanged"), &ev); err != nil || ev.Shielded ||
		ev.BossID != "steel_robot" || *m.State.BossShielded {
		t.Fatalf("shield changed %+v %v", ev, err)
	}

	m.startTurn(context.Background())
	m.State.CurrentPlayerID = boss.PlayerID
	m.boss.round = 2
	if _, ok := m.bossFreeSummon(boss); !ok {
		t.Fatal("no free summon")
	}
	if err := json.Unmarshal(lastEvent(t, drainMsgs(watch), "BossShieldChanged"), &ev); err != nil || !ev.Shielded || !*m.State.BossShielded {
		t.Fatalf("shield up %+v %v", ev, err)
	}
	if got := m.shieldedDamage(boss, 1000); got != 100 {
		t.Fatalf("shielded again %d", got)
	}
}

// shotOf returns the last ShotResolved sent to c.
func shotOf(t *testing.T, c *ws.Client) ShotResolved {
	t.Helper()
	var s ShotResolved
	if err := json.Unmarshal(lastEvent(t, drainMsgs(c), "ShotResolved"), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func eventTypes(s ShotResolved) map[string]int {
	out := map[string]int{}
	for _, e := range s.Events {
		out[e.Type]++
	}
	return out
}

// bossCast makes it the boss's turn and casts skill id.
func bossCast(t *testing.T, m *Match, id string) {
	t.Helper()
	skill, ok := m.boss.cfg.Skill(id)
	if !ok {
		t.Fatalf("no skill %s", id)
	}
	m.State.CurrentPlayerID = m.boss.playerID
	m.resolving = false
	m.castBossSkill(context.Background(), skill)
}

// Boss skills resolve as ShotResolved with the existing event types (rain, explode, damage,
// status, beam) plus summon and lava.
func TestBossSkillEvents(t *testing.T) {
	loadConfigs(t)

	t.Run("rain", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 1)
		p := m.State.Players["p0"]
		bossCast(t, m, "shell_barrage")
		s := shotOf(t, watch)
		ev := eventTypes(s)
		if s.ShooterID != "boss_steel_robot" || s.ActionMode != "skill" || s.SkillID != "shell_barrage" || len(s.Projectiles) != 24 ||
			ev["rain"] != 1 || ev["explode"] < 12 || ev["damage"] < 1 || p.HP >= p.MaxHP {
			t.Fatalf("rain shot %+v events %v hp %d", s.SkillID, ev, p.HP)
		}
		for _, pr := range s.Projectiles {
			if pr.Kind != "steel_shell" || pr.SkillID != "shell_barrage" || pr.ExplosionRadius != 75 || pr.CraterRadius != 21 {
				t.Fatalf("projectile %+v", pr)
			}
		}
		cap := int(math.Round(reduceByDefense(1.0*630, p.Defense, gamedata.DefenseK())))
		if lost := p.MaxHP - p.HP; lost > cap {
			t.Fatalf("lost %d > cap %d", lost, cap)
		}
	})

	t.Run("beam", func(t *testing.T) { // an aimed beam (pattern "", boss v2) still works
		m, watch := bossMatch(t, "steel_robot", 4)
		m.boss.cfg.Skills = append(m.boss.cfg.Skills, gamedata.BossSkill{SkillID: "aimed", Name: "Aimed", Kind: gamedata.BossSkillBeam, Weight: 1,
			Target: gamedata.BossTargetLowestHP, Damage: 1.3, HalfWidth: 16, Decay: 0.85})
		m.State.Players["p2"].HP = 100 // lowest HP: the target
		bossCast(t, m, "aimed")
		s := shotOf(t, watch)
		ev := eventTypes(s)
		if ev["beam"] != 1 || ev["damage"] < 1 || m.State.Players["p2"].IsAlive {
			t.Fatalf("beam events %v p2 %+v", ev, m.State.Players["p2"].HP)
		}
	})

	t.Run("status", func(t *testing.T) {
		m, watch := bossMatch(t, "ice_witch", 1)
		m.boss.statusRollFix = func() float64 { return 0 } // every status roll lands
		bossCast(t, m, "ice_spikes")
		s := shotOf(t, watch)
		if eventTypes(s)["status"] != 1 || !HasEffect(m.State.Players["p0"], "freeze") {
			t.Fatalf("ice spikes events %v effects %+v", eventTypes(s), m.State.Players["p0"].StatusEffects)
		}
	})

	t.Run("summon", func(t *testing.T) {
		m, watch := bossMatch(t, "ice_witch", 4)
		for i := 1; i <= 3; i++ {
			killPlayer(m.State.Players[fmt.Sprintf("minion_snowman_%d", i)])
		}
		m.boss.round = 2
		if _, ok := m.bossFreeSummon(m.bossPlayer()); !ok {
			t.Fatal("no summon")
		}
		s := shotOf(t, watch)
		var summons []ShotEvent
		for _, e := range s.Events {
			if e.Type == "summon" {
				summons = append(summons, e)
			}
		}
		// the wave is dead: five more (maxAlive 5), the whole unit in each event
		e := summons
		if s.ActionMode != ActionSummon || s.SkillID != "" || s.ShooterID != "boss_ice_witch" || len(s.Events) != 5 ||
			len(e) != 5 || e[0].MinionID != "snowman" || e[0].PlayerID != "minion_snowman_4" || e[0].HP == nil || *e[0].HP != 900 ||
			e[0].Position == nil || e[0].MaxHP == nil || *e[0].MaxHP != 900 || e[0].DisplayName != "Người tuyết" || e[0].TeamID != 2 ||
			e[0].BodySize != 0.85 || e[0].Flying == nil || *e[0].Flying || e[0].Behavior != "leaper" || e[0].Movement != "none" {
			t.Fatalf("summon shot %+v", s)
		}
		var raw struct {
			Events []map[string]any `json:"events"`
		}
		_ = json.Unmarshal(mustJSON(t, s), &raw)
		for _, ev := range raw.Events {
			if ev["type"] == "summon" {
				for _, k := range []string{"playerId", "minionId", "position", "hp", "maxHp", "displayName", "teamId", "bodySize", "flying", "behavior", "movement"} {
					if _, ok := ev[k]; !ok {
						t.Fatalf("summon event without %s: %v", k, ev)
					}
				}
			}
		}
		p := m.State.Players["minion_snowman_4"]
		if p == nil || !p.IsAlive || p.Position != *e[0].Position || m.aliveMinions() != 5 {
			t.Fatalf("summoned %+v alive %d", p, m.aliveMinions())
		}
		// full, and the wave lives: no summon due
		m.boss.round = 10
		if _, due := m.summonDue(); due {
			t.Fatal("a summon is due with maxAlive minions")
		}
	})

	t.Run("lava", func(t *testing.T) {
		m, watch := bossMatch(t, "lava_crab", 1)
		p := m.State.Players["p0"]
		bossCast(t, m, "fire_bombs")
		s := shotOf(t, watch)
		ev := eventTypes(s)
		if ev["lava"] < 1 || len(m.State.LavaZones) != ev["lava"] || ev["rain"] != 1 {
			t.Fatalf("fire bombs events %v zones %v", ev, m.State.LavaZones)
		}
		// a party member standing in a pool burns at the start of its turn; the pool dries up
		z := m.State.LavaZones[0]
		if z.Damage != 120 || z.Radius != 40 || z.ExpiresRound != m.boss.round+1 {
			t.Fatalf("zone %+v", z)
		}
		p.Position = z.Position
		p.Shield, p.HP, p.StatusEffects = 0, p.MaxHP, []StatusEffect{}
		m.State.CurrentPlayerID = "p0"
		m.startTurn(context.Background())
		if p.HP != p.MaxHP-120 {
			t.Fatalf("lava: hp %d of %d", p.HP, p.MaxHP)
		}
		m.boss.round++
		m.bossTurnEnded(context.Background())
		if len(m.State.LavaZones) != 0 {
			t.Fatalf("zones left %v", m.State.LavaZones)
		}
	})
}

// The skill pick: by weight among the attack skills off cooldown (never a summon skill); a skill
// cast this round waits its cooldown in boss turns.
func TestBossSkillCooldown(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 4)
	m.boss.cfg.Skills = append(m.boss.cfg.Skills, gamedata.BossSkill{SkillID: "drone_wave", Name: "W", Kind: gamedata.BossSkillSummon,
		Weight: 100, MinionID: "drone", Count: 2}) // a legacy summon skill: never an attack
	m.boss.round = 1
	m.boss.lastUsed["laser_beam"] = 1 // cooldown 2: free again at round 4
	for r := 2; r <= 3; r++ {
		m.boss.round = r
		for i := 0; i < 30; i++ {
			if sk, _ := m.pickBossSkill(rand.New(rand.NewSource(int64(i)))); sk.SkillID != "shell_barrage" {
				t.Fatalf("round %d picked %s on cooldown", r, sk.SkillID)
			}
		}
	}
	m.boss.round = 5
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		sk, _ := m.pickBossSkill(rand.New(rand.NewSource(int64(i))))
		seen[sk.SkillID] = true
	}
	if !seen["laser_beam"] || seen["drone_wave"] || !seen["shell_barrage"] {
		t.Fatalf("picked %v", seen)
	}
}

// A big body is hit and blasted as such: a shell 60 px above the boss's feet hits it, a normal
// player's body is missed at that height; a blast on top of the boss deals damage.
func TestBossBodySize(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 1)
	boss := m.State.Players["boss_steel_robot"]
	p := m.State.Players["p0"]
	r := m.gd.Physics.PlayerHitRadius
	from, to := Vector2{X: boss.Position.X - 200, Y: boss.Position.Y - 110}, Vector2{X: boss.Position.X + 5, Y: boss.Position.Y - 110}
	hit, _, id := checkSegmentCollisions(from, to, p.PlayerID, 1, m.Terrain, m.State.Players, &drillState{}, r, 20, 4)
	if !hit || id != boss.PlayerID {
		t.Fatalf("shell over the boss: hit %v %s", hit, id)
	}
	far := Vector2{X: p.Position.X, Y: p.Position.Y - 110}
	hit, _, _ = checkSegmentCollisions(Vector2{X: far.X - 200, Y: far.Y}, Vector2{X: far.X + 5, Y: far.Y}, boss.PlayerID, 2, m.Terrain,
		m.State.Players, &drillState{}, r, 20, 4)
	if hit {
		t.Fatal("a player's body reaches 110 px up")
	}
	top := Vector2{X: boss.Position.X, Y: boss.Position.Y - 130}
	if d := blastDistance(boss, top, r, 20); d > 30 {
		t.Fatalf("blast on the boss's head %v px away", d)
	}
	if d := blastDistance(p, Vector2{X: p.Position.X, Y: p.Position.Y - 20 - r - 30}, r, 20); math.Abs(d-30) > 1e-9 {
		t.Fatalf("player blast distance %v", d)
	}
}

// The boss never drops off the map with out_of_map clamp: a fall below the map leaves it put.
func TestBossClampsFall(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 1)
	boss := m.State.Players["boss_steel_robot"]
	boss.Position = Vector2{X: 10, Y: 100} // over the void left of the party deck
	tl := m.newTimeline()
	m.settleFalls(m.State.Players["p0"], 0, tl)
	if !boss.IsAlive || boss.Position.Y != 100 {
		t.Fatalf("boss fell: %+v", boss.Position)
	}
	m.boss.cfg.OutOfMap = gamedata.BossOutOfMapKill
	m.settleFalls(m.State.Players["p0"], 0, tl)
	if boss.IsAlive {
		t.Fatal("kill: the boss survived the fall")
	}
}

func matchEnded(t *testing.T, c *ws.Client) map[string]json.RawMessage {
	t.Helper()
	var ended map[string]json.RawMessage
	if err := json.Unmarshal(lastEvent(t, drainMsgs(c), "MatchEnded"), &ended); err != nil {
		t.Fatal(err)
	}
	return ended
}

// Win: the boss dies (its minions die with it). Lose: the party is dead, or the boss's
// turnLimit-th turn ends (boss_escaped).
func TestBossWinLose(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()

	t.Run("boss_defeated", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 2)
		boss, shooter := m.State.Players["boss_steel_robot"], m.State.Players["p0"]
		killPlayer(m.State.Players["minion_drone_1"])
		boss.HP = 50
		tl := m.newTimeline()
		r := &ProjectileResult{ProjectileID: "x", HitPlayerID: boss.PlayerID, ExplosionPoint: vecPtr(bodyCentre(boss, 20)), ExplosionRadius: 40,
			Path: []ProjectileStep{{Time: 0}, {Time: 1}}}
		m.explodeProjectile(shooter, r, 600, tl)
		m.finishShot(ctx, &ShotResolved{ShooterID: shooter.PlayerID, ActionMode: "weapon", Projectiles: []*ProjectileResult{r}}, tl)
		if m.State.Status != "ended" || m.State.Players["minion_drone_2"].IsAlive {
			t.Fatalf("status %s, minion 2 alive %v", m.State.Status, m.State.Players["minion_drone_2"].IsAlive)
		}
		msgs := drainMsgs(watch)
		var shot ShotResolved
		_ = json.Unmarshal(lastEvent(t, msgs, "ShotResolved"), &shot)
		minionDie := false
		for _, e := range shot.Events {
			minionDie = minionDie || (e.Type == "die" && e.PlayerID == "minion_drone_2" && e.Cause == BossResultDefeated)
		}
		var ended struct {
			WinningTeam int          `json:"winningTeam"`
			Result      string       `json:"result"`
			Boss        bossMatchEnd `json:"boss"`
			MVP         string       `json:"mvpPlayerId"`
		}
		_ = json.Unmarshal(lastEvent(t, msgs, "MatchEnded"), &ended)
		if !minionDie || ended.WinningTeam != 1 || ended.Result != BossResultDefeated || ended.Boss.BossID != "steel_robot" ||
			ended.Boss.BossHP != 0 || ended.Boss.BossDamage["p0"] < 50 || ended.MVP != "p0" {
			t.Fatalf("minion die %v ended %+v", minionDie, ended)
		}
	})

	t.Run("party_defeated", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 2)
		killPlayer(m.State.Players["p0"])
		killPlayer(m.State.Players["p1"])
		m.checkWinCondition(ctx)
		e := matchEnded(t, watch)
		if string(e["winningTeam"]) != "2" || string(e["result"]) != `"party_defeated"` {
			t.Fatalf("ended %s %s", e["winningTeam"], e["result"])
		}
	})

	t.Run("boss_escaped", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 1)
		m.startTurn(ctx)
		turns := 0
		for m.State.Status == "in_progress" && turns < 200 {
			m.endTurn(ctx) // nobody acts; the boss's turns count up to turnLimit
			turns++
		}
		e := matchEnded(t, watch)
		if m.boss.round != 15 || string(e["winningTeam"]) != "2" || string(e["result"]) != `"boss_escaped"` {
			t.Fatalf("round %d ended %s %s", m.boss.round, e["winningTeam"], e["result"])
		}
	})
}

// Bot parties fight every boss to the end on its arena (boss skills, minion AI, summons, lava),
// and every party spawn can damage the boss from where it stands.
func TestBotPartiesFinishBossMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("boss matches")
	}
	loadConfigs(t)
	for _, id := range []string{"steel_robot", "lava_crab", "ice_witch"} {
		t.Run(id, func(t *testing.T) {
			m, _ := bossMatch(t, id, 4)
			m.BotTier = matchmaker.BotTierConfig{HitChance: 80, UseItemChance: 0, MovementSmart: 1}
			boss := m.State.Players[BossPlayerID(id)]
			for i := 0; i < 4; i++ {
				p := m.State.Players[fmt.Sprintf("p%d", i)]
				w, _ := botWeapon(m.gd, p)
				hp := boss.HP
				shield := m.boss.cfg.ShieldedDamageFactor
				m.boss.cfg.ShieldedDamageFactor = 1
				s := NewBotBrain(perfectAim).bestShotIn(m, p, p.Position, w, shotEnv{})
				m.boss.cfg.ShieldedDamageFactor = shield
				if !s.found || s.damage <= 0 || boss.HP != hp {
					t.Fatalf("%s at %v has no damaging shot (%+v)", p.PlayerID, p.Position, s)
				}
			}
			rounds := playBossMatch(t, m, 400)
			if m.State.Status != "ended" {
				t.Fatalf("not over after %d turns, round %d", rounds, m.boss.round)
			}
			t.Logf("%s: over in %d turns, round %d, boss hp %d", id, rounds, m.boss.round, boss.HP)
		})
	}
}

// playBossMatch plays m with the bot AI (party and minions) and the boss's skill pick, turn
// after turn, synchronously.
func playBossMatch(t *testing.T, m *Match, maxTurns int) int {
	t.Helper()
	ctx := context.Background()
	m.startTurn(ctx)
	turns := 0
	for ; turns < maxTurns && m.State.Status == "in_progress"; turns++ {
		seq := m.turnSeq
		cur := m.State.Players[m.State.CurrentPlayerID]
		if cur.Role == RoleBoss {
			m.bossFreeSummon(cur) // the free summon first, even when it lost its attack
			if skipsTurn(m.gd, cur) == "" {
				m.bossHover(cur)
				if sk, ok := m.pickBossSkill(m.skillRNG()); ok {
					m.castBossSkill(ctx, sk)
				}
			}
		} else if m.isMover(cur) {
			if skipsTurn(m.gd, cur) == "" {
				m.playMoverTurn(ctx, cur)
			}
		} else {
			var ts botTurnState
			for step := 0; step < botMaxSteps && m.turnSeq == seq && !m.resolving && m.State.Status == "in_progress"; step++ {
				if skipsTurn(m.gd, cur) != "" {
					break
				}
				tier := m.BotTier
				if hc := m.minionHitChance(cur); hc > 0 {
					tier.HitChance = hc
				}
				switch act := NewBotBrain(tier).DecideAction(m, cur, ts).(type) {
				case UseItemAction:
					m.processUseItem(ctx, nil, act)
					ts.noteItem(act.ItemID)
				case MoveAction:
					m.processMove(ctx, nil, act)
					ts.moved = true
				case ShootAction:
					m.processShoot(ctx, nil, act)
					step = botMaxSteps
				}
			}
		}
		if m.State.Status == "in_progress" && m.turnSeq == seq {
			m.endTurn(ctx)
		}
	}
	return turns
}

// TurnStarted carries the round and the lava pools in boss mode.
func TestBossTurnStartedRound(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "lava_crab", 1)
	m.startTurn(context.Background())
	var ts map[string]json.RawMessage
	_ = json.Unmarshal(lastEvent(t, drainMsgs(watch), "TurnStarted"), &ts)
	if string(ts["round"]) != "0" || string(ts["lavaZones"]) != "[]" {
		t.Fatalf("TurnStarted %v", ts)
	}
	if !reflect.DeepEqual(m.State.LavaZones, []LavaZone{}) {
		t.Fatal("zones")
	}
}
