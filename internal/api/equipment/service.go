package equipment

import (
	"context"
	"fmt"
	"math/rand"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{
		repo:        repo,
		economyRepo: economyRepo,
		db:          db,
	}
}

// ---------------------------------------------------------------------------
// Inventory queries (pass-through)
// ---------------------------------------------------------------------------

func (s *Service) GetPlayerEquipment(ctx context.Context, playerID string) ([]PlayerEquipment, error) {
	return s.repo.GetPlayerEquipment(ctx, playerID)
}

func (s *Service) GetPlayerStones(ctx context.Context, playerID string) ([]PlayerStone, error) {
	return s.repo.GetPlayerStones(ctx, playerID)
}

func (s *Service) GetPlayerGems(ctx context.Context, playerID string) ([]PlayerGem, error) {
	return s.repo.GetPlayerGems(ctx, playerID)
}

func (s *Service) GetPlayerMaterials(ctx context.Context, playerID string) ([]PlayerMaterial, error) {
	return s.repo.GetPlayerMaterials(ctx, playerID)
}

func (s *Service) GetShopEquipment(ctx context.Context, characterID string) ([]EquipmentItemConfig, error) {
	return s.repo.GetShopEquipmentItems(ctx, characterID)
}

func (s *Service) GetShopStones(ctx context.Context) ([]StoneConfig, error) {
	return s.repo.GetAllStoneConfigs(ctx)
}

func (s *Service) GetShopGems(ctx context.Context) ([]GemConfig, error) {
	return s.repo.GetAllGemConfigs(ctx)
}

func (s *Service) GetShopMaterials(ctx context.Context) ([]MaterialConfig, error) {
	return s.repo.GetAllMaterials(ctx)
}

func (s *Service) GetCraftingRecipes(ctx context.Context) ([]CraftingRecipe, error) {
	return s.repo.GetAllCraftingRecipes(ctx)
}

// ---------------------------------------------------------------------------
// BuyEquipment
// ---------------------------------------------------------------------------

func (s *Service) BuyEquipment(ctx context.Context, playerID string, req BuyEquipmentRequest) (string, error) {
	cfg, err := s.repo.GetEquipmentItemConfig(ctx, req.ItemID)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", model.ErrEquipmentNotFound
	}
	if !cfg.IsActive {
		return "", model.ErrBadRequest
	}
	if cfg.Category != "normal" {
		return "", model.ErrBadRequest
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if cfg.PriceCoin > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "coin", cfg.PriceCoin, "buy_equipment", req.ItemID, false)
		if err != nil {
			return "", err
		}
	} else if cfg.PriceGem > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "gem", cfg.PriceGem, "buy_equipment", req.ItemID, false)
		if err != nil {
			return "", err
		}
	}

	equipmentID, err := s.repo.CreateEquipmentTx(ctx, tx, playerID, cfg.ItemID, cfg.Slot, cfg.Category, cfg.Tier, cfg.GemSlots)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return equipmentID, nil
}

// ---------------------------------------------------------------------------
// BuyStones
// ---------------------------------------------------------------------------

func (s *Service) BuyStones(ctx context.Context, playerID string, req BuyStoneRequest) error {
	if req.Quantity <= 0 {
		return model.ErrBadRequest
	}

	cfg, err := s.repo.GetStoneConfig(ctx, req.StoneLevel)
	if err != nil {
		return err
	}
	if cfg == nil {
		return model.ErrNotFound
	}
	if cfg.Source == "merge_only" {
		return model.ErrBadRequest
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	totalCoin := cfg.PriceCoin * req.Quantity
	totalGem := cfg.PriceGem * req.Quantity

	if totalCoin > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "coin", totalCoin, "buy_stones", fmt.Sprintf("stone_%d", req.StoneLevel), false)
		if err != nil {
			return err
		}
	} else if totalGem > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "gem", totalGem, "buy_stones", fmt.Sprintf("stone_%d", req.StoneLevel), false)
		if err != nil {
			return err
		}
	}

	if err := s.repo.AddStonesTx(ctx, tx, playerID, req.StoneLevel, req.Quantity); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// BuyGems
// ---------------------------------------------------------------------------

