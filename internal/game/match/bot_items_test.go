package match

import (
	"context"
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// solidRect fills [x0, x1) × [y0, y1) with terrain.
func solidRect(tr *Terrain, x0, x1, y0, y1 int) {
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			setBit(tr.Mask, x, y, tr.Width)
		}
	}
}

// The medkit at or below rules.bots medkitHpPercent (35 %), always (perfectAim forgets never),
// never above it or at full HP, and not when the tier forgets.
func TestBotMedkitThreshold(t *testing.T) {
	for _, tc := range []struct {
		hp     int
		forget float64
		heal   bool
	}{
		{525, 0, true}, // 35 % of 1500
		{100, 0, true},
		{526, 0, false},
		{1500, 0, false},
		{100, 1, false},
	} {
		m, bot, _ := newBotMatch(t, 400, 1000, nil)
		bot.HP = tc.hp
		bot.Items = []string{"medkit"}
		m.BotTier.MedkitForgetChance = tc.forget
		m.BotTier.UseItemChance = 0 // the medkit does not roll the item chance
		act := NewBotBrain(m.BotTier).DecideAction(m, bot, botTurnState{})
		use, ok := act.(UseItemAction)
		if healed := ok && use.ItemID == "medkit"; healed != tc.heal {
			t.Fatalf("hp %d forget %v: %#v, want heal=%v", tc.hp, tc.forget, act, tc.heal)
		}
	}
}

// Deep in a pit with walls too tall to shoot over at the far enemy and to walk out of, the bot
// fires its teleport; it lands on safe ground outside the pit, from where a shot hits.
func TestBotTeleportsOutOfAPit(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 300, 1300, nil)
	solidRect(m.Terrain, 220, 260, 100, 496) // left wall
	solidRect(m.Terrain, 340, 380, 100, 496) // right wall
	bot.Items = []string{"teleport"}
	m.BotTier.UseItemChance = 0 // the teleport does not roll the item chance
	brain := NewBotBrain(m.BotTier)
	weapon := gamedata.Current().Data.Weapons["w1"]
	if s := brain.bestShot(m, bot, bot.Position, weapon); s.damage > 0 {
		t.Fatalf("fixture: the pit does not block (%.0f°/%.0f deals %.0f)", s.angle, s.power, s.damage)
	}

	act, ok := brain.DecideAction(m, bot, botTurnState{}).(ShootAction)
	if !ok || act.ActionMode != "item" || act.ItemID == nil || *act.ItemID != "teleport" {
		t.Fatalf("want a teleport, got %#v", act)
	}
	r := SimulateProjectile(bot.PlayerID, bot.TeamID, shotOrigin(m.Terrain, bot), act.Angle, act.Power, weapon, m.State.Wind,
		m.Terrain, m.State.Players, 0)
	at, ok := teleportLanding(m.Terrain, r)
	if !ok || !brain.safeLanding(m, bot, at) || (at.X > 220 && at.X < 380) {
		t.Fatalf("teleport %.0f°/%.0f lands at %+v (ok %v), want safe ground outside the pit", act.Angle, act.Power, at, ok)
	}
	if !m.Terrain.CanStandAt(at.X, at.Y) || math.Abs(at.X-enemy.Position.X) < float64(m.gd.Rules.Bots.TeleportEnemyMarginPx) {
		t.Fatalf("landing %+v not standable or next to the enemy", at)
	}

	m.processShoot(context.Background(), nil, act)
	if bot.Position != at || len(bot.Items) != 0 {
		t.Fatalf("bot at %+v items %v after the teleport, want %+v", bot.Position, bot.Items, at)
	}
	if s := brain.bestShot(m, bot, bot.Position, weapon); s.damage <= 0 {
		t.Fatalf("no damaging shot from the landing %+v", at)
	}
}

// Lava next to the spot the teleport would pick: the bot picks another spot, at least
// rules.bots teleportLavaMarginPx from it.
func TestBotTeleportAvoidsLava(t *testing.T) {
	m, bot, _ := newBotMatch(t, 300, 1300, nil)
	solidRect(m.Terrain, 220, 260, 100, 496)
	solidRect(m.Terrain, 340, 380, 100, 496)
	bot.Items = []string{"teleport"}
	brain := NewBotBrain(m.BotTier)
	weapon := gamedata.Current().Data.Weapons["w1"]
	_, first, ok := brain.teleportShot(m, bot, weapon)
	if !ok {
		t.Fatal("fixture: no teleport out of the pit")
	}
	lava := TerrainZone{Type: "lava", MinX: first.X - 10, MaxX: first.X + 10, MinY: first.Y - 5, MaxY: first.Y + 5}
	m.Terrain.Zones = append(m.Terrain.Zones, lava)
	_, at, ok := brain.teleportShot(m, bot, weapon)
	margin := float64(m.gd.Rules.Bots.TeleportLavaMarginPx)
	if ok && at.X > lava.MinX-margin && at.X < lava.MaxX+margin && math.Abs(at.Y-first.Y) < 10 {
		t.Fatalf("teleport lands at %+v, next to the lava around %+v", at, first)
	}
}

