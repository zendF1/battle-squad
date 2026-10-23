package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// Boss mode (mode boss:<bossId>, gamedata.BossConfig). Team 1 is the party (up to 4 humans and
// bot teammates); team 2 is the boss and its minions, BattlePlayerStates with IsBot, Role
// "boss" / "minion", BossID / MinionID and a BodySize, so damage, blasts, status effects and
// falls work on them as on players (body.go: bigger bodies are hit and blasted as such).
//
// Turn order: the party and the minions alternate (party[0], minion[0], party[1], …, the rest),
// the boss last: it acts once per round. Minions play with the bot AI and the weapon of their
// characterId (their damage replacing the gun's). The boss's turn casts one of its skills
// (pickBossSkill: by weight among those off cooldown) as a ShotResolved (actionMode skill,
// skillId the boss skill): rain (missiles from the top, explode / damage / status, lava pools),
// beam (beam / damage / status) or summon (summon events). While a minion lives the boss takes
// shieldedDamageFactor × the damage (BossShieldChanged when that changes). The party wins when
// the boss dies (its minions die with it), loses when every party member is dead or when the
// boss's turnLimit-th turn ends (MatchEnded result boss_escaped).

// Boss match results (MatchEnded.result).
const (
	BossResultDefeated      = "boss_defeated"  // the boss died: team 1 wins
	BossResultPartyDefeated = "party_defeated" // every party member is dead
	BossResultEscaped       = "boss_escaped"   // the boss's last turn (turnLimit) ended
)

const (
	bossTurnDelaySec = 1.2  // TurnStarted → the boss shows its skill (ActionSelected)
	bossCastDelaySec = 1.0  // ActionSelected → the skill's ShotResolved
	bossChargeTime   = 0.6  // s into the shot: the rain starts, the beam fires, the first minion appears
	bossRainInterval = 0.08 // s between two shells of a rain
	bossBeamTime     = 0.8  // s the beam shows
	bossBeamSpeed    = 12000.0
	bossSummonGap    = 0.3 // s between two summoned minions
	lavaHeightPx     = 60  // a lava pool reaches this far above and below its point
)

// bossRuntime is the boss fight's state on the match.
type bossRuntime struct {
	cfg      gamedata.BossConfig
	playerID string
	round    int            // boss turns started
	lastUsed map[string]int // skill id → the round it was cast
	party    []string       // team 1 in turn order
	minions  []string       // minion player ids in the order they appeared
	wave     []string       // the last wave (the initial minions, then each summon): no summon while one lives
	spawns   []Vector2      // the arena's minion spawn points
	pillars  []Vector2      // the arena's pillar tops (gamedata.ArenaPillars), nil for most arenas
	seq      int            // minions created
	shielded bool
	lavaSeq  int
	// lastSummon is the round of the boss's last free summon (0: none yet).
	lastSummon int
	// statusRoll draws the status chance rolls of the cast resolving (0..1; castBossSkill seeds
	// it from the match and turn, so a replay rolls the same); tests may fix it (statusRollFix).
	statusRoll    func() float64
	statusRollFix func() float64
}

// bossStatusLands reports whether skill s's status lands on a party member its cast hit: a roll
// under its statusChance (default 0.5).
func (m *Match) bossStatusLands(s gamedata.BossSkill) bool {
	c := s.StatusChanceOrDefault()
	if c >= 1 {
		return true
	}
	roll := m.boss.statusRoll
	if m.boss.statusRollFix != nil {
		roll = m.boss.statusRollFix
	}
	if roll == nil {
		roll = m.turnRNG("status").Float64
	}
	return roll() < c
}

// BossPlayerID is the player id of boss bossID in its match.
func BossPlayerID(bossID string) string { return "boss_" + bossID }

