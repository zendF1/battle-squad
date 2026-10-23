package gamedata

// DefaultBosses are the bosses migrations 046_boss_mode, 048_boss_mode_v2, 049_boss_placement,
// 050_boss_mode_v3 and 052_boss_status_items seed (the YAML fallback plays with them too); TestBossMigration keeps the two
// equal. Boss v2: the drones are flying bombers, the crabs walking bombers (on the mound arena)
// and the witch hovers (y 320..430, below the client's boss HP bar), 30 turns. Boss v3: minions
// move 3 × as far (drones fly 270 px, crabs walk 240 px) and hit harder (bombers' shots 0.8 ×,
// snowmen 1.3 ×), the snowmen are leapers on the witch's sky ice islands, every boss summons for
// free at the start of its turn (summon: 3 drones, 4 crabs, 5 snowmen = maxAlive) and attacks with
// wide skills (×1.5 damage and craters): the robot's party-wide shell rain and 5-beam sweep, the
// crab's map-wide fire bombs and ground shockwave, the witch's wide ice spikes, 20-beam radial
// burst (stun) and map-wide blizzard (freeze; its ice shards leave a ring around the witch); each
// rain and minion shot has its own projectile kind. 052: a status lands on a hit party member half
// of the time (statusChance 0.5, rolled once per member and cast), and the status skills hit 1.3 x
// as hard (damage and caps). HP, the skills' caps and the minion blasts are retuned with the bot
// balance runs (TestBossBalanceSim: 4 bots at the recommended level win about half of their
// fights): robot 10000 HP, crab 15000, witch 14000 (050: 13000). Original designs: a steel mech
// titan with drones, a lava crab with crabs and lava, an ice witch with snowmen and frost.
func DefaultBosses() []BossConfig {
	// t is {goldPer1000Damage, maxGold, winGold, expPer1000Damage, maxExp, winExp}
	bonus := func(t [6]int) BossRewardTable {
		return BossRewardTable{
			BaseGold: 100, GoldPer1000Damage: float64(t[0]), MaxGold: t[1], WinGold: t[2],
			BaseExp: 40, ExpPer1000Damage: float64(t[3]), MaxExp: t[4], WinExp: t[5],
			DamagePerDropRoll: 2000, MaxDropRolls: 4, WinDropRolls: 1, EmptyWeight: 2,
			Drops: []BossDrop{
				{ItemID: "medkit", Weight: 3, Min: 1, Max: 2},
				{ItemID: "power_shot", Weight: 2, Min: 1, Max: 1},
				{ItemID: "teleport", Weight: 2, Min: 1, Max: 1},
				{ItemID: "drill_bomb", Weight: 2, Min: 1, Max: 1},
				{ItemID: "freeze_bomb", Weight: 1, Min: 1, Max: 1},
			},
		}
	}
	normal := func(t [6]int) BossRewardTable {
		return BossRewardTable{
			BaseGold: 30, GoldPer1000Damage: float64(t[0]), MaxGold: t[1], WinGold: t[2],
			BaseExp: 20, ExpPer1000Damage: float64(t[3]), MaxExp: t[4], WinExp: t[5],
			DamagePerDropRoll: 6000, MaxDropRolls: 1, WinDropRolls: 0, EmptyWeight: 6,
			Drops: []BossDrop{
				{ItemID: "medkit", Weight: 3, Min: 1, Max: 1},
				{ItemID: "power_shot", Weight: 1, Min: 1, Max: 1},
				{ItemID: "teleport", Weight: 1, Min: 1, Max: 1},
			},
		}
	}
	rewards := func(b, n [6]int) BossRewards {
		return BossRewards{MinionDamageFactor: 0.3, Bonus: bonus(b), Normal: normal(n)}
	}
	detonate := true
	return []BossConfig{
		{
			BossID: "steel_robot", Name: "Rô-bốt Thép",
			Description:      "Người khổng lồ cơ khí canh giữ xưởng sắt bỏ hoang. Nã pháo phủ kín chiến trường, quét năm tia laze ngang map và thả drone che chắn.",
			RecommendedLevel: 10, Enabled: true, SortOrder: 1, MapID: "arena_steel_robot",
			HP: 10000, Defense: 60, Damage: 630, BodySize: 3, TurnLimit: 15, ShieldedDamageFactor: 0.1, OutOfMap: BossOutOfMapClamp,
			Skills: []BossSkill{
				{SkillID: "shell_barrage", Name: "Mưa đạn pháo", Description: "Hai mươi bốn quả đạn pháo dội xuống bảy phần mười chiến trường quanh cả đội.",
					Kind: BossSkillRain, Weight: 3, Target: BossTargetParty, Count: 24, Spread: 840, Damage: 0.3, Blast: 75, Crater: 21, Cap: 1, ProjectileKind: "steel_shell"},
				{SkillID: "laser_beam", Name: "Tia laze nghiền", Description: "Năm tia laze xuyên địa hình quét ngang toàn map ở độ cao của từng người chơi.",
					Kind: BossSkillBeam, Weight: 2, Cooldown: 2, Pattern: BeamSweep, Count: 5, Damage: 0.9, HalfWidth: 16},
			},
			Minions: []MinionConfig{
				{MinionID: "drone", Name: "Drone", CharacterID: "spark", HP: 650, Defense: 20, Damage: 160, MoveEnergy: 80, BodySize: 0.7, HitChance: 45,
					Behavior: MinionBomber, Movement: MinionMoveFly, ShotDamageFactor: 0.8, DetonateRange: 70, FlyRange: 270, HoverHeight: 60,
					FlySpeed: 220, ExplodeDamageOf: ExplodeOfTarget, ExplodeDamagePct: 50, ExplodeRadius: 80, ExplodeCrater: 20, ProjectileKind: "drone_bolt"},
			},
			Waves:   BossWaves{Initial: []BossWaveEntry{{MinionID: "drone", Count: 2}}, MaxAlive: 3},
			Summon:  &BossSummon{MinionID: "drone", Count: 3, CooldownRounds: 3, FirstRound: 2},
			Rewards: rewards([6]int{60, 1500, 400, 25, 600, 120}, [6]int{15, 400, 100, 8, 200, 40}), DailyBonusRuns: 3,
		},
		{
			BossID: "lava_crab", Name: "Cua Dung Nham",
			Description:      "Con cua khổng lồ sống trong miệng núi lửa. Ném bom dung nham khắp map để lại vũng lửa, đập càng tạo sóng chấn động chạy dọc mặt đất.",
			RecommendedLevel: 25, Enabled: true, SortOrder: 2, MapID: "arena_lava_crab_mound",
			HP: 15000, Defense: 90, Damage: 720, BodySize: 3.2, TurnLimit: 15, ShieldedDamageFactor: 0.1, OutOfMap: BossOutOfMapClamp,
			Skills: []BossSkill{
				{SkillID: "fire_bombs", Name: "Bom dung nham", Description: "Mười quả bom lửa rơi khắp chiến trường; ai trúng có 50 % bị bỏng, mỗi chỗ nổ để lại vũng dung nham một lượt.",
					Kind: BossSkillRain, Weight: 3, Cooldown: 1, Target: BossTargetMap, Count: 10, Damage: 0.455, Blast: 80, Crater: 27, Cap: 0.91,
					Status: "burn", StatusTurns: 1, StatusChance: 0.5, LavaRadius: 40, LavaTurns: 1, LavaDamage: 120, ProjectileKind: "magma_bomb"},
				{SkillID: "claw_slam", Name: "Cú kẹp đập", Description: "Càng đập xuống đất, sóng chấn động chạy ra hai mép map, trúng mọi người đứng trên mặt đất.",
					Kind: BossSkillShockwave, Weight: 2, Cooldown: 1, Damage: 0.6, Blast: 120, Crater: 14, Step: 150, Interval: 0.08, MinFactor: 0.4},
			},
			Minions: []MinionConfig{
				{MinionID: "crab", Name: "Cua con", CharacterID: "tanko", HP: 800, Defense: 40, Damage: 180, MoveEnergy: 120, BodySize: 0.75, HitChance: 45,
					Behavior: MinionBomber, Movement: MinionMoveWalk, ShotDamageFactor: 0.8, DetonateRange: 70, WalkRange: 240,
					ExplodeDamageOf: ExplodeOfTarget, ExplodeDamagePct: 35, ExplodeRadius: 80, ExplodeCrater: 20, ProjectileKind: "crab_spit"},
			},
			Waves:   BossWaves{Initial: []BossWaveEntry{{MinionID: "crab", Count: 2}}, MaxAlive: 4},
			Summon:  &BossSummon{MinionID: "crab", Count: 4, CooldownRounds: 3, FirstRound: 2},
			Rewards: rewards([6]int{80, 2000, 520, 32, 800, 160}, [6]int{20, 520, 130, 10, 260, 52}), DailyBonusRuns: 3,
		},
		{
			BossID: "ice_witch", Name: "Phù Thuỷ Băng",
			Description:      "Phù thuỷ của đỉnh tuyết vĩnh cửu, bay giữa các đảo băng. Hai mươi tia băng toả tròn làm choáng, bão tuyết phủ cả map đóng băng người trúng, người tuyết nhảy tới nổ tung.",
			RecommendedLevel: 40, Enabled: true, SortOrder: 3, MapID: "arena_ice_witch_islands",
			HP: 14000, Defense: 80, Damage: 780, BodySize: 2.8, TurnLimit: 30, ShieldedDamageFactor: 0.1, OutOfMap: BossOutOfMapClamp,
			Flying: true, Hover: &BossHover{MinX: 400, MaxX: 2000, MinY: 320, MaxY: 430, MinMove: 200, FlySpeed: 220},
			Skills: []BossSkill{
				{SkillID: "ice_spikes", Name: "Gai băng", Description: "Mười bốn gai băng rơi rộng quanh một người chơi; ai trúng có 50 % bị đóng băng một lượt.",
					Kind: BossSkillRain, Weight: 3, Cooldown: 2, Target: BossTargetRandom, Count: 14, Spread: 360, Damage: 0.325, Blast: 65, Crater: 15, Cap: 0.91,
					Status: "freeze", StatusTurns: 1, StatusChance: 0.5, ProjectileKind: "ice_spike"},
				{SkillID: "frost_beam", Name: "Tia băng", Description: "Hai mươi tia băng toả tròn đều từ phù thuỷ ra tới mép map, xuyên địa hình; ai trúng có 50 % bị choáng một lượt.",
					Kind: BossSkillBeam, Weight: 2, Cooldown: 3, Pattern: BeamRadial, Count: 20, Damage: 1.04, HalfWidth: 14, Status: "stun", StatusTurns: 1, StatusChance: 0.5},
				{SkillID: "blizzard", Name: "Bão tuyết", Description: "Bốn mươi mảnh băng từ quanh phù thuỷ bay ra trút xuống khắp chiến trường; ai trúng có 50 % bị đóng băng một lượt.",
					Kind: BossSkillRain, Weight: 2, Cooldown: 3, Target: BossTargetMap, Count: 40, Damage: 0.195, Blast: 50, Crater: 12, Cap: 0.78,
					Status: "freeze", StatusTurns: 1, StatusChance: 0.5, ProjectileKind: "ice_shard", LaunchFrom: RainFromBoss},
			},
			Minions: []MinionConfig{
				{MinionID: "snowman", Name: "Người tuyết", CharacterID: "flora", HP: 900, Defense: 30, Damage: 170, MoveEnergy: 40, BodySize: 0.85, HitChance: 50,
					Behavior: MinionLeaper, Movement: MinionMoveNone, ShotDamageFactor: 1.3, DetonateRange: 70, ExplodeOnDeath: true,
					ExplodeDamageOf: ExplodeOfTarget, ExplodeDamagePct: 15, ExplodeRadius: 140, ExplodeCrater: 24, LeapDetonateNextTurn: &detonate, ProjectileKind: "snowball"},
			},
			Waves:   BossWaves{Initial: []BossWaveEntry{{MinionID: "snowman", Count: 3}}, MaxAlive: 5},
			Summon:  &BossSummon{MinionID: "snowman", Count: 5, CooldownRounds: 3, FirstRound: 2},
			Rewards: rewards([6]int{96, 2400, 640, 40, 960, 192}, [6]int{24, 640, 160, 13, 320, 64}), DailyBonusRuns: 3,
		},
	}
}
