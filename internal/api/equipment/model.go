package equipment

import "time"

// --- Database models ---

type PlayerEquipment struct {
	EquipmentID  string     `json:"equipmentId"`
	PlayerID     string     `json:"playerId"`
	ItemID       string     `json:"itemId"`
	Slot         string     `json:"slot"`
	Category     string     `json:"category"`
	Tier         *string    `json:"tier,omitempty"`
	UpgradeLevel int        `json:"upgradeLevel"`
	GemSlot1     *string    `json:"gemSlot1,omitempty"`
	GemSlot2     *string    `json:"gemSlot2,omitempty"`
	IsEquipped   bool       `json:"isEquipped"`
	EquippedOn   *string    `json:"equippedOn,omitempty"`
	IsLocked     bool       `json:"isLocked"`
	CreatedAt    time.Time  `json:"createdAt"`
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
}

type StoneInput struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
}

type DismantleRequest struct {
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
}

type BuyStoneRequest struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
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

type UpgradeResult struct {
	Success     bool    `json:"success"`
	NewLevel    int     `json:"newLevel"`
	Percent     float64 `json:"percent"`
	EquipmentID string  `json:"equipmentId"`
}

type MergeResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// EquipmentStats holds computed total stats from all equipped items for a character.
type EquipmentStats struct {
	HP         int     `json:"hp"`
	Damage     int     `json:"damage"`
	Defense    int     `json:"defense"`
	CritPct    float64 `json:"critPct"`
	MoveEnergy int     `json:"moveEnergy"`
}
