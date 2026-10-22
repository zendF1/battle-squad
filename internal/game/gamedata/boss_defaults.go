package gamedata

// DefaultBosses are the bosses migration 046_boss_mode seeds (the YAML fallback plays with
// them too); TestBossMigrationSeedsDefaults keeps the two equal. Original designs: a steel
// mech titan with drones, a lava crab with crabs and lava, an ice witch with snowmen and frost.
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
	return []BossConfig{
		{
			BossID: "steel_robot", Name: "Rô-bốt Thép",
			Description:      "Người khổng lồ cơ khí canh giữ xưởng sắt bỏ hoang. Nã pháo dồn dập, bắn tia laze xuyên đất và gọi drone che chắn.",
			RecommendedLevel: 10, Enabled: true, SortOrder: 1, MapID: "arena_steel_robot",
			HP: 12000, Defense: 60, Damage: 420, BodySize: 3, TurnLimit: 15, ShieldedDamageFactor: 0.1, OutOfMap: BossOutOfMapClamp,
			Skills: []BossSkill{
				{SkillID: "shell_barrage", Name: "Mưa đạn pháo", Description: "Mười quả đạn pháo dội xuống quanh một người chơi.",
					Kind: BossSkillRain, Weight: 3, Target: BossTargetRandom, Count: 10, Spread: 180, Damage: 0.35, Blast: 55, Crater: 14, Cap: 1.2},
				{SkillID: "laser_beam", Name: "Tia laze nghiền", Description: "Tia laze xuyên địa hình bắn thẳng vào người yếu nhất, trúng mọi người trên đường.",
					Kind: BossSkillBeam, Weight: 2, Cooldown: 2, Target: BossTargetLowestHP, Damage: 1.3, HalfWidth: 16, Decay: 0.85},
				{SkillID: "drone_wave", Name: "Gọi drone", Description: "Thả hai drone. Còn drone thì Rô-bốt Thép được che chắn.",
					Kind: BossSkillSummon, Weight: 2, Cooldown: 3, MinionID: "drone", Count: 2},
			},
			Minions: []MinionConfig{
				{MinionID: "drone", Name: "Drone", CharacterID: "spark", HP: 650, Defense: 20, Damage: 160, MoveEnergy: 80, BodySize: 0.7, HitChance: 45},
			},
			Waves:   BossWaves{Initial: []BossWaveEntry{{MinionID: "drone", Count: 2}}, MaxAlive: 4},
			Rewards: rewards([6]int{60, 1500, 400, 25, 600, 120}, [6]int{15, 400, 100, 8, 200, 40}), DailyBonusRuns: 3,
		},
		{
			BossID: "lava_crab", Name: "Cua Dung Nham",
			Description:      "Con cua khổng lồ sống trong miệng núi lửa. Ném bom dung nham để lại vũng lửa, kẹp càng nghiền nát người đến gần.",
			RecommendedLevel: 25, Enabled: true, SortOrder: 2, MapID: "arena_lava_crab",
			HP: 18000, Defense: 90, Damage: 480, BodySize: 3.2, TurnLimit: 15, ShieldedDamageFactor: 0.1, OutOfMap: BossOutOfMapClamp,
			Skills: []BossSkill{
				{SkillID: "fire_bombs", Name: "Bom dung nham", Description: "Bốn quả bom lửa rơi xuống, gây bỏng và để lại vũng dung nham hai lượt.",
					Kind: BossSkillRain, Weight: 3, Cooldown: 1, Target: BossTargetRandom, Count: 4, Spread: 260, Damage: 0.55, Blast: 60, Crater: 18, Cap: 1,
					Status: "burn", StatusTurns: 1, LavaRadius: 50, LavaTurns: 2, LavaDamage: 150},
				{SkillID: "claw_slam", Name: "Càng nghiền", Description: "Một cú kẹp nặng giáng xuống người đứng gần nhất.",
					Kind: BossSkillRain, Weight: 2, Cooldown: 1, Target: BossTargetNearest, Count: 1, Damage: 1.6, Blast: 80, Crater: 36},
				{SkillID: "crab_wave", Name: "Gọi cua con", Description: "Hai cua con bò ra. Còn cua con thì Cua Dung Nham được che chắn.",
					Kind: BossSkillSummon, Weight: 2, Cooldown: 3, MinionID: "crab", Count: 2},
			},
			Minions: []MinionConfig{
				{MinionID: "crab", Name: "Cua con", CharacterID: "tanko", HP: 800, Defense: 40, Damage: 180, MoveEnergy: 50, BodySize: 0.75, HitChance: 45},
			},
			Waves:   BossWaves{Initial: []BossWaveEntry{{MinionID: "crab", Count: 2}}, MaxAlive: 4},
			Rewards: rewards([6]int{80, 2000, 520, 32, 800, 160}, [6]int{20, 520, 130, 10, 260, 52}), DailyBonusRuns: 3,
		},
		{
			BossID: "ice_witch", Name: "Phù Thuỷ Băng",
			Description:      "Phù thuỷ của đỉnh tuyết vĩnh cửu. Gai băng đóng băng người trúng, tia băng giá giữ chân, bão tuyết phủ cả chiến trường.",
			RecommendedLevel: 40, Enabled: true, SortOrder: 3, MapID: "arena_ice_witch",
			HP: 24000, Defense: 80, Damage: 520, BodySize: 2.8, TurnLimit: 15, ShieldedDamageFactor: 0.1, OutOfMap: BossOutOfMapClamp,
			Skills: []BossSkill{
				{SkillID: "ice_spikes", Name: "Gai băng", Description: "Tám gai băng rơi quanh một người chơi; ai trúng bị đóng băng một lượt.",
					Kind: BossSkillRain, Weight: 3, Cooldown: 2, Target: BossTargetRandom, Count: 8, Spread: 220, Damage: 0.3, Blast: 45, Crater: 10, Cap: 0.9,
					Status: "freeze", StatusTurns: 1},
				{SkillID: "frost_beam", Name: "Tia băng giá", Description: "Tia băng xuyên địa hình bắn vào người yếu nhất, giữ chân mọi người trúng một lượt.",
					Kind: BossSkillBeam, Weight: 2, Cooldown: 1, Target: BossTargetLowestHP, Damage: 1.1, HalfWidth: 14, Decay: 0.9, Status: "root", StatusTurns: 1},
				{SkillID: "blizzard", Name: "Bão tuyết", Description: "Mười sáu mảnh băng rải khắp chiến trường, ai trúng bị lưới băng làm chậm.",
					Kind: BossSkillRain, Weight: 2, Cooldown: 1, Target: BossTargetRandom, Count: 16, Spread: 420, Damage: 0.18, Blast: 40, Crater: 8, Cap: 0.8,
					Status: "net", StatusTurns: 1},
				{SkillID: "snowman_wave", Name: "Gọi người tuyết", Description: "Hai người tuyết hiện ra. Còn người tuyết thì Phù Thuỷ Băng được che chắn.",
					Kind: BossSkillSummon, Weight: 2, Cooldown: 3, MinionID: "snowman", Count: 2},
			},
			Minions: []MinionConfig{
				{MinionID: "snowman", Name: "Người tuyết", CharacterID: "flora", HP: 900, Defense: 30, Damage: 170, MoveEnergy: 40, BodySize: 0.85, HitChance: 50},
			},
			Waves:   BossWaves{Initial: []BossWaveEntry{{MinionID: "snowman", Count: 3}}, MaxAlive: 4},
			Rewards: rewards([6]int{96, 2400, 640, 40, 960, 192}, [6]int{24, 640, 160, 13, 320, 64}), DailyBonusRuns: 3,
		},
	}
}
