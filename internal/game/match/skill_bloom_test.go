package match

import (
	"testing"

	"battle-squad/internal/game/ws"
)

// bloomMatch: Flora and a Rookie teammate against a Rookie.
func bloomMatch(t *testing.T) (*Match, map[string]*ws.Client) {
	return flatSkillMatch(t, skillPlayer{"p1", 1, "flora", 400}, skillPlayer{"e1", 2, "rookie", 1300},
		skillPlayer{"a1", 1, "rookie", 700})
}

// TestBloomHealsShieldsAndCleanses: Flora 40%, teammate 30% of max HP (rank 0); the overflow
// becomes shield; bad effects removed, the caster's shot buffs kept; enemies untouched.
func TestBloomHealsShieldsAndCleanses(t *testing.T) {
	m, clients := bloomMatch(t)
	flora, ally, enemy := m.State.Players["p1"], m.State.Players["a1"], m.State.Players["e1"]
	flora.HP = 1000 // of 1900: +760, no overflow
	ally.HP = 1450  // of 1500: +450 → 50 HP, 400 shield
	enemy.HP = 500
	flora.StatusEffects = []StatusEffect{{EffectID: "root", DurationTurn: 1}, {EffectID: "power_shot", DurationTurn: 1}}
	ally.StatusEffects = []StatusEffect{{EffectID: "burn", DurationTurn: 1}, {EffectID: "stun", DurationTurn: 1}, {EffectID: "net", DurationTurn: 1}}
	s := fireSkill(t, m, clients, "p1", 90, 50)

	if flora.HP != 1760 || flora.Shield != 0 || ally.HP != 1500 || ally.Shield != 400 || enemy.HP != 500 {
		t.Fatalf("flora %d/%d ally %d/%d enemy %d", flora.HP, flora.Shield, ally.HP, ally.Shield, enemy.HP)
	}
	if h := eventsOf(s, "heal", "a1"); len(h) != 1 || h[0].Amount != 50 || h[0].T != bloomSeedTime {
		t.Fatalf("ally heal %+v", h)
	}
	if sh := eventsOf(s, "shield", "a1"); len(sh) != 1 || sh[0].Amount != 400 || *sh[0].Shield != 400 {
		t.Fatalf("ally shield %+v", sh)
	}
	if len(eventsOf(s, "shield", "p1")) != 0 || len(eventsOf(s, "heal", "e1")) != 0 {
		t.Fatal("unexpected shield / heal")
	}
	if c := eventsOf(s, "cleanse", "a1"); len(c) != 1 || len(c[0].EffectIDs) != 3 || len(ally.StatusEffects) != 0 {
		t.Fatalf("ally cleanse %+v, left %+v", c, ally.StatusEffects)
	}
	if len(flora.StatusEffects) != 1 || flora.StatusEffects[0].EffectID != "power_shot" {
		t.Fatalf("flora effects %+v, want only power_shot kept", flora.StatusEffects)
	}
	if len(s.Projectiles) != 2 {
		t.Fatalf("%d seeds, want one per living ally", len(s.Projectiles))
	}
	for _, r := range s.Projectiles {
		p := m.State.Players[r.HitPlayerID]
		end := r.Path[len(r.Path)-1]
		if r.Kind != "seed" || r.ExplosionPoint != nil || end.Time != bloomSeedTime ||
			end.Position != (Vector2{X: p.Position.X, Y: p.Position.Y - ShotOriginLift}) {
			t.Fatalf("seed %+v ends %+v", r, end)
		}
	}
	if !m.resolving {
		t.Fatal("bloom did not end the turn")
	}
}

func TestBloomShieldCappedAtHalfMaxHP(t *testing.T) {
	m, clients := bloomMatch(t)
	ally := m.State.Players["a1"]
	m.State.Players["e1"].IsAlive = true
	ally.Shield = 600 // + 450 → capped at 750
	s := fireSkill(t, m, clients, "p1", 90, 50)
	if ally.Shield != 750 || ally.HP != 1500 {
		t.Fatalf("ally shield %d hp %d, want 750 cap", ally.Shield, ally.HP)
	}
	if sh := eventsOf(s, "shield", "a1"); len(sh) != 1 || sh[0].Amount != 150 {
		t.Fatalf("shield events %+v", sh)
	}
	// Rank 4: Flora 60%, teammate 50%
	m2, clients2 := bloomMatch(t)
	m2.State.Players["p1"].SkillRank = 4
	m2.State.Players["p1"].HP = 100
	m2.State.Players["a1"].HP = 100
	fireSkill(t, m2, clients2, "p1", 90, 50)
	if hp := m2.State.Players["p1"].HP; hp != 100+1140 {
		t.Fatalf("flora rank 4 hp %d, want +60%% = 1240", hp)
	}
	if hp := m2.State.Players["a1"].HP; hp != 100+750 {
		t.Fatalf("ally rank 4 hp %d, want +50%% = 850", hp)
	}
}

func TestBotBloomWhenHurtOrAfflicted(t *testing.T) {
	m, bot := botSkillMatch(t, skillPlayer{"b", 1, "flora", 300}, skillPlayer{"e1", 2, "rookie", 900},
		skillPlayer{"a1", 1, "rookie", 600})
	brain := NewBotBrain(perfectAim)
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode == "skill" {
		t.Fatal("bloom at full HP")
	}
	m.State.Players["a1"].StatusEffects = []StatusEffect{{EffectID: "root", DurationTurn: 1}}
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode != "skill" {
		t.Fatal("no bloom for an afflicted ally")
	}
	m.State.Players["a1"].StatusEffects = nil
	bot.HP = bot.MaxHP / 2
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode != "skill" {
		t.Fatal("no bloom at half HP")
	}
	bot.SkillEnergy = 80
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode == "skill" {
		t.Fatal("skill without full energy")
	}
}

func TestBloomPercentPerRank(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		if bloomSelfPercent(rank) != 40+5*rank || bloomAllyPercent(rank) != 30+5*rank {
			t.Errorf("bloom rank %d: self %d ally %d", rank, bloomSelfPercent(rank), bloomAllyPercent(rank))
		}
	}
}
