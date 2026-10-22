package equipment

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/milestone"
	"battle-squad/internal/api/mission"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxIdempotencyKeyLen = 64

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
	// roll returns the upgrade roll in [0, 1); success is roll×100 < percent.
	// Tests replace it to force an outcome.
	roll func() float64
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{
		repo:        repo,
		economyRepo: economyRepo,
		db:          db,
		roll:        rand.Float64,
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
		return DefaultMergeRatePerItem
	}
	return ParseMergeRatePerItem(val)
}

// getSellRate reads game_settings "equipment.sell_rate" (ParseSellRate).
func (s *Service) getSellRate(ctx context.Context) float64 {
	var val string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT value FROM game_settings WHERE key = 'equipment.sell_rate'`).Scan(&val)
	if err != nil {
		return DefaultSellRate
	}
	return ParseSellRate(val)
}

// getSetting reads one game_settings value; ok is false when it is missing.
func (s *Service) getSetting(ctx context.Context, key string) (string, bool) {
	var val string
	if err := s.db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&val); err != nil {
		return "", false
	}
	return val, true
}

// getWeaponUpgradeCostMultiplier reads
// "equipment.weapon_upgrade_cost_multiplier" (ParseWeaponUpgradeCostMultiplier).
func (s *Service) getWeaponUpgradeCostMultiplier(ctx context.Context) float64 {
	val, _ := s.getSetting(ctx, "equipment.weapon_upgrade_cost_multiplier")
	return ParseWeaponUpgradeCostMultiplier(val)
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
	ms, err := battlestats.LoadMilestones(ctx, s.db.Pool)
	if err != nil {
		return nil, err
	}
	c := &Catalog{Items: items, UpgradeRates: rates, SetBonuses: bonuses, Stones: stones, SellRate: s.getSellRate(ctx),
		MergeRatePerItem: s.getMergeRatePerItem(ctx), UpgradeMilestones: []UpgradeMilestone{
			{Level: 6, Bonus: ms.Bonus6}, {Level: 10, Bonus: ms.Bonus10}, {Level: 14, Bonus: ms.Bonus14}, {Level: 16, Bonus: ms.Bonus16},
		},
		WeaponUpgradeCostMultiplier: s.getWeaponUpgradeCostMultiplier(ctx),
	}
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

	currency, amount := ItemPrice(cfg)
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
	if err := mission.RecordShopPurchaseTx(ctx, tx, playerID, time.Now()); err != nil {
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

// maxStoneBuyQuantity caps one stone buy (the price × quantity product and
// the player_stones INT column stay far from overflowing).
const maxStoneBuyQuantity = 999

// BuyStones debits quantity × the stone's price and adds the stones in one
// transaction. With an idempotencyKey a retry returns the first buy (Replay
// true) without charging again, and the key used for another level or
// quantity is ErrIdempotencyKeyReused; a failed attempt records nothing.
// Without a key every call buys again.
func (s *Service) BuyStones(ctx context.Context, playerID string, req BuyStoneRequest) (*BuyStonesResult, error) {
	if req.Quantity <= 0 || req.Quantity > maxStoneBuyQuantity {
		return nil, model.ErrBadRequest
	}
	if len(req.IdempotencyKey) > maxIdempotencyKeyLen {
		return nil, model.ErrIdempotencyKeyTooLong
	}

	// Read before the tx: the pool is never queried while a tx holds a connection.
	cfg, err := s.repo.GetStoneConfig(ctx, req.StoneLevel)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Lock the player first so concurrent buys (same key or not) run one at a time
	coin, gem, found, err := s.repo.LockWalletTx(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, model.ErrNotFound
	}
	balanceOf := func(currency string) int {
		if currency == "gem" {
			return gem
		}
		return coin
	}

	if req.IdempotencyKey != "" {
		existing, err := s.repo.FindStonePurchaseTx(ctx, tx, playerID, req.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.StoneLevel != req.StoneLevel || existing.Quantity != req.Quantity {
				return nil, model.ErrIdempotencyKeyReused
			}
			owned, err := s.repo.StoneQuantityTx(ctx, tx, playerID, req.StoneLevel)
			if err != nil {
				return nil, err
			}
			return &BuyStonesResult{StoneLevel: existing.StoneLevel, Quantity: existing.Quantity, Currency: existing.Currency,
				Amount: existing.Amount, Balance: balanceOf(existing.Currency), Owned: owned, Replay: true}, nil
		}
	}

	if cfg == nil {
		return nil, model.ErrNotFound
	}
	if cfg.Source == "merge_only" {
		return nil, model.ErrBadRequest
	}

	currency, price := StonePrice(cfg)
	amount := price * req.Quantity
	balance := balanceOf(currency)
	if amount > 0 {
		balance, err = s.economyRepo.DebitTx(ctx, tx, playerID, currency, amount, "buy_stones", fmt.Sprintf("stone_%d", req.StoneLevel), false)
		if err != nil {
			return nil, err
		}
	}

	if err := s.repo.AddStonesTx(ctx, tx, playerID, req.StoneLevel, req.Quantity); err != nil {
		return nil, err
	}

	if req.IdempotencyKey != "" {
		p := stonePurchase{StoneLevel: req.StoneLevel, Quantity: req.Quantity, Currency: currency, Amount: amount}
		if err := s.repo.CreateStonePurchaseTx(ctx, tx, playerID, req.IdempotencyKey, p); err != nil {
			return nil, err
		}
	}
	if err := mission.RecordShopPurchaseTx(ctx, tx, playerID, time.Now()); err != nil {
		return nil, err
	}

	owned, err := s.repo.StoneQuantityTx(ctx, tx, playerID, req.StoneLevel)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &BuyStonesResult{StoneLevel: req.StoneLevel, Quantity: req.Quantity, Currency: currency, Amount: amount,
		Balance: balance, Owned: owned}, nil
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
	if err := mission.RecordShopPurchaseTx(ctx, tx, playerID, time.Now()); err != nil {
		return err
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
	if err := mission.RecordShopPurchaseTx(ctx, tx, playerID, time.Now()); err != nil {
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

// UpgradeEquipment consumes the stones and rolls one step. The percent is
// CalculateFinalUpgradePercent: weapons need weapon_upgrade_cost_multiplier ×
// upgradeCost power; careful multiplies the capped stone percent by
// (1 + careful_bonus_percent / 100) and costs the step's careful_gem_cost gem,
// paid on success and on failure. A refused
// attempt (EQUIP_MODIFIED, not enough stones or gems, careful unavailable)
// changes nothing. Locks go equipment → profile → stones, like SellEquipment.
func (s *Service) UpgradeEquipment(ctx context.Context, playerID string, req UpgradeRequest) (*UpgradeResult, error) {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return nil, err
	}
	if equip == nil {
		return nil, model.ErrEquipmentNotOwned
	}
	// The client saw another level (e.g. a retry of an attempt that already
	// rolled): refuse before charging anything.
	if req.ExpectedLevel != nil && *req.ExpectedLevel != equip.UpgradeLevel {
		return nil, model.ErrEquipmentModified
	}
	if equip.UpgradeLevel >= MaxUpgradeLevel {
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
	// Only zero quantities (or zero-power stones): nothing would be consumed,
	// and the roll must not fail the piece back to its safe zone for free.
	if totalPower <= 0 {
		return nil, model.ErrEquipmentNoStones
	}

	gemCost := 0
	carefulBonus := 0.0
	if req.Careful {
		if rateConfig.CarefulGemCost <= 0 || rateConfig.CarefulBonusPercent <= 0 {
			return nil, model.ErrCarefulUnavailable
		}
		gemCost = rateConfig.CarefulGemCost
		carefulBonus = rateConfig.CarefulBonusPercent
	}
	costMultiplier := UpgradeCostMultiplier(equip.Slot, s.getWeaponUpgradeCostMultiplier(ctx))
	_, percent := CalculateFinalUpgradePercent(totalPower, rateConfig.UpgradeCost, costMultiplier, rateConfig.MaxPercent, req.Careful, carefulBonus)

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

	var gemBalance *int
	if req.Careful {
		_, gem, found, err := s.repo.LockWalletTx(ctx, tx, playerID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, model.ErrNotFound
		}
		if gem < gemCost {
			return nil, model.ErrInsufficientBalance
		}
		balance, err := s.economyRepo.DebitTx(ctx, tx, playerID, "gem", gemCost, "careful_upgrade", req.EquipmentID, false)
		if err != nil {
			return nil, err
		}
		gemBalance = &balance
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
	success := s.roll()*100 < percent

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

	// Milestone peaks: a later fail reset or sale does not lower them
	if success {
		if err := milestone.RecordUpgradeTx(ctx, tx, playerID, equip.Slot, newLevel); err != nil {
			return nil, err
		}
	}
	// Daily mission: every attempt counts, success or fail
	if err := mission.RecordUpgradeTx(ctx, tx, playerID, time.Now()); err != nil {
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
		Careful:     req.Careful,
		GemSpent:    gemCost,
		GemBalance:  gemBalance,
	}, nil
}

// ---------------------------------------------------------------------------
// DismantleEquipment / SellEquipment
// ---------------------------------------------------------------------------

// getSafezoneStart and CalculateDismantleRefund are in logic.go

// loadStoneConfigs returns config_stones (ascending level) and level → power,
// the table UpgradeEquipment values stones with. Callers load it before their
// tx so the pool is never hit while holding a tx connection.
func (s *Service) loadStoneConfigs(ctx context.Context) ([]StoneConfig, map[int]int, error) {
	stoneCfgs, err := s.repo.GetAllStoneConfigs(ctx)
	if err != nil {
		return nil, nil, err
	}
	stonePowers := make(map[int]int, len(stoneCfgs))
	for _, c := range stoneCfgs {
		stonePowers[c.StoneLevel] = c.Power
	}
	return stoneCfgs, stonePowers, nil
}

// lockRemovableTx locks the player's piece so a concurrent dismantle, sell or
// upgrade waits, then sees it gone. Not owned (or already removed) is
// ErrEquipmentNotOwned; with rejectLocked a piece ever equipped (is_locked,
// so also every worn piece) is ErrEquipmentLocked; equipped is
// ErrEquipmentEquipped.
func (s *Service) lockRemovableTx(ctx context.Context, tx pgx.Tx, playerID, equipmentID string, rejectLocked bool) (*PlayerEquipment, error) {
	equip, err := s.repo.GetPlayerEquipmentForUpdateTx(ctx, tx, playerID, equipmentID)
	if err != nil {
		return nil, err
	}
	if equip == nil {
		return nil, model.ErrEquipmentNotOwned
	}
	if rejectLocked && equip.IsLocked {
		return nil, model.ErrEquipmentLocked
	}
	if equip.IsEquipped {
		return nil, model.ErrEquipmentEquipped
	}
	return equip, nil
}

// removeLockedTx removes a piece locked by lockRemovableTx: unsets its gem
// slots (the gems stay in player_gems), refunds stones for its upgrades and
// deletes it. Returns the refunded stones in ascending level.
func (s *Service) removeLockedTx(ctx context.Context, tx pgx.Tx, playerID string, equip *PlayerEquipment, stoneCfgs []StoneConfig, stonePowers map[int]int) ([]StoneInput, error) {
	if equip.GemSlot1 != nil {
		if err := s.repo.SetGemSlotTx(ctx, tx, equip.EquipmentID, 1, nil); err != nil {
			return nil, err
		}
	}
	if equip.GemSlot2 != nil {
		if err := s.repo.SetGemSlotTx(ctx, tx, equip.EquipmentID, 2, nil); err != nil {
			return nil, err
		}
	}

	refunded := []StoneInput{}
	// If upgrade level > 0, calculate and refund stones
	if equip.UpgradeLevel > 0 {
		safezoneStart := getSafezoneStart(equip.UpgradeLevel)

		logs, err := s.repo.GetUpgradeLogForDismantleTx(ctx, tx, equip.EquipmentID, safezoneStart)
		if err != nil {
			return nil, err
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
				return nil, err
			}
			refunded = append(refunded, StoneInput{StoneLevel: c.StoneLevel, Quantity: qty})
		}
	}

	deleted, err := s.repo.DeleteEquipmentTx(ctx, tx, equip.EquipmentID)
	if err != nil {
		return nil, err
	}
	if deleted != 1 {
		return nil, model.ErrEquipmentNotOwned
	}
	return refunded, nil
}

func (s *Service) DismantleEquipment(ctx context.Context, playerID string, req DismantleRequest) error {
	stoneCfgs, stonePowers, err := s.loadStoneConfigs(ctx)
	if err != nil {
		return err
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Locked (once equipped) pieces may still be dismantled
	equip, err := s.lockRemovableTx(ctx, tx, playerID, req.EquipmentID, false)
	if err != nil {
		return err
	}
	if _, err := s.removeLockedTx(ctx, tx, playerID, equip, stoneCfgs, stonePowers); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// SellEquipment does everything DismantleEquipment does and also pays
// floor(price × sell rate) of the item's buy currency (coin if priceCoin > 0,
// else gem). A piece ever equipped (is_locked) is ErrEquipmentLocked
// (docs/equipment-system-design.md §11). An item without config, with price 0 (crafted) or whose refund
// rounds to 0 is ErrEquipmentNotSellable and nothing changes. The credit runs
// before the stone refund so locks go equipment → profile → stones, the order
// BuyStones (profile → stones) also uses.
func (s *Service) SellEquipment(ctx context.Context, playerID string, req SellRequest) (*SellResult, error) {
	sellRate := s.getSellRate(ctx)
	stoneCfgs, stonePowers, err := s.loadStoneConfigs(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	equip, err := s.lockRemovableTx(ctx, tx, playerID, req.EquipmentID, true)
	if err != nil {
		return nil, err
	}

	cfg, err := s.repo.GetEquipmentItemConfigTx(ctx, tx, equip.ItemID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, model.ErrEquipmentNotSellable
	}
	currency, price := ItemPrice(cfg)
	amount := CalculateSellRefund(price, sellRate)
	if amount <= 0 {
		return nil, model.ErrEquipmentNotSellable
	}

	balance, err := s.economyRepo.CreditTx(ctx, tx, playerID, currency, amount, "sell_equipment", equip.ItemID)
	if err != nil {
		return nil, err
	}

	stones, err := s.removeLockedTx(ctx, tx, playerID, equip, stoneCfgs, stonePowers)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SellResult{Currency: currency, Amount: amount, Balance: balance, Stones: stones}, nil
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
	if req.Count < MergeMinCount {
		return nil, model.ErrMergeMinCount
	}
	if req.Count > MergeMaxCount {
		return nil, model.ErrMergeMaxCount
	}
	if req.StoneLevel >= MaxStoneLevel {
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