// NewBossSide builds team 2 of a match against boss cfg: the boss at the arena's boss spawn
// point, then its initial minions (waves.initial) at the minion spawn points. The match lands
// them on the ground (Terrain.SpawnY).
func NewBossSide(gd *gamedata.Snapshot, cfg gamedata.BossConfig) []*BattlePlayerState {
	_, bossSpawn, minionSpawns := gamedata.BossSpawns(gd.Data.Maps[cfg.MapID])
	minionSpawns = nearestFirst(minionSpawns, bossSpawn)
	out := []*BattlePlayerState{{
		PlayerID: BossPlayerID(cfg.BossID), DisplayName: cfg.Name, TeamID: 2,
		HP: cfg.HP, MaxHP: cfg.HP, Defense: cfg.Defense, Position: Vector2{X: bossSpawn.X, Y: bossSpawn.Y},
		Items: []string{}, StatusEffects: []StatusEffect{}, IsAlive: true, IsBot: true,
		Role: RoleBoss, BossID: cfg.BossID, BodySize: cfg.BodySize, Flying: cfg.Flying,
	}}
	seq := 0
	for _, w := range cfg.Waves.Initial {
		mc, ok := cfg.Minion(w.MinionID)
		if !ok {
			continue
		}
		for i := 0; i < w.Count; i++ {
			seq++
			sp := minionSpawns[(seq-1)%len(minionSpawns)]
			out = append(out, newMinion(gd, cfg, mc, seq, Vector2{X: sp.X, Y: sp.Y}))
		}
	}
	return out
}

// newMinion is the seq-th minion of the match, of type mc, at pos. Its shots deal its damage ×
// shotDamageFactor (the weapon's damage + damageBonus).
func newMinion(gd *gamedata.Snapshot, cfg gamedata.BossConfig, mc gamedata.MinionConfig, seq int, pos Vector2) *BattlePlayerState {
	mc = mc.Normalized()
	weaponDamage := 0
	if c, ok := gd.Data.Characters[mc.CharacterID]; ok {
		weaponDamage = gd.Data.Weapons[c.WeaponID].Damage
	}
	energy := max(mc.MoveEnergy, 1)
	shotDamage := int(math.Round(float64(mc.Damage) * mc.ShotDamageFactor))
	return &BattlePlayerState{
		PlayerID: fmt.Sprintf("minion_%s_%d", mc.MinionID, seq), DisplayName: mc.Name, TeamID: 2, CharacterID: mc.CharacterID,
		HP: mc.HP, MaxHP: mc.HP, Defense: mc.Defense, DamageBonus: shotDamage - weaponDamage, Position: pos,
		MoveEnergy: energy, MaxMoveEnergy: energy, Items: []string{}, StatusEffects: []StatusEffect{},
		IsAlive: true, IsBot: true, Role: RoleMinion, BossID: cfg.BossID, MinionID: mc.MinionID, BodySize: mc.BodySize,
		Flying: mc.Movement == gamedata.MinionMoveFly, Behavior: mc.Behavior, Movement: mc.Movement,
	}
}

// SetupBoss makes the match a fight against boss cfg (its players include NewBossSide's): the
// turn order (party and minions alternating, the boss last), the boss state and the shield.
// Call it before Run.
func (m *Match) SetupBoss(cfg gamedata.BossConfig) {
	b := &bossRuntime{cfg: cfg, lastUsed: map[string]int{}}
	var minions []*BattlePlayerState
	for _, id := range m.State.TurnOrder {
		p := m.State.Players[id]
		switch p.Role {
		case RoleBoss:
			b.playerID = p.PlayerID
		case RoleMinion:
			minions = append(minions, p)
		default:
			b.party = append(b.party, p.PlayerID)
		}
	}
	sort.Slice(minions, func(i, j int) bool { return minionSeq(minions[i].PlayerID) < minionSeq(minions[j].PlayerID) })
	for _, p := range minions {
		b.minions = append(b.minions, p.PlayerID)
	}
	b.seq = len(minions)
	b.wave = append([]string(nil), b.minions...)
	arena := m.gd.Data.Maps[cfg.MapID]
	_, bossSpawn, spawns := gamedata.BossSpawns(arena)
	for _, sp := range nearestFirst(spawns, bossSpawn) {
		b.spawns = append(b.spawns, Vector2{X: sp.X, Y: sp.Y})
	}
	for _, sp := range gamedata.ArenaPillars(arena) {
		b.pillars = append(b.pillars, Vector2{X: sp.X, Y: sp.Y})
	}
	m.boss = b
	m.placeBossSide()
	m.State.Boss = &BossState{BossID: cfg.BossID, PlayerID: b.playerID, TurnLimit: cfg.TurnLimit}
	m.rebuildBossTurnOrder()
	m.State.CurrentPlayerID = m.State.TurnOrder[0]
	b.shielded = m.aliveMinions() > 0
	shielded := b.shielded
	m.State.BossShielded = &shielded
	m.State.LavaZones = []LavaZone{}
}