// When no shot can hit (the enemy sits in a bunker wider than the blast) the bot fires the
// shot that lands closest to it, not a fixed 45°/50.
func TestBotFallbackShotLandsClosest(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 200, 1000, nil)
	solidRect(m.Terrain, 920, 1080, 400, 416) // roof
	solidRect(m.Terrain, 920, 936, 416, 496)  // walls
	solidRect(m.Terrain, 1064, 1080, 416, 496)
	bot.MoveEnergy = 0
	brain := NewBotBrain(m.BotTier)
	weapon := gamedata.Current().Data.Weapons["w1"]

	act := decideShot(t, m, bot)
	got := brain.evalShot(m, bot, bot.Position, act.Angle, act.Power, weapon)
	fixed := brain.evalShot(m, bot, bot.Position, 45, 50, weapon)
	if got.damage > 0 || fixed.damage > 0 {
		t.Fatalf("fixture: a shot hits the bunker (%.0f / %.0f)", got.damage, fixed.damage)
	}
	if (act.Angle == 45 && act.Power == 50) || !got.found || got.miss >= fixed.miss || got.allyRisk > 0 {
		t.Fatalf("fired %.0f°/%.0f landing %.0fpx from the enemy at %+v; 45°/50 lands %.0fpx away", act.Angle, act.Power, got.miss,
			enemy.Position, fixed.miss)
	}
	if got.miss > 100 {
		t.Fatalf("closest landing %.0fpx away, want next to the bunker", got.miss)
	}

	// against a strong wind the closest landing is still computed with it
	m.State.Wind = WindState{Direction: -1, Power: 4}
	act = decideShot(t, m, bot)
	if s := brain.evalShot(m, bot, bot.Position, act.Angle, act.Power, weapon); !s.found || s.miss > 100 {
		t.Fatalf("with wind: %.0f°/%.0f lands %.0fpx away", act.Angle, act.Power, s.miss)
	}
}

// power_shot rides a shot that hits, never a deliberate miss.
func TestBotPowerShotOnAHit(t *testing.T) {
	m, bot, _ := newBotMatch(t, 400, 1000, nil)
	bot.Items = []string{"power_shot"}
	act := decideShot(t, m, bot)
	if act.ActionMode != "item" || act.ItemID == nil || *act.ItemID != "power_shot" {
		t.Fatalf("hit without the power shot: %#v", act)
	}
	m.BotTier.HitChance = 0
	if act := decideShot(t, m, bot); act.ActionMode != "weapon" {
		t.Fatalf("power shot wasted on a miss: %#v", act)
	}
	m.BotTier.HitChance, m.BotTier.UseItemChance = 100, 0
	if act := decideShot(t, m, bot); act.ActionMode != "weapon" {
		t.Fatalf("item used with useItemChance 0: %#v", act)
	}
}

// Behind a thin wall (and unable to walk) the bot fires its drill bomb through it.
func TestBotDrillsThroughAWall(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 600, 1200, nil)
	solidRect(m.Terrain, 500, 720, 380, 400) // roof
	solidRect(m.Terrain, 700, 720, 400, 496) // right wall, 20px: the drill pierces 50
	bot.MoveEnergy = 0
	bot.Items = []string{"drill_bomb"}
	act := decideShot(t, m, bot)
	if act.ActionMode != "item" || act.ItemID == nil || *act.ItemID != "drill_bomb" {
		t.Fatalf("want the drill bomb, got %#v", act)
	}
	hp := enemy.HP
	m.processShoot(context.Background(), nil, act)
	if enemy.HP >= hp {
		t.Fatalf("the drilled shot %.0f°/%.0f missed", act.Angle, act.Power)
	}
}

// Boxed in behind a thick wall the bot calls an air strike on the enemy.
func TestBotAirStrikeWhenBoxedIn(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 600, 1200, nil)
	solidRect(m.Terrain, 500, 720, 380, 400) // roof
	solidRect(m.Terrain, 640, 720, 400, 496) // right wall, 80px
	bot.MoveEnergy = 0
	bot.Items = []string{"air_strike"}
	act := decideShot(t, m, bot)
	if act.ActionMode != "item" || act.ItemID == nil || *act.ItemID != "air_strike" || act.TargetX == nil {
		t.Fatalf("want an air strike, got %#v", act)
	}
	hp := enemy.HP
	m.processShoot(context.Background(), nil, act)
	if enemy.HP >= hp {
		t.Fatalf("the air strike at x=%.0f missed the enemy at %+v", *act.TargetX, enemy.Position)
	}
}

// A wind that blows every shot away: the bot stops it, then hits.
func TestBotStopsARuinousWind(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 400, 1000, nil)
	w := m.gd.Data.Weapons["w1"]
	w.WindInfluence = 20
	m.gd.Data.Weapons["w1"] = w
	m.State.Wind = WindState{Direction: -1, Power: 4}
	bot.MoveEnergy = 0
	bot.Items = []string{"wind_stopper"}
	brain := NewBotBrain(m.BotTier)
	act := brain.DecideAction(m, bot, botTurnState{})
	if use, ok := act.(UseItemAction); !ok || use.ItemID != "wind_stopper" {
		t.Fatalf("want the wind stopper, got %#v", act)
	}
	m.processUseItem(context.Background(), nil, act.(UseItemAction))
	turn := botTurnState{}
	turn.noteItem("wind_stopper")
	shot, ok := brain.DecideAction(m, bot, turn).(ShootAction)
	if !ok {
		t.Fatalf("no shot after the wind stopper")
	}
	assertShotLands(t, m, bot, enemy, shot)
}
