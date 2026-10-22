package match

import (
	"math"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
)

// Shorthands for the default game data numbers the tests check against (the rules moved into
// the DB; gamedata.DefaultRules and the Default*Tuning are their fallbacks).
// TestShorthandsAreTheDefaults keeps them equal.
const (
	ShotOriginLift           = 20
	PlayerHeight             = 40
	maxClimbPerStep          = 3
	maxShotPower             = 100.0
	bowlDepth                = 0.72
	bowlCeiling              = 1.5
	craterPxPerTerrainDamage = 0.6
	MaxTerrainDamageBonus    = 50
	netMoveEnergyCap         = 50
	skillEnergyMax           = 100
	skillEnergyPerTurn       = 20
	skillEnergyOnHit         = 10
	burnMaxHPPercent         = 10
	shieldCapPercent         = 50
	skipTurnDelay            = 2.0
	NoAimAngle               = 90.0
	NoAimPower               = 50.0
	botFollowUpDelay         = 1500 * time.Millisecond

	rainDelay    = 0.5
	rainInterval = 0.05
	rainStartY   = -60.0
	rainBlast    = 0.7
	rainCrater   = 1.0
	rainCap      = 2.0
	rainFocus    = 2.0

	chainLaunchGap = 0.1
	chainBlast     = 1.3
	chainHitGap    = 0.08
	chainFallSpeed = 3.0

	laserChargeTime = 0.3
	laserDecay      = 0.8

	bloomRiseTime  = 0.55
	bloomCrossTime = 0.2
	bloomSeedTime  = 1.4
	bloomLinger    = 0.9
	bloomSkyY      = -140.0
)

var (
	defRain  = gamedata.DefaultRainTuning()
	defChain = gamedata.DefaultChainTuning()
	defLaser = gamedata.DefaultLaserTuning()
	defBloom = gamedata.DefaultBloomTuning()
)

func TestShorthandsAreTheDefaults(t *testing.T) {
	r, p := gamedata.DefaultRules(), gamedata.DefaultPhysics()
	eq := func(name string, got, want float64) {
		if got != want {
			t.Errorf("%s: shorthand %v, default %v", name, got, want)
		}
	}
	eq("ShotOriginLift", ShotOriginLift, float64(r.Physics.ShotOriginLift))
	eq("PlayerHeight", PlayerHeight, float64(r.Terrain.PlayerHeight))
	eq("maxClimbPerStep", maxClimbPerStep, float64(r.Terrain.MaxClimbPerStep))
	eq("maxShotPower", maxShotPower, r.Aim.PowerMax)
	eq("bowlDepth", bowlDepth, r.Terrain.BowlDepth)
	eq("bowlCeiling", bowlCeiling, r.Terrain.BowlCeiling)
	eq("craterPxPerTerrainDamage", craterPxPerTerrainDamage, r.Terrain.CraterPxPerTerrainDamage)
	eq("MaxTerrainDamageBonus", MaxTerrainDamageBonus, float64(r.Terrain.MaxTerrainDamageBonusPercent))
	eq("netMoveEnergyCap", netMoveEnergyCap, gamedata.DefaultNetMoveEnergyCap)
	eq("skillEnergyMax", skillEnergyMax, float64(r.SkillEnergy.Max))
	eq("skillEnergyPerTurn", skillEnergyPerTurn, float64(r.SkillEnergy.PerTurn))
	eq("skillEnergyOnHit", skillEnergyOnHit, float64(r.SkillEnergy.OnHit))
	eq("burnMaxHPPercent", burnMaxHPPercent, float64(r.Combat.BurnMaxHPPercent))
	eq("shieldCapPercent", shieldCapPercent, float64(r.Combat.ShieldCapPercent))
	eq("skipTurnDelay", skipTurnDelay, r.Timings.SkipTurnDelaySec)
	eq("NoAimAngle", NoAimAngle, r.Aim.NoAimAngle)
	eq("NoAimPower", NoAimPower, r.Aim.NoAimPower)
	eq("botFollowUpDelay", botFollowUpDelay.Seconds(), r.Timings.BotFollowUpDelaySec)
	eq("rainDelay", rainDelay, defRain.Delay)
	eq("rainInterval", rainInterval, defRain.Interval)
	eq("rainStartY", rainStartY, defRain.StartY)
	eq("rainBlast", rainBlast, defRain.Blast)
	eq("rainCrater", rainCrater, defRain.Crater)
	eq("rainCap", rainCap, defRain.Cap)
	eq("rainFocus", rainFocus, defRain.Focus)
	eq("chainLaunchGap", chainLaunchGap, defChain.LaunchGap)
	eq("chainBlast", chainBlast, defChain.Blast)
	eq("chainHitGap", chainHitGap, defChain.HitGap)
	eq("chainFallSpeed", chainFallSpeed, defChain.FallSpeed)
	eq("laserChargeTime", laserChargeTime, defLaser.ChargeTime)
	eq("laserDecay", laserDecay, defLaser.Decay)
	eq("bloomRiseTime", bloomRiseTime, defBloom.RiseTime)
	eq("bloomCrossTime", bloomCrossTime, defBloom.CrossTime)
	eq("bloomSeedTime", bloomSeedTime, defBloom.SeedTime())
	eq("bloomLinger", bloomLinger, defBloom.Linger)
	eq("bloomSkyY", bloomSkyY, defBloom.SkyY)
	eq("turn seconds", 20, float64(p.TurnTimeSeconds))
	eq("px per energy", 2, p.PxPerEnergy())
	eq("energy per px", 0.5, p.EnergyPerPx())
	eq("fall threshold", 150, p.FallDamageThreshold)
	eq("fall per px", 3.75, p.FallDamagePerPixel)
}

// useData plays the test with d as the game data (the default physics and rules).
func useData(t testing.TB, d *gamedata.GameData) {
	gamedata.UseForTest(t, gamedata.Current().WithData(d))
}

// usePhysics plays the test with physics p.
func usePhysics(t testing.TB, p *gamedata.PhysicsConfig) {
	gamedata.UseForTest(t, gamedata.Current().WithPhysics(p))
}

// useBricks plays the test with the brick types b.
func useBricks(t testing.TB, b map[int]gamedata.BrickTypeConfig) {
	gamedata.UseForTest(t, gamedata.Current().WithBricks(b))
}

// useConfigDir plays the test with configs/*.yaml as the game data.
func useConfigDir(t testing.TB) {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	snap, err := gamedata.LoadYAML(filepath.Join(filepath.Dir(self), "..", "..", "..", "configs"))
	if err != nil {
		t.Fatal(err)
	}
	gamedata.UseForTest(t, snap)
}

// testCapOfBase is a skill cap of base damage with the current game data's defense K.
func testCapOfBase(base float64) func(p *BattlePlayerState) int {
	k := gamedata.DefenseK()
	return func(p *BattlePlayerState) int { return int(math.Round(reduceByDefense(base, p.Defense, k))) }
}

// testGravity is the current game data's gravity.
func testGravity() float64 { return gamedata.Current().Physics.Gravity }
