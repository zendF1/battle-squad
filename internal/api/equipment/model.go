package equipment

import (
	"encoding/json"
	"time"
)

// --- Database models ---

type PlayerEquipment struct {
	EquipmentID  string    `json:"equipmentId"`
	PlayerID     string    `json:"playerId"`
	ItemID       string    `json:"itemId"`
	Slot         string    `json:"slot"`
	Category     string    `json:"category"`
	Tier         *string   `json:"tier,omitempty"`
	UpgradeLevel int       `json:"upgradeLevel"`
	GemSlot1     *string   `json:"gemSlot1,omitempty"`
	GemSlot2     *string   `json:"gemSlot2,omitempty"`
	IsEquipped   bool      `json:"isEquipped"`
	EquippedOn   *string   `json:"equippedOn,omitempty"`
	IsLocked     bool      `json:"isLocked"`
	CreatedAt    time.Time `json:"createdAt"`
}

type PlayerGem struct {
	GemID     string    `json:"gemId"`
	PlayerID  string    `json:"playerId"`
	GemType   string    `json:"gemType"`
	GemLevel  int       `json:"gemLevel"`
	CreatedAt time.Time `json:"createdAt"`
}

type PlayerStone struct {
	PlayerID   string `json:"playerId"`
	StoneLevel int    `json:"stoneLevel"`
	Quantity   int    `json:"quantity"`
}

type PlayerMaterial struct {
	PlayerID   string `json:"playerId"`
	MaterialID string `json:"materialId"`
	Quantity   int    `json:"quantity"`
}

// --- Config models ---

type EquipmentItemConfig struct {
	ItemID         string  `json:"itemId"`
	Name           string  `json:"name"`
	Slot           string  `json:"slot"`
	Category       string  `json:"category"`
	Tier           *string `json:"tier,omitempty"`
	RequiredLevel  int     `json:"requiredLevel"`
	CharacterID    *string `json:"characterId,omitempty"`
	GemSlots       int     `json:"gemSlots"`
	StatHP         int     `json:"statHp"`
	StatDamage     int     `json:"statDamage"`
	StatDefense    int     `json:"statDefense"`
	StatCrit       float64 `json:"statCrit"`
	StatMoveEnergy int     `json:"statMoveEnergy"`
	PriceCoin      int     `json:"priceCoin"`
	PriceGem       int     `json:"priceGem"`
	IsActive       bool    `json:"isActive"`
}

type UpgradeRateConfig struct {
	FromLevel   int     `json:"fromLevel"`
	ToLevel     int     `json:"toLevel"`
	UpgradeCost int     `json:"upgradeCost"`
	MaxPercent  float64 `json:"maxPercent"`
	FailResetTo int     `json:"failResetTo"`
	// CarefulGemCost: gems a careful upgrade of this step costs.
	CarefulGemCost int `json:"carefulGemCost"`
	// CarefulBonusPercent: a careful upgrade multiplies the stone percent by
	// (1 + this / 100). Careful is offered only when both are > 0.
	CarefulBonusPercent float64 `json:"carefulBonusPercent"`
}

type StoneConfig struct {
	StoneLevel int    `json:"stoneLevel"`
	Power      int    `json:"power"`
	PriceCoin  int    `json:"priceCoin"`
	PriceGem   int    `json:"priceGem"`
	Source     string `json:"source"`
}

type GemConfig struct {
	GemType   string  `json:"gemType"`
	GemLevel  int     `json:"gemLevel"`
	StatValue float64 `json:"statValue"`
}

type SetBonusConfig struct {
	Tier           string  `json:"tier"`
	PiecesRequired int     `json:"piecesRequired"`
	BonusHPPct     float64 `json:"bonusHpPct"`
	BonusDMGPct    float64 `json:"bonusDmgPct"`
	BonusDEFPct    float64 `json:"bonusDefPct"`
	BonusCritPct   float64 `json:"bonusCritPct"`
}

type MaterialConfig struct {
	MaterialID  string `json:"materialId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	PriceGem    int    `json:"priceGem"`
	Tier        string `json:"tier"`
	IsActive    bool   `json:"isActive"`
}

type CraftingRecipe struct {
	RecipeID     string           `json:"recipeId"`
	ResultItemID string           `json:"resultItemId"`
	Materials    []RecipeMaterial `json:"materials"`
	IsActive     bool             `json:"isActive"`
}

type RecipeMaterial struct {
	MaterialID string `json:"materialId"`
	Quantity   int    `json:"quantity"`
}

// UnmarshalJSON reads materialId, or material_id as written by recipes seeded
// before migrations/016.
func (m *RecipeMaterial) UnmarshalJSON(data []byte) error {
	var raw struct {
		MaterialID       string `json:"materialId"`
		LegacyMaterialID string `json:"material_id"`
		Quantity         int    `json:"quantity"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.MaterialID = raw.MaterialID
	if m.MaterialID == "" {
		m.MaterialID = raw.LegacyMaterialID
	}
	m.Quantity = raw.Quantity
	return nil
}

// --- Request types ---

type EquipRequest struct {
	EquipmentID string `json:"equipmentId"`
	CharacterID string `json:"characterId"`
}

type UnequipRequest struct {
	EquipmentID string `json:"equipmentId"`
}

type UpgradeRequest struct {
	EquipmentID string       `json:"equipmentId"`
	Stones      []StoneInput `json:"stones"`
	// Careful (optional): also pay the step's carefulGemCost gem to multiply
	// the stone percent by (1 + carefulBonusPercent / 100).
	Careful bool `json:"careful"`
	// ExpectedLevel (optional): the upgrade level the client saw; another
	// current level is EQUIP_MODIFIED and nothing is charged, so a blind
	// retry after a lost response does not roll the next step.
	ExpectedLevel *int `json:"expectedLevel,omitempty"`
}