func (s *Service) BuyGems(ctx context.Context, playerID string, req BuyGemRequest) error {
	if req.Quantity <= 0 {
		return model.ErrBadRequest
	}
	if req.GemLevel < 1 || req.GemLevel > 6 {
		return model.ErrBadRequest
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Price: level 1-3 = coin (level*200), level 4-6 = gem ((level-3)*30)
	var currency string
	var priceEach int
	if req.GemLevel <= 3 {
		currency = "coin"
		priceEach = req.GemLevel * 200
	} else {
		currency = "gem"
		priceEach = (req.GemLevel - 3) * 30
	}

	totalPrice := priceEach * req.Quantity
	refID := fmt.Sprintf("gem_%s_%d", req.GemType, req.GemLevel)

	_, err = s.economyRepo.DebitTx(ctx, tx, playerID, currency, totalPrice, "buy_gems", refID, false)
	if err != nil {
		return err
	}

	// Create individual gem records
	for i := 0; i < req.Quantity; i++ {
		_, err = s.repo.CreateGemTx(ctx, tx, playerID, req.GemType, req.GemLevel)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// BuyMaterials
// ---------------------------------------------------------------------------

func (s *Service) BuyMaterials(ctx context.Context, playerID string, req BuyMaterialRequest) error {
	if req.Quantity <= 0 {
		return model.ErrBadRequest
	}

	cfg, err := s.repo.GetMaterial(ctx, req.MaterialID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return model.ErrNotFound
	}
	if cfg.Source != "gem_shop" {
		return model.ErrBadRequest
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	totalGem := cfg.PriceGem * req.Quantity
	_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "gem", totalGem, "buy_materials", req.MaterialID, false)
	if err != nil {
		return err
	}

	if err := s.repo.AddMaterialsTx(ctx, tx, playerID, req.MaterialID, req.Quantity); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// EquipItem
// ---------------------------------------------------------------------------

func (s *Service) EquipItem(ctx context.Context, playerID string, req EquipRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if equip.IsEquipped {
		return model.ErrEquipmentAlreadyEquipped
	}

	// Check character level requirement
	itemCfg, err := s.repo.GetEquipmentItemConfig(ctx, equip.ItemID)
	if err != nil {
		return err
	}
	if itemCfg != nil && itemCfg.RequiredLevel > 0 {
		charLevel, err := s.repo.GetCharacterLevel(ctx, playerID, req.CharacterID)
		if err != nil {
			return err
		}
		if charLevel < itemCfg.RequiredLevel {
			return model.ErrEquipmentLevelRequired
		}
	}

	// Auto-unequip existing item in same slot (same character+slot)
	existing, err := s.repo.GetEquippedInSlot(ctx, playerID, req.CharacterID, equip.Slot)
	if err != nil {
		return err
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if existing != nil {
		if err := s.repo.UnequipItemTx(ctx, tx, existing.EquipmentID); err != nil {
			return err
		}
	}

	if err := s.repo.EquipItemTx(ctx, tx, req.EquipmentID, req.CharacterID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// UnequipItem
// ---------------------------------------------------------------------------

func (s *Service) UnequipItem(ctx context.Context, playerID string, req UnequipRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if !equip.IsEquipped {
		return model.ErrBadRequest
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.UnequipItemTx(ctx, tx, req.EquipmentID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// UpgradeEquipment
// ---------------------------------------------------------------------------

func (s *Service) UpgradeEquipment(ctx context.Context, playerID string, req UpgradeRequest) (*UpgradeResult, error) {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return nil, err
	}
	if equip == nil {
		return nil, model.ErrEquipmentNotOwned
	}
	if equip.UpgradeLevel >= 16 {
		return nil, model.ErrEquipmentMaxUpgrade
	}
	if len(req.Stones) == 0 {
		return nil, model.ErrEquipmentNoStones
	}

	rateConfig, err := s.repo.GetUpgradeRate(ctx, equip.UpgradeLevel)
	if err != nil {
		return nil, err
	}
	if rateConfig == nil {
		return nil, model.ErrEquipmentMaxUpgrade
	}

	// Calculate total power from stones
	var totalPower int
	for _, si := range req.Stones {
		if si.Quantity <= 0 {
			continue
		}
		stoneCfg, err := s.repo.GetStoneConfig(ctx, si.StoneLevel)
		if err != nil {
			return nil, err
		}
		if stoneCfg == nil {
			return nil, model.ErrNotFound
		}
		totalPower += stoneCfg.Power * si.Quantity
	}

	percent := float64(totalPower) * 100.0 / float64(rateConfig.UpgradeCost)
	if percent > rateConfig.MaxPercent {
		percent = rateConfig.MaxPercent
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Deduct all stones
	for _, si := range req.Stones {
		if si.Quantity <= 0 {
			continue
		}
		if err := s.repo.DeductStonesTx(ctx, tx, playerID, si.StoneLevel, si.Quantity); err != nil {
			return nil, model.ErrInsufficientStones
		}
	}

	// Roll for success
	success := rand.Float64()*100 < percent

	var newLevel int
	if success {
		newLevel = equip.UpgradeLevel + 1
	} else {
		newLevel = rateConfig.FailResetTo
	}

	if err := s.repo.UpdateUpgradeLevelTx(ctx, tx, req.EquipmentID, newLevel); err != nil {
		return nil, err
	}

	if err := s.repo.InsertUpgradeLogTx(ctx, tx, req.EquipmentID, equip.UpgradeLevel, newLevel, req.Stones, totalPower, success); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &UpgradeResult{
		Success:     success,
		NewLevel:    newLevel,
		Percent:     percent,
		EquipmentID: req.EquipmentID,
	}, nil
}

// ---------------------------------------------------------------------------
// DismantleEquipment
// ---------------------------------------------------------------------------

// stonePowers and getSafezoneStart are in logic.go

func (s *Service) DismantleEquipment(ctx context.Context, playerID string, req DismantleRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Clear gem slots (gems remain in player_gems inventory, just unset the slot references)
	if equip.GemSlot1 != nil {
		if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, 1, nil); err != nil {
			return err
		}
	}
	if equip.GemSlot2 != nil {
		if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, 2, nil); err != nil {
			return err
		}
	}

	// If upgrade level > 0, calculate and refund stones
	if equip.UpgradeLevel > 0 {
		safezoneStart := getSafezoneStart(equip.UpgradeLevel)

		logs, err := s.repo.GetUpgradeLogForDismantle(ctx, req.EquipmentID, safezoneStart)
		if err != nil {
			return err
		}

		var sumPower int
		for _, log := range logs {
			sumPower += log.TotalPower
		}

		// Refund 50% of totalPower as highest-value stones possible
		refund := sumPower / 2
		// Distribute refund as highest-value stones first (level 12 down to 1)
		for stoneLevel := 12; stoneLevel >= 1 && refund > 0; stoneLevel-- {
			power := StonePowers[stoneLevel]
			qty := refund / power
			if qty > 0 {
				if err := s.repo.AddStonesTx(ctx, tx, playerID, stoneLevel, qty); err != nil {
					return err
				}
				refund -= qty * power
			}
		}
	}

	if err := s.repo.DeleteEquipmentTx(ctx, tx, req.EquipmentID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// SocketGem
// ---------------------------------------------------------------------------

func (s *Service) SocketGem(ctx context.Context, playerID string, req SocketGemRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}

	// Fetch item config to know gem_slots count
	cfg, err := s.repo.GetEquipmentItemConfig(ctx, equip.ItemID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return model.ErrEquipmentNotFound
	}

	// Validate slotIndex (1 or 2, must be <= gemSlots)
	if req.SlotIndex < 1 || req.SlotIndex > 2 {
		return model.ErrGemSlotInvalid
	}
	if req.SlotIndex > cfg.GemSlots {
		return model.ErrGemSlotInvalid
	}

	// Check slot not already occupied
	switch req.SlotIndex {
	case 1:
		if equip.GemSlot1 != nil {
			return model.ErrEquipmentSlotOccupied
		}
	case 2:
		if equip.GemSlot2 != nil {
			return model.ErrEquipmentSlotOccupied
		}
	}

	// Verify gem ownership
	gem, err := s.repo.GetPlayerGemByID(ctx, playerID, req.GemID)
	if err != nil {
		return err
	}
	if gem == nil {
		return model.ErrGemNotOwned
	}

	// Check gem not already socketed elsewhere
	socketed, err := s.repo.IsGemSocketed(ctx, req.GemID)
	if err != nil {
		return err
	}
	if socketed {
		return model.ErrEquipmentSlotOccupied
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	gemID := req.GemID
	if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, req.SlotIndex, &gemID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// UnsocketGem
// ---------------------------------------------------------------------------

func (s *Service) UnsocketGem(ctx context.Context, playerID string, req UnsocketGemRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}

	// Validate slotIndex
	if req.SlotIndex < 1 || req.SlotIndex > 2 {
		return model.ErrGemSlotInvalid
	}

	// Check slot not empty
	switch req.SlotIndex {
	case 1:
		if equip.GemSlot1 == nil {
			return model.ErrBadRequest
		}
	case 2:
		if equip.GemSlot2 == nil {
			return model.ErrBadRequest
		}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, req.SlotIndex, nil); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// MergeStones
// ---------------------------------------------------------------------------

func (s *Service) MergeStones(ctx context.Context, playerID string, req MergeStoneRequest) (*MergeResult, error) {
	if req.Count < 2 {
		return nil, model.ErrMergeMinCount
	}
	if req.Count > 4 {
		return nil, model.ErrMergeMaxCount
	}
	if req.StoneLevel >= 12 {
		return nil, model.ErrMergeMaxLevel
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.DeductStonesTx(ctx, tx, playerID, req.StoneLevel, req.Count); err != nil {
		return nil, model.ErrInsufficientStones
	}

	// Roll: count*25% success rate
	successRate := float64(req.Count) * 25.0
	success := rand.Float64()*100 < successRate

	if success {
		if err := s.repo.AddStonesTx(ctx, tx, playerID, req.StoneLevel+1, 1); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	msg := "Merge failed, stones lost"
	if success {
		msg = fmt.Sprintf("Successfully merged into a level %d stone", req.StoneLevel+1)
	}

	return &MergeResult{Success: success, Message: msg}, nil
}

// ---------------------------------------------------------------------------
// MergeGems
// ---------------------------------------------------------------------------

func (s *Service) MergeGems(ctx context.Context, playerID string, req MergeGemRequest) (*MergeResult, error) {
	if req.Count < 2 {
		return nil, model.ErrMergeMinCount
	}
	if req.Count > 4 {
		return nil, model.ErrMergeMaxCount
	}
	if req.GemLevel >= 10 {
		return nil, model.ErrMergeMaxLevel
	}

	// Get unequipped gems of type+level, verify enough available
	available, err := s.repo.GetPlayerGemsByTypeAndLevel(ctx, playerID, req.GemType, req.GemLevel)
	if err != nil {
		return nil, err
	}
	if len(available) < req.Count {
		return nil, model.ErrGemNotOwned
	}

	// Pick the gems to consume
	toConsume := make([]string, req.Count)
	for i := 0; i < req.Count; i++ {
		toConsume[i] = available[i].GemID
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.DeleteGemsTx(ctx, tx, toConsume); err != nil {
		return nil, err
	}

	// Roll: count*25% success rate
	successRate := float64(req.Count) * 25.0
	success := rand.Float64()*100 < successRate

	if success {
		_, err = s.repo.CreateGemTx(ctx, tx, playerID, req.GemType, req.GemLevel+1)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	msg := "Merge failed, gems lost"
	if success {
		msg = fmt.Sprintf("Successfully merged into a level %d %s gem", req.GemLevel+1, req.GemType)
	}

	return &MergeResult{Success: success, Message: msg}, nil
}

// ---------------------------------------------------------------------------
// CraftEquipment
// ---------------------------------------------------------------------------

func (s *Service) CraftEquipment(ctx context.Context, playerID string, req CraftRequest) (string, error) {
	recipe, err := s.repo.GetCraftingRecipe(ctx, req.RecipeID)
	if err != nil {
		return "", err
	}
	if recipe == nil {
		return "", model.ErrRecipeNotFound
	}
	if !recipe.IsActive {
		return "", model.ErrRecipeNotFound
	}

	// Fetch result item config
	resultCfg, err := s.repo.GetEquipmentItemConfig(ctx, recipe.ResultItemID)
	if err != nil {
		return "", err
	}
	if resultCfg == nil {
		return "", model.ErrEquipmentNotFound
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Deduct all materials
	for _, mat := range recipe.Materials {
		if err := s.repo.DeductMaterialsTx(ctx, tx, playerID, mat.MaterialID, mat.Quantity); err != nil {
			return "", model.ErrInsufficientMaterials
		}
	}

	// Create equipment with crafted category + tier
	category := "crafted"
	equipmentID, err := s.repo.CreateEquipmentTx(ctx, tx, playerID, resultCfg.ItemID, resultCfg.Slot, category, resultCfg.Tier, resultCfg.GemSlots)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	return equipmentID, nil
}

// ---------------------------------------------------------------------------
// GetEquipmentStatsForCharacter
// ---------------------------------------------------------------------------

func (s *Service) GetEquipmentStatsForCharacter(ctx context.Context, playerID, characterID string) (*EquipmentStats, error) {
	return s.repo.GetEquipmentStatsForCharacter(ctx, playerID, characterID)
}

// CalculateUpgradeMultiplier is in logic.go