// minionSeq is the number at the end of a minion's player id (minion_<type>_<n>).
func minionSeq(playerID string) int {
	n, _ := strconv.Atoi(playerID[strings.LastIndex(playerID, "_")+1:])
	return n
}

// rebuildBossTurnOrder lays the turn order out again: the party and the living minions
// alternating, the rest of the longer list, then the boss. Summons happen on the boss's own
// turn, the last of the order, so the next turn is still the first of the order.
func (m *Match) rebuildBossTurnOrder() {
	b := m.boss
	var alive []string
	for _, id := range b.minions {
		if p := m.State.Players[id]; p != nil && p.IsAlive {
			alive = append(alive, id)
		}
	}
	order := make([]string, 0, len(b.party)+len(alive)+1)
	for i := 0; i < max(len(b.party), len(alive)); i++ {
		if i < len(b.party) {
			order = append(order, b.party[i])
		}
		if i < len(alive) {
			order = append(order, alive[i])
		}
	}
	m.State.TurnOrder = append(order, b.playerID)
}

// bossPlayer is the boss's state (nil outside boss mode).
func (m *Match) bossPlayer() *BattlePlayerState {
	if m.boss == nil {
		return nil
	}
	return m.State.Players[m.boss.playerID]
}

// aliveMinions is how many minions live.
func (m *Match) aliveMinions() int {
	n := 0
	for _, p := range m.State.Players {
		if p.Role == RoleMinion && p.IsAlive {
			n++
		}
	}
	return n
}

// shieldedDamage is the damage p takes of damage: shieldedDamageFactor × it for the boss while
// a minion lives, damage otherwise.
func (m *Match) shieldedDamage(p *BattlePlayerState, damage int) int {
	if m.boss == nil || p.Role != RoleBoss || damage <= 0 || m.aliveMinions() == 0 {
		return damage
	}
	return int(math.Round(float64(damage) * m.boss.cfg.ShieldedDamageFactor))
}

// noteBossSideDamage counts damage a party member dealt to the boss or a minion.
func (m *Match) noteBossSideDamage(shooter, target *BattlePlayerState, damage int) {
	if m.boss == nil || shooter.Role != "" {
		return
	}
	switch target.Role {
	case RoleBoss:
		shooter.BossDamage += damage
	case RoleMinion:
		shooter.MinionDamage += damage
	}
}

// bossClampsFall reports whether p is a boss that must not drop off the map (out_of_map clamp):
// a blast that would drop it below the map leaves it where it stands.
func (m *Match) bossClampsFall(p *BattlePlayerState) bool {
	return m.boss != nil && p.Role == RoleBoss && m.boss.cfg.OutOfMap != gamedata.BossOutOfMapKill
}

// killMinions kills the living minions (the boss died); it returns them.
func (m *Match) killMinions() []*BattlePlayerState {
	var out []*BattlePlayerState
	for _, p := range m.playersByID() {
		if p.Role == RoleMinion && p.IsAlive {
			killPlayer(p)
			out = append(out, p)
		}
	}
	return out
}

// bossAfterShot: when the shot killed the boss, its minions die with it (die events cause
// boss_defeated when the boss died).
func (m *Match) bossAfterShot(tl *shotTimeline) {
	boss := m.bossPlayer()
	if boss == nil || boss.IsAlive {
		return
	}
	t := tl.end
	for _, e := range tl.events {
		if e.Type == "die" && e.PlayerID == boss.PlayerID {
			t = e.T
		}
	}
	for _, p := range m.killMinions() {
		tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: BossResultDefeated})
	}
}

// BossShieldChanged is sent when the boss's shield goes up (a minion appears) or down (the
// last minion dies), right after the ShotResolved / PlayerDamaged that changed it.
type BossShieldChanged struct {
	BossID   string `json:"bossId"`
	PlayerID string `json:"playerId"`
	Shielded bool   `json:"shielded"`
}

// syncBossShield updates MatchState.bossShielded and sends BossShieldChanged when it changed.
func (m *Match) syncBossShield() {
	if m.boss == nil {
		return
	}
	boss := m.bossPlayer()
	shielded := m.aliveMinions() > 0 && boss != nil && boss.IsAlive
	if shielded == m.boss.shielded {
		return
	}
	m.boss.shielded = shielded
	m.State.BossShielded = &shielded
	ev := BossShieldChanged{BossID: m.boss.cfg.BossID, PlayerID: m.boss.playerID, Shielded: shielded}
	payload, _ := json.Marshal(ev)
	m.broadcast(ws.Message{Event: "BossShieldChanged", Data: payload})
	m.el.Log("BossShieldChanged", m.boss.playerID, ev)
}