type StoneInput struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
}

type DismantleRequest struct {
	EquipmentID string `json:"equipmentId"`
}

type SellRequest struct {
	EquipmentID string `json:"equipmentId"`
}

type SocketGemRequest struct {
	EquipmentID string `json:"equipmentId"`
	SlotIndex   int    `json:"slotIndex"`
	GemID       string `json:"gemId"`
}

type UnsocketGemRequest struct {
	EquipmentID string `json:"equipmentId"`
	SlotIndex   int    `json:"slotIndex"`
}

type BuyEquipmentRequest struct {
	ItemID string `json:"itemId"`
	// IdempotencyKey (optional, ≤ 64): a retry with the same key and item
	// returns the first purchase's equipment without charging again.
	IdempotencyKey string `json:"idempotencyKey"`
}

type BuyStoneRequest struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
	// IdempotencyKey (optional, ≤ 64): a retry with the same key, level and
	// quantity returns the first purchase without charging again.
	IdempotencyKey string `json:"idempotencyKey"`
}

type BuyGemRequest struct {
	GemType  string `json:"gemType"`
	GemLevel int    `json:"gemLevel"`
	Quantity int    `json:"quantity"`
}

type BuyMaterialRequest struct {
	MaterialID string `json:"materialId"`
	Quantity   int    `json:"quantity"`
}

type MergeStoneRequest struct {
	StoneLevel int `json:"stoneLevel"`
	Count      int `json:"count"`
}

type MergeGemRequest struct {
	GemType  string `json:"gemType"`
	GemLevel int    `json:"gemLevel"`
	Count    int    `json:"count"`
}

type CraftRequest struct {
	RecipeID string `json:"recipeId"`
}

// --- Response types ---

// Catalog is the GET /equipment/catalog data: every active item (normal and
// crafted), the upgrade rates, set bonuses, stones and the sell rate.
type Catalog struct {
	Items        []EquipmentItemConfig `json:"items"`
	UpgradeRates []UpgradeRateConfig   `json:"upgradeRates"`
	SetBonuses   []SetBonusConfig      `json:"setBonuses"`
	Stones       []StoneConfig         `json:"stones"`
	// SellRate: selling a piece pays floor(price × sellRate) of its currency.
	SellRate float64 `json:"sellRate"`
	// MergeRatePerItem: each stone or gem put into a merge adds this many
	// percent of success (game_settings equipment.merge_rate_per_item).
	MergeRatePerItem float64 `json:"mergeRatePerItem"`
	// UpgradeMilestones: the extra stat multiplier reached at each milestone
	// level (+6, +10, +14, +16), as battlestats applies it.
	UpgradeMilestones []UpgradeMilestone `json:"upgradeMilestones"`
	// WeaponUpgradeCostMultiplier: weapons need upgradeCost × this much stone
	// power (other slots × 1); maxPercent and failResetTo are unchanged.
	WeaponUpgradeCostMultiplier float64 `json:"weaponUpgradeCostMultiplier"`
}

// UpgradeMilestone is one milestone bonus: from Level on, item stats are
// multiplied by a further +Bonus (0.10 = +10%).
type UpgradeMilestone struct {
	Level int     `json:"level"`
	Bonus float64 `json:"bonus"`
}

// SellResult is the POST /player/equipment/sell data: the amount of currency
// paid, the new balance of that currency and the stones refunded for upgrades
// (ascending stoneLevel, empty when none).
type SellResult struct {
	Currency string       `json:"currency"`
	Amount   int          `json:"amount"`
	Balance  int          `json:"balance"`
	Stones   []StoneInput `json:"stones"`
}

// BuyStonesResult is the POST /shop/stones/buy data: what was bought, what it
// cost, the new balance of that currency and the stones of that level now
// owned. Replay is true when the idempotency key had already bought it (no
// new charge).
type BuyStonesResult struct {
	StoneLevel int    `json:"stoneLevel"`
	Quantity   int    `json:"quantity"`
	Currency   string `json:"currency"`
	Amount     int    `json:"amount"`
	Balance    int    `json:"balance"`
	Owned      int    `json:"owned"`
	Replay     bool   `json:"replay"`
}

// stonePurchase is a committed stone buy recorded under its idempotency key.
type stonePurchase struct {
	StoneLevel int
	Quantity   int
	Currency   string
	Amount     int
}

// BuyEquipmentResult is the POST /shop/equipment/buy data.
type BuyEquipmentResult struct {
	EquipmentID string `json:"equipmentId"`
}

// equipmentPurchase is a committed buy recorded under its idempotency key.
type equipmentPurchase struct {
	ItemID      string
	EquipmentID string
}

// UpgradeResult is the POST /player/equipment/upgrade data. Percent is the
// final percent rolled against (careful bonus included). GemSpent is the
// careful price paid (0 when not careful) and GemBalance the gem balance
// after it, present only when careful.
type UpgradeResult struct {
	Success     bool    `json:"success"`
	NewLevel    int     `json:"newLevel"`
	Percent     float64 `json:"percent"`
	EquipmentID string  `json:"equipmentId"`
	Careful     bool    `json:"careful"`
	GemSpent    int     `json:"gemSpent"`
	GemBalance  *int    `json:"gemBalance,omitempty"`
}

type MergeResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
