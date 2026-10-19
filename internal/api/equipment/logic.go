package equipment

import (
	"math"
	"sort"
	"strconv"
)

// logic.go contains all pure gameplay calculation functions.
// These are extracted from service.go for testability without DB dependencies.

// StonePowers maps stone level (1-12) to its seeded power value.
// Power follows geometric progression ×3. Runtime code must read config_stones
// instead, since admins can edit power.
var StonePowers = [13]int{0, 1, 3, 9, 27, 81, 243, 729, 2187, 6561, 19683, 59049, 177147}

// CalculateUpgradePercent computes the success probability for an upgrade attempt.
// percent = totalPower * 100 / upgradeCost, capped at maxPercent.
func CalculateUpgradePercent(totalPower, upgradeCost int, maxPercent float64) float64 {
	if upgradeCost <= 0 {
		return 0
	}
	percent := float64(totalPower) * 100.0 / float64(upgradeCost)
	if percent > maxPercent {
		percent = maxPercent
	}
	if percent < 0 {
		percent = 0
	}
	return percent
}

// CalculateUpgradeMultiplier returns the stat multiplier for a given upgrade level.
// Formula: 1 + level*0.02 + milestone bonuses
// Milestones: level>=6 +0.10, level>=10 +0.20, level>=14 +0.40, level>=16 +1.00
func CalculateUpgradeMultiplier(upgradeLevel int) float64 {
	m := 1.0 + float64(upgradeLevel)*0.02
	if upgradeLevel >= 6 {
		m += 0.10
	}
	if upgradeLevel >= 10 {
		m += 0.20
	}
	if upgradeLevel >= 14 {
		m += 0.40
	}
	if upgradeLevel >= 16 {
		m += 1.00
	}
	return m
}

// getSafezoneStart returns the safezone floor level for a given upgrade level.
// +1-6: stays at current level (returns 0)
// +6-10: resets to +6
// +10-14: resets to +10
// +14-16: resets to +14
func getSafezoneStart(level int) int {
	if level >= 14 {
		return 14
	}
	if level >= 10 {
		return 10
	}
	if level >= 6 {
		return 6
	}
	return 0
}

// CalculateDismantleRefund computes stones returned when dismantling.
// powers maps stoneLevel → power and must come from config_stones, the same
// source UpgradeEquipment uses to value consumed stones.
// Returns a map of stoneLevel → quantity.
// Refund = 50% of totalPower used in current safezone, distributed as highest-value stones.
func CalculateDismantleRefund(totalPowerUsed int, powers map[int]int) map[int]int {
	levels := make([]int, 0, len(powers))
	for level, power := range powers {
		if power > 0 {
			levels = append(levels, level)
		}
	}
	sort.Slice(levels, func(i, j int) bool {
		pi, pj := powers[levels[i]], powers[levels[j]]
		if pi != pj {
			return pi > pj
		}
		return levels[i] > levels[j]
	})

	result := make(map[int]int)
	refund := totalPowerUsed / 2
	for _, stoneLevel := range levels {
		if refund <= 0 {
			break
		}
		power := powers[stoneLevel]
		qty := refund / power
		if qty > 0 {
			result[stoneLevel] = qty
			refund -= qty * power
		}
	}
	return result
}

// DefaultSellRate is the share of the buy price paid back when selling a piece,
// used when game_settings "equipment.sell_rate" is missing or invalid.
const DefaultSellRate = 0.25

// ParseSellRate reads the "equipment.sell_rate" setting: a float in (0, 1],
// else DefaultSellRate.
func ParseSellRate(raw string) float64 {
	rate, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(rate) || rate <= 0 || rate > 1 {
		return DefaultSellRate
	}
	return rate
}

// ItemPrice is what the shop charges for an item: priceCoin, else priceGem.
// amount 0 means the item is free (crafted).
func ItemPrice(cfg *EquipmentItemConfig) (currency string, amount int) {
	if cfg.PriceCoin > 0 {
		return "coin", cfg.PriceCoin
	}
	if cfg.PriceGem > 0 {
		return "gem", cfg.PriceGem
	}
	return "", 0
}

// StonePrice is what one stone costs: priceCoin, else priceGem; amount 0 means
// free.
func StonePrice(cfg *StoneConfig) (currency string, amount int) {
	if cfg.PriceCoin > 0 {
		return "coin", cfg.PriceCoin
	}
	if cfg.PriceGem > 0 {
		return "gem", cfg.PriceGem
	}
	return "", 0
}

// CalculateSellRefund is floor(price × rate). The epsilon keeps products that
// are whole in decimal (100 × 0.29) from flooring one below.
func CalculateSellRefund(price int, rate float64) int {
	if price <= 0 || rate <= 0 {
		return 0
	}
	return int(math.Floor(float64(price)*rate + 1e-9))
}

// CalculateMergeSuccessRate returns the merge success probability (0-100).
// Each item contributes 25%, so 2 items = 50%, 3 = 75%, 4 = 100%.
func CalculateMergeSuccessRate(count int) float64 {
	return float64(count) * 25.0
}

// ValidateMergeStone checks merge stone request parameters.
// Returns error string or "" if valid.
func ValidateMergeStone(stoneLevel, count int) string {
	if count < 2 {
		return "minimum 2 stones required"
	}
	if count > 4 {
		return "maximum 4 stones allowed"
	}
	if stoneLevel < 1 || stoneLevel > 12 {
		return "invalid stone level"
	}
	if stoneLevel >= 12 {
		return "already at max level"
	}
	return ""
}

// ValidateMergeGem checks merge gem request parameters.
func ValidateMergeGem(gemLevel, count int) string {
	if count < 2 {
		return "minimum 2 gems required"
	}
	if count > 4 {
		return "maximum 4 gems allowed"
	}
	if gemLevel < 1 || gemLevel > 10 {
		return "invalid gem level"
	}
	if gemLevel >= 10 {
		return "already at max level"
	}
	return ""
}

// CalculateGemPrice returns the currency and price per unit for buying gems.
// Level 1-3: coin (level * 200), Level 4-6: gem ((level-3) * 30)
func CalculateGemPrice(gemLevel int) (currency string, priceEach int) {
	if gemLevel <= 3 {
		return "coin", gemLevel * 200
	}
	return "gem", (gemLevel - 3) * 30
}

// CalculateEquipmentStat applies upgrade multiplier to a base stat value.
func CalculateEquipmentStat(baseStat int, upgradeLevel int) int {
	mul := CalculateUpgradeMultiplier(upgradeLevel)
	return int(float64(baseStat)*mul + 0.5) // round
}

// CalculateTotalStonePower sums up the total power from a list of stone inputs.
func CalculateTotalStonePower(stones []StoneInput) int {
	total := 0
	for _, s := range stones {
		if s.StoneLevel >= 1 && s.StoneLevel <= 12 && s.Quantity > 0 {
			total += StonePowers[s.StoneLevel] * s.Quantity
		}
	}
	return total
}

// UpgradeFailLevel returns the level equipment drops to on upgrade failure.
func UpgradeFailLevel(currentLevel, failResetTo int) int {
	return failResetTo
}

// UpgradeSuccessLevel returns the level equipment goes to on upgrade success.
func UpgradeSuccessLevel(currentLevel int) int {
	return currentLevel + 1
}

// CanEquipOnCharacter reports whether an item restricted to itemCharacterID
// (nil or empty = usable by any character) may be equipped on characterID.
func CanEquipOnCharacter(itemCharacterID *string, characterID string) bool {
	if itemCharacterID == nil || *itemCharacterID == "" {
		return true
	}
	return *itemCharacterID == characterID
}