// bossTurnStarted runs at the start of the boss's turn: a new round.
func (m *Match) bossTurnStarted(boss *BattlePlayerState) {
	m.boss.round++
	m.State.Boss.Round = m.boss.round
	boss.MoveEnergy, boss.MaxMoveEnergy = 0, 0 // the boss never walks
}

// bossTurnEnded runs at the end of the boss's turn: its lava pools of this round dry up, and
// after its turnLimit-th turn it escapes. It reports whether the match ended.
func (m *Match) bossTurnEnded(ctx context.Context) bool {
	kept := m.State.LavaZones[:0]
	for _, z := range m.State.LavaZones {
		if z.ExpiresRound > m.boss.round {
			kept = append(kept, z)
		}
	}
	m.State.LavaZones = kept
	if m.boss.round >= m.boss.cfg.TurnLimit {
		m.finishMatch(ctx, 2, BossResultEscaped)
		return true
	}
	return false
}

// bossAct is the boss's turn: it shows the skill it picked (ActionSelected) and casts it
// bossCastDelaySec later.
func (m *Match) bossAct(ctx context.Context) {
	boss := m.bossPlayer()
	if m.resolving || boss == nil || !boss.IsAlive || m.State.CurrentPlayerID != boss.PlayerID {
		return
	}
	skill, ok := m.pickBossSkill(m.skillRNG())
	if !ok {
		m.endTurn(ctx)
		return
	}
	sel := ActionSelected{PlayerID: boss.PlayerID, ActionMode: "skill", SkillID: skill.SkillID}
	m.selection = sel
	m.broadcastSelection(sel)
	m.runOnTurnAfter(ctx, secs(bossCastDelaySec), func(ctx context.Context) {
		m.castBossSkill(ctx, skill)
	})
}

// pickBossSkill picks the boss's attack this turn: by weight among its attack skills
// (AttackSkills: summoning is free, never an attack) off cooldown (cast more than cooldown boss
// turns ago); when none is, the first attack skill.
func (m *Match) pickBossSkill(rng *rand.Rand) (gamedata.BossSkill, bool) {
	b := m.boss
	attacks := b.cfg.AttackSkills()
	var cands []gamedata.BossSkill
	total := 0.0
	for _, s := range attacks {
		if last, used := b.lastUsed[s.SkillID]; used && b.round-last <= s.Cooldown {
			continue
		}
		cands = append(cands, s)
		total += s.Weight
	}
	if len(cands) == 0 {
		if len(attacks) > 0 {
			return attacks[0], true
		}
		return gamedata.BossSkill{}, false
	}
	roll := rng.Float64() * total
	for _, s := range cands {
		if roll < s.Weight {
			return s, true
		}
		roll -= s.Weight
	}
	return cands[len(cands)-1], true
}

// castBossSkill resolves the boss's skill as its turn's ShotResolved.
func (m *Match) castBossSkill(ctx context.Context, skill gamedata.BossSkill) {
	boss := m.bossPlayer()
	if m.resolving || boss == nil || !boss.IsAlive || m.State.CurrentPlayerID != boss.PlayerID {
		return
	}
	m.boss.lastUsed[skill.SkillID] = m.boss.round
	shot := &ShotResolved{ShooterID: boss.PlayerID, ActionMode: "skill", SkillID: skill.SkillID}
	tl := m.newTimeline()
	rng := m.skillRNG()
	m.boss.statusRoll = m.turnRNG("status").Float64
	switch skill.Kind {
	case gamedata.BossSkillRain:
		m.bossRain(boss, skill, shot, tl, rng)
	case gamedata.BossSkillBeam:
		switch skill.Pattern {
		case gamedata.BeamRadial:
			m.bossRadialBeams(boss, skill, tl, rng)
		case gamedata.BeamSweep:
			m.bossSweepBeams(boss, skill, tl)
		default:
			m.bossBeam(boss, skill, tl, rng)
		}
	case gamedata.BossSkillShockwave:
		m.bossShockwave(boss, skill, tl)
	}
	tl.reach(bossChargeTime)
	m.el.Log("BossSkill", boss.PlayerID, skill.SkillID)
	m.finishShot(ctx, shot, tl)
}

