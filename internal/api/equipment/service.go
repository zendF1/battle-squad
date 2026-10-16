package equipment

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"

	"github.com/jackc/pgx/v5/pgconn"
)

const maxIdempotencyKeyLen = 64

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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Service) getMergeRatePerItem(ctx context.Context) float64 {
	var val string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT value FROM game_settings WHERE key = 'equipment.merge_rate_per_item'`).Scan(&val)
	if err != nil {
		return 25.0 // default
	}
	rate, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 25.0
	}
	return rate
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
	return s.repo.GetActiveMaterials(ctx)
}

func (s *Service) GetCraftingRecipes(ctx context.Context) ([]CraftingRecipe, error) {
	return s.repo.GetAllCraftingRecipes(ctx)
}

// GetCatalog returns every item (normal and crafted) with isActive, the
// upgrade rates, set bonuses and stones. Inactive items are included so owned
// pieces of retired items keep their name and stats; the shop sells only
// active ones (GET /shop/equipment, or the catalog filtered by isActive).
// Lists are never nil.
func (s *Service) GetCatalog(ctx context.Context) (*Catalog, error) {
	items, err := s.repo.GetAllEquipmentItemConfigs(ctx)
	if err != nil {
		return nil, err
	}
	rates, err := s.repo.GetAllUpgradeRates(ctx)
	if err != nil {
		return nil, err
	}
	bonuses, err := s.repo.GetAllSetBonuses(ctx)
	if err != nil {
		return nil, err
	}
	stones, err := s.repo.GetAllStoneConfigs(ctx)
	if err != nil {
		return nil, err
	}
	c := &Catalog{Items: items, UpgradeRates: rates, SetBonuses: bonuses, Stones: stones}
	if c.Items == nil {
		c.Items = []EquipmentItemConfig{}
	}
	if c.UpgradeRates == nil {
		c.UpgradeRates = []UpgradeRateConfig{}
	}
	if c.SetBonuses == nil {
		c.SetBonuses = []SetBonusConfig{}
	}
	if c.Stones == nil {
		c.Stones = []StoneConfig{}
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// BuyEquipment
// ---------------------------------------------------------------------------

// BuyEquipment debits the item's price and creates the equipment in one
// transaction. With an idempotencyKey a retry returns the first buy's
// equipment without charging again, and the key used for another item is
// ErrIdempotencyKeyReused; a failed attempt records nothing. Without a key
// every call buys again.
func (s *Service) BuyEquipment(ctx context.Context, playerID string, req BuyEquipmentRequest) (string, error) {
	if req.ItemID == "" {
		return "", model.ErrEquipmentItemIDRequired
	}
	if len(req.IdempotencyKey) > maxIdempotencyKeyLen {
		return "", model.ErrIdempotencyKeyTooLong
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Lock the player first so concurrent buys (same key or not) run one at a time
	found, err := s.repo.LockPlayerTx(ctx, tx, playerID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", model.ErrNotFound
	}

	if req.IdempotencyKey != "" {
		existing, err := s.repo.FindEquipmentPurchaseTx(ctx, tx, playerID, req.IdempotencyKey)
		if err != nil {
			return "", err
		}
		if existing != nil {
			if existing.ItemID != req.ItemID {
				return "", model.ErrIdempotencyKeyReused
			}
			return existing.EquipmentID, nil
		}
	}

	cfg, err := s.repo.GetEquipmentItemConfigTx(ctx, tx, req.ItemID)
	if err != nil {
		return "", err
	}
	if cfg == nil || !cfg.IsActive {
		return "", model.ErrEquipmentNotFound
	}
	if cfg.Category != "normal" {
		return "", model.ErrEquipmentNotForSale
	}

	currency, amount := "", 0
	if cfg.PriceCoin > 0 {
		currency, amount = "coin", cfg.PriceCoin
	} else if cfg.PriceGem > 0 {
		currency, amount = "gem", cfg.PriceGem
	}
	if amount > 0 {
		if _, err := s.economyRepo.DebitTx(ctx, tx, playerID, currency, amount, "buy_equipment", req.ItemID, false); err != nil {
			return "", err
		}
	}

	equipmentID, err := s.repo.CreateEquipmentTx(ctx, tx, playerID, cfg.ItemID, cfg.Slot, cfg.Category, cfg.Tier, cfg.GemSlots)
	if err != nil {
		return "", err
	}

	if req.IdempotencyKey != "" {
		if err := s.repo.CreateEquipmentPurchaseTx(ctx, tx, playerID, req.IdempotencyKey, cfg.ItemID, equipmentID, currency, amount); err != nil {
			return "", err
		}
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

	gemCfg, err := s.repo.GetGemConfig(ctx, req.GemType, req.GemLevel)
	if err != nil {
		return err
	}
	if gemCfg == nil {
		return model.ErrNotFound
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
	if !cfg.IsActive {
		return model.ErrBadRequest
	}
	if cfg.PriceGem <= 0 {
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
	if req.CharacterID == "" {
		return model.ErrBadRequest
	}

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

	// Check character restriction and level requirement
	itemCfg, err := s.repo.GetEquipmentItemConfig(ctx, equip.ItemID)
	if err != nil {
		return err
	}
	if itemCfg != nil && !CanEquipOnCharacter(itemCfg.CharacterID, req.CharacterID) {
		return model.ErrEquipmentWrongCharacter
	}
	charLevel, owned, err := s.repo.GetCharacterLevel(ctx, playerID, req.CharacterID)
	if err != nil {
		return err
	}
	if !owned {
		return model.ErrCharacterNotOwned
	}
	if itemCfg != nil && charLevel < itemCfg.RequiredLevel {
		return model.ErrEquipmentLevelRequired
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Serialize equips into this character+slot, then re-check state under lock
	if err := s.repo.LockEquipSlotTx(ctx, tx, playerID, req.CharacterID, equip.Slot); err != nil {
		return err
	}
	equip, err = s.repo.GetPlayerEquipmentForUpdateTx(ctx, tx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if equip.IsEquipped {
		return model.ErrEquipmentAlreadyEquipped
	}

	// Auto-unequip existing item in same slot (same character+slot)
	existing, err := s.repo.GetEquippedInSlotTx(ctx, tx, playerID, req.CharacterID, equip.Slot)
	if err != nil {
		return err
	}
	if existing != nil {
		if _, err := s.repo.UnequipItemTx(ctx, tx, existing.EquipmentID); err != nil {
			return err
		}
	}

	n, err := s.repo.EquipItemTx(ctx, tx, req.EquipmentID, req.CharacterID)
	if err != nil {
		if isUniqueViolation(err) {
			return model.ErrEquipmentSlotOccupied
		}
		return err
	}
	if n != 1 {
		return model.ErrEquipmentModified
	}

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// UnequipItem
// ---------------------------------------------------------------------------

func (s *Service) UnequipItem(ctx context.Context, playerID string, req UnequipRequest) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	equip, err := s.repo.GetPlayerEquipmentForUpdateTx(ctx, tx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if !equip.IsEquipped {
		return model.ErrEquipmentNotEquipped
	}

	n, err := s.repo.UnequipItemTx(ctx, tx, req.EquipmentID)
	if err != nil {
		return err
	}
	if n != 1 {
		return model.ErrEquipmentModified
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

	// Lock the row so concurrent upgrades/dismantles of the same item are
	// serialized; the rate above is only valid if the level is unchanged.
	locked, err := s.repo.GetPlayerEquipmentForUpdateTx(ctx, tx, playerID, req.EquipmentID)
	if err != nil {
		return nil, err
	}
	if locked == nil {
		return nil, model.ErrEquipmentNotOwned
	}
	if locked.UpgradeLevel != equip.UpgradeLevel {
		return nil, model.ErrEquipmentModified
	}

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

// getSafezoneStart and CalculateDismantleRefund are in logic.go

func (s *Service) DismantleEquipment(ctx context.Context, playerID string, req DismantleRequest) error {
	// Refund uses the same power table as UpgradeEquipment. Loaded before the
	// tx so we never hit the pool while holding a tx connection.
	stoneCfgs, err := s.repo.GetAllStoneConfigs(ctx)
	if err != nil {
		return err
	}
	stonePowers := make(map[int]int, len(stoneCfgs))
	for _, c := range stoneCfgs {
		stonePowers[c.StoneLevel] = c.Power
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the row so a concurrent dismantle/upgrade waits, then sees it gone
	equip, err := s.repo.GetPlayerEquipmentForUpdateTx(ctx, tx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if equip.IsEquipped {
		return model.ErrEquipmentEquipped
	}

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

		logs, err := s.repo.GetUpgradeLogForDismantleTx(ctx, tx, req.EquipmentID, safezoneStart)
		if err != nil {
			return err
		}

		var sumPower int
		for _, log := range logs {
			sumPower += log.TotalPower
		}

		// Refund 50% of totalPower as highest-value stones possible.
		// Add in ascending level order so concurrent txs lock stone rows consistently.
		refund := CalculateDismantleRefund(sumPower, stonePowers)
		for _, c := range stoneCfgs {
			qty := refund[c.StoneLevel]
			if qty == 0 {
				continue
			}
			if err := s.repo.AddStonesTx(ctx, tx, playerID, c.StoneLevel, qty); err != nil {
				return err
			}
		}
	}

	deleted, err := s.repo.DeleteEquipmentTx(ctx, tx, req.EquipmentID)
	if err != nil {
		return err
	}
	if deleted != 1 {
		return model.ErrEquipmentNotOwned
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

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock equipment then gem so concurrent sockets of the same gem or into
	// the same slot are serialized, then check state under the locks.
	equip, err = s.repo.GetPlayerEquipmentForUpdateTx(ctx, tx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
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

	// Verify gem ownership (and lock it)
	owned, err := s.repo.LockPlayerGemTx(ctx, tx, playerID, req.GemID)
	if err != nil {
		return err
	}
	if !owned {
		return model.ErrGemNotOwned
	}

	// Check gem not already socketed elsewhere
	socketed, err := s.repo.AnyGemSocketedTx(ctx, tx, []string{req.GemID})
	if err != nil {
		return err
	}
	if socketed {
		return model.ErrEquipmentSlotOccupied
	}

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

	ratePerItem := s.getMergeRatePerItem(ctx)

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.DeductStonesTx(ctx, tx, playerID, req.StoneLevel, req.Count); err != nil {
		return nil, model.ErrInsufficientStones
	}

	// Roll: count * configurable rate per item
	successRate := float64(req.Count) * ratePerItem
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

	ratePerItem := s.getMergeRatePerItem(ctx)

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Lock unsocketed gems of type+level, verify enough available
	toConsume, err := s.repo.LockUnsocketedGemsTx(ctx, tx, playerID, req.GemType, req.GemLevel, req.Count)
	if err != nil {
		return nil, err
	}
	if len(toConsume) < req.Count {
		return nil, model.ErrGemNotOwned
	}

	// Re-check under lock: a concurrent SocketGem may have committed meanwhile
	socketed, err := s.repo.AnyGemSocketedTx(ctx, tx, toConsume)
	if err != nil {
		return nil, err
	}
	if socketed {
		return nil, model.ErrGemNotOwned
	}

	deleted, err := s.repo.DeleteGemsTx(ctx, tx, toConsume)
	if err != nil {
		return nil, err
	}
	if deleted != int64(req.Count) {
		return nil, model.ErrGemNotOwned
	}

	// Roll: count * configurable rate per item
	successRate := float64(req.Count) * ratePerItem
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

// CalculateUpgradeMultiplier is in logic.go