// bossTarget is the living party member a skill aims at (random, nearest the boss, lowest HP).
func (m *Match) bossTarget(boss *BattlePlayerState, target string, rng *rand.Rand) *BattlePlayerState {
	var party []*BattlePlayerState
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID != boss.TeamID {
			party = append(party, p)
		}
	}
	if len(party) == 0 {
		return nil
	}
	switch target {
	case gamedata.BossTargetNearest:
		sort.SliceStable(party, func(i, j int) bool {
			return math.Abs(party[i].Position.X-boss.Position.X) < math.Abs(party[j].Position.X-boss.Position.X)
		})
		return party[0]
	case gamedata.BossTargetLowestHP:
		sort.SliceStable(party, func(i, j int) bool { return party[i].HP < party[j].HP })
		return party[0]
	}
	return party[rng.Intn(len(party))]
}

// bossRain: count shells fall straight down from the top of the map over the target ± spread px,
// bossRainInterval apart from bossChargeTime (a rain event), resolved in the order they land
// (each sees the craters before it), each damage × the boss damage with a blast px blast and a
// crater px crater, at most cap × the boss damage per target; status on every enemy damaged;
// with lava each impact leaves a lava pool (a lava event).
func (m *Match) bossRain(boss *BattlePlayerState, s gamedata.BossSkill, shot *ShotResolved, tl *shotTimeline, rng *rand.Rand) {
	centre, spread, ok := m.rainCentre(boss, s, rng)
	if !ok {
		return
	}
	base := float64(m.boss.cfg.Damage)
	tu := gamedata.DefaultRainTuning()
	xs := rainColumns(rng, s.Count, centre.X, spread, m.Terrain.Width, 1)
	tl.add(ShotEvent{T: bossChargeTime, Type: "rain", Position: vecPtr(centre), Radius: spread, Count: s.Count})

	weapon := gamedata.WeaponConfig{WeaponID: s.SkillID, Damage: m.boss.cfg.Damage, ExplosionRadius: int(math.Round(s.Blast)), ProjectileWeight: 1}
	var limit *skillCap
	if s.Cap > 0 {
		limit = newSkillCap(m.capOfBase(s.Cap * base))
	}
	launch := func(k int) float64 { return bossChargeTime + bossRainInterval*float64(k) }
	// from the boss (launchFrom boss): each shell first arcs from its ring around the boss's body
	// up to its column (arcs[k], arcTime[k] s), then falls from startY
	startY := tu.StartY
	var arcs [][]ProjectileStep
	arcTime := make([]float64, s.Count)
	if s.LaunchFrom == gamedata.RainFromBoss {
		startY = rainArcTopY
		arcs, arcTime = m.rainArcs(boss, xs, startY, rng)
	}
	drop := func(k int) float64 { return launch(k) + arcTime[k] }
	kind := s.ProjectileKind
	if kind == "" {
		kind = gamedata.DefaultRainProjectileKind
	}
	dropPower := rainDropPower(tu, m.gd.Physics)
	statused := map[string]bool{}
	left := make([]int, s.Count)
	for k := range left {
		left[k] = k
	}
	for len(left) > 0 {
		pick, bestEnd := -1, math.MaxFloat64
		var best *ProjectileResult
		for i, k := range left {
			r := SimulateProjectile(boss.PlayerID, boss.TeamID, Vector2{X: xs[k], Y: startY}, 270, dropPower,
				weapon, WindState{}, m.Terrain, m.State.Players, 0)
			if end := drop(k) + flightEnd(r); end < bestEnd {
				pick, best, bestEnd = i, r, end
			}
		}
		k := left[pick]
		left = append(left[:pick], left[pick+1:]...)

		boss.ShotsFired++
		delayPath(best, drop(k))
		if arcs != nil {
			arc := make([]ProjectileStep, len(arcs[k]))
			for i, st := range arcs[k] {
				st.Time += launch(k)
				arc[i] = st
			}
			best.Path = append(arc, best.Path...)
			best.LaunchDelay = launch(k)
		}
		best.SkillID, best.Kind = s.SkillID, kind
		best.ExplosionRadius, best.CraterRadius = s.Blast, s.Crater
		shot.Projectiles = append(shot.Projectiles, best)
		if best.ExplosionPoint == nil {
			continue
		}
		t := flightEnd(best)
		hit := m.explodeCapped(boss, best, base*s.Damage, tl, limit)
		if s.Status != "" {
			m.bossStatusOnce(tl, t, boss, hit, statused, s)
		}
		if s.LavaTurns > 0 && best.ExplosionPoint.Y < float64(m.Terrain.Height) {
			m.addLava(tl, t, *best.ExplosionPoint, s)
		}
		m.settleFalls(boss, t, tl)
	}
}

// addLava leaves a lava pool of skill s at point (a lava event at t): it burns the party for
// s.lavaTurns rounds (MatchState.lavaZones).
func (m *Match) addLava(tl *shotTimeline, t float64, point Vector2, s gamedata.BossSkill) {
	m.boss.lavaSeq++
	z := LavaZone{ZoneID: fmt.Sprintf("lava_%d", m.boss.lavaSeq), Position: point, Radius: s.LavaRadius,
		Damage: s.LavaDamage, ExpiresRound: m.boss.round + s.LavaTurns}
	if z.Damage <= 0 {
		z.Damage = m.gd.Rules.Combat.LavaDamage
	}
	m.State.LavaZones = append(m.State.LavaZones, z)
	tl.add(ShotEvent{T: t, Type: "lava", ZoneID: z.ZoneID, Position: vecPtr(point), Radius: z.Radius, Amount: z.Damage, Turns: s.LavaTurns})
}

// lavaZoneAt is the lava pool party member p stands in, nil for none (or for the boss's side).
func (m *Match) lavaZoneAt(p *BattlePlayerState) *LavaZone {
	if m.boss == nil || p.Role != "" {
		return nil
	}
	for i := range m.State.LavaZones {
		z := &m.State.LavaZones[i]
		if math.Abs(p.Position.X-z.Position.X) <= z.Radius && math.Abs(p.Position.Y-z.Position.Y) <= lavaHeightPx {
			return z
		}
	}
	return nil
}

// bossBeam: a beam from the boss's body centre through the target's to the map edge, fired at
// bossChargeTime (a beam event), through terrain; every party member within halfWidth px takes
// damage × the boss damage (× decay for each next one) and the status.
func (m *Match) bossBeam(boss *BattlePlayerState, s gamedata.BossSkill, tl *shotTimeline, rng *rand.Rand) {
	target := m.bossTarget(boss, s.Target, rng)
	if target == nil {
		return
	}
	lift := shotOriginLift(m.Terrain)
	origin, aim := bodyCentre(boss, lift), bodyCentre(target, lift)
	dx, dy := aim.X-origin.X, aim.Y-origin.Y
	length := math.Hypot(dx, dy)
	if length <= 0 {
		return
	}
	boss.ShotsFired++
	end := beamEnd(origin, Vector2{X: dx / length, Y: dy / length}, m.Terrain.Width, m.Terrain.Height)
	tl.add(ShotEvent{T: bossChargeTime, Type: "beam", PlayerID: boss.PlayerID, From: vecPtr(origin), To: vecPtr(end), Duration: bossBeamTime})

	k := m.gd.Physics.DefenseKOrDefault()
	raw := s.Damage * float64(m.boss.cfg.Damage)
	for _, h := range m.laserHits(boss, s.HalfWidth, origin, end) {
		p, t := h.p, bossChargeTime+math.Max(h.s, 0)/bossBeamSpeed
		damage := int(math.Round(reduceByDefense(raw, p.Defense, k)))
		if s.Decay > 0 {
			raw *= s.Decay
		}
		if damage <= 0 {
			continue
		}
		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		boss.DamageDealt += damage
		boss.ShotsHit++
		tl.add(damageEvent(t, p, damage, shieldBefore, "laser"))
		if killed {
			boss.KillCount++
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: "laser"})
			continue
		}
		if s.Status != "" && m.bossStatusLands(s) {
			m.applyShotStatus(tl, t, boss, p, s.Status, s.StatusTurns)
		}
	}
}

// bossSummon: count minions of type minionID appear (summon events bossSummonGap apart from
// bossChargeTime) at the minion spawn points, at most waves.maxAlive alive; they take their
// turns from the next round. It reports how many appeared.
func (m *Match) bossSummon(minionID string, count int, tl *shotTimeline) int {
	b := m.boss
	mc, ok := b.cfg.Minion(minionID)
	if !ok || len(b.spawns) == 0 {
		return 0
	}
	n := min(count, b.cfg.Waves.MaxAlive-m.aliveMinions())
	rng := m.turnRNG("summon")
	if n > 0 {
		b.wave = nil
	}
	for i := 0; i < n; i++ {
		b.seq++
		pos := m.freeMinionSpawn(rng)
		pos.Y = m.Terrain.SpawnY(pos.X, pos.Y)
		p := newMinion(m.gd, b.cfg, mc, b.seq, pos)
		m.liftFlier(p)
		m.State.Players[p.PlayerID] = p
		b.minions = append(b.minions, p.PlayerID)
		b.wave = append(b.wave, p.PlayerID)
		tl.add(summonEvent(bossChargeTime+bossSummonGap*float64(i), p))
	}
	if n > 0 {
		m.rebuildBossTurnOrder()
	}
	return max(n, 0)
}

// waveAlive reports whether a minion of the last wave still lives.
func (m *Match) waveAlive() bool {
	for _, id := range m.boss.wave {
		if p := m.State.Players[id]; p != nil && p.IsAlive {
			return true
		}
	}
	return false
}

// nearestFirst is spawns ordered by their distance to the boss's spawn (nearest first, ties in
// order): minions appear by the boss, not by the party.
func nearestFirst(spawns []gamedata.SpawnPoint, boss gamedata.SpawnPoint) []gamedata.SpawnPoint {
	out := append([]gamedata.SpawnPoint(nil), spawns...)
	sort.SliceStable(out, func(i, j int) bool {
		return math.Hypot(out[i].X-boss.X, out[i].Y-boss.Y) < math.Hypot(out[j].X-boss.X, out[j].Y-boss.Y)
	})
	return out
}

// summonEvent is the summon event of minion p at t: the new unit in full.
func summonEvent(t float64, p *BattlePlayerState) ShotEvent {
	flying := p.Flying
	return ShotEvent{T: t, Type: "summon", PlayerID: p.PlayerID, MinionID: p.MinionID, Position: vecPtr(p.Position),
		HP: intPtr(p.HP), MaxHP: intPtr(p.MaxHP), DisplayName: p.DisplayName, TeamID: p.TeamID, BodySize: p.BodySize,
		Flying: &flying, Behavior: p.Behavior, Movement: p.Movement}
}

// freeMinionSpawn is where the next minion appears: on a pillar arena a random free pillar top
// (freePillar), else the first minion spawn point no living minion stands near (40 px), else the
// next one in turn.
func (m *Match) freeMinionSpawn(rng *rand.Rand) Vector2 {
	b := m.boss
	if sp, ok := m.freePillar(rng); ok {
		return sp
	}
	for _, sp := range b.spawns {
		free := true
		for _, p := range m.State.Players {
			if p.Role == RoleMinion && p.IsAlive && math.Abs(p.Position.X-sp.X) < 40 {
				free = false
				break
			}
		}
		if free {
			return sp
		}
	}
	return b.spawns[b.seq%len(b.spawns)]
}

// bossMatchEnd is MatchEnded.boss: the boss, the result, the rounds played, the boss's HP left
// and every party member's damage to the boss (bots included: the damage ranking).
type bossMatchEnd struct {
	BossID     string         `json:"bossId"`
	Result     string         `json:"result"`
	Round      int            `json:"round"`
	TurnLimit  int            `json:"turnLimit"`
	BossHP     int            `json:"bossHp"`
	BossMaxHP  int            `json:"bossMaxHp"`
	BossDamage map[string]int `json:"bossDamage"`
}

func (m *Match) bossEnd(result string) bossMatchEnd {
	out := bossMatchEnd{BossID: m.boss.cfg.BossID, Result: result, Round: m.boss.round, TurnLimit: m.boss.cfg.TurnLimit,
		BossDamage: map[string]int{}}
	if boss := m.bossPlayer(); boss != nil {
		out.BossHP, out.BossMaxHP = boss.HP, boss.MaxHP
	}
	for _, p := range m.State.Players {
		if p.Role == "" {
			out.BossDamage[p.PlayerID] = p.BossDamage
		}
	}
	return out
}

// minionHitChance is the hit chance of minion p's bot AI (its config, 0: the match's tier).
func (m *Match) minionHitChance(p *BattlePlayerState) float64 {
	if m.boss == nil || p.Role != RoleMinion {
		return 0
	}
	mc, _ := m.boss.cfg.Minion(p.MinionID)
	return mc.HitChance
}

// secs is s seconds.
func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
