package model

import (
	"errors"
	"net/http"

	"battle-squad/internal/shared/observability"
)

type AppError struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
	Status  int                    `json:"-"`
}

func (e AppError) Error() string {
	return e.Message
}

type ErrorBody struct {
	Code          string                 `json:"code"`
	Message       string                 `json:"message"`
	Details       map[string]interface{} `json:"details,omitempty"`
	CorrelationID string                 `json:"correlationId"`
}

// ErrorResponse is the error payload of WebSocket error events.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// WriteError writes appErr as a {"success":false,"error":{...}} envelope.
func WriteError(w http.ResponseWriter, r *http.Request, appErr AppError) {
	corrID, _ := r.Context().Value(observability.CorrelationIDKey).(string)
	writeEnvelope(w, appErr.Status, Envelope{
		Success: false,
		Error: &ErrorBody{
			Code:          appErr.Code,
			Message:       appErr.Message,
			Details:       appErr.Details,
			CorrelationID: corrID,
		},
	})
}

// WriteServiceError writes err as-is when it is an AppError; anything else is
// unexpected, so it is logged and hidden behind ErrInternalServer.
func WriteServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr AppError
	if errors.As(err, &appErr) {
		WriteError(w, r, appErr)
		return
	}
	logger := observability.FromContext(r.Context())
	logger.Error().Err(err).Str("path", r.URL.Path).Msg("unexpected service error")
	WriteError(w, r, ErrInternalServer)
}

// Error codes registry
var (
	ErrInternalServer = AppError{Code: "INTERNAL_SERVER_ERROR", Message: "Something went wrong on our end", Status: http.StatusInternalServerError}
	ErrUnauthorized   = AppError{Code: "AUTH_UNAUTHORIZED", Message: "Unauthorized access", Status: http.StatusUnauthorized}
	ErrForbidden      = AppError{Code: "AUTH_FORBIDDEN", Message: "Access denied", Status: http.StatusForbidden}
	ErrAdminRequired  = AppError{Code: "AUTH_ADMIN_REQUIRED", Message: "Admin role required", Status: http.StatusForbidden}
	ErrBadRequest     = AppError{Code: "BAD_REQUEST", Message: "Invalid parameters", Status: http.StatusBadRequest}
	ErrNotFound       = AppError{Code: "NOT_FOUND", Message: "Resource not found", Status: http.StatusNotFound}

	ErrBanned      = AppError{Code: "AUTH_ACCOUNT_BANNED", Message: "Your account has been banned", Status: http.StatusForbidden}
	ErrForceUpdate = AppError{Code: "APP_FORCE_UPDATE", Message: "A newer version of the app is required to play", Status: http.StatusUpgradeRequired}

	ErrDisplayNameEmpty   = AppError{Code: "PLAYER_UPDATE_FAILED", Message: "display name cannot be empty", Status: http.StatusBadRequest}
	ErrDisplayNameTooLong = AppError{Code: "PLAYER_UPDATE_FAILED", Message: "display name is too long", Status: http.StatusBadRequest}

	ErrInsufficientBalance  = AppError{Code: "SHOP_INSUFFICIENT_BALANCE", Message: "Insufficient balance for this purchase", Status: http.StatusBadRequest}
	ErrItemOutOfStock       = AppError{Code: "SHOP_ITEM_OUT_OF_STOCK", Message: "Item offer is out of stock", Status: http.StatusConflict}
	ErrShopAlreadyOwned     = AppError{Code: "SHOP_ALREADY_OWNED", Message: "You already own this character", Status: http.StatusConflict}
	ErrIdempotencyKeyReused = AppError{Code: "IDEMPOTENCY_KEY_REUSED", Message: "Idempotency key was already used for a different request", Status: http.StatusConflict}

	ErrIAPStoreNotConfigured  = AppError{Code: "IAP_STORE_NOT_CONFIGURED", Message: "Store receipt verification is not available", Status: http.StatusServiceUnavailable}
	ErrIAPProductInactive     = AppError{Code: "IAP_PRODUCT_INACTIVE", Message: "Product is not active or does not exist", Status: http.StatusNotFound}
	ErrIAPReceiptInvalid      = AppError{Code: "IAP_VERIFICATION_FAILED", Message: "The store rejected this receipt", Status: http.StatusBadRequest}
	ErrIAPTransactionConflict = AppError{Code: "IAP_TRANSACTION_CONFLICT", Message: "This receipt was already used for a different purchase", Status: http.StatusConflict}
	ErrMethodNotAllowed       = AppError{Code: "METHOD_NOT_ALLOWED", Message: "Method not allowed", Status: http.StatusMethodNotAllowed}

	ErrMatchNotYourTurn = AppError{Code: "MATCH_NOT_YOUR_TURN", Message: "It is not your turn to play", Status: http.StatusForbidden}
	ErrMatchAlreadyShot = AppError{Code: "MATCH_ALREADY_SHOT", Message: "You have already fired a shot during this turn", Status: http.StatusConflict}
	ErrLoadoutInvalid   = AppError{Code: "LOADOUT_INVALID", Message: "This character or item selection is not allowed", Status: http.StatusBadRequest}
	ErrMatchStartFailed = AppError{Code: "MATCH_START_FAILED", Message: "Failed to start match", Status: http.StatusInternalServerError}

	ErrEquipmentNotFound        = AppError{Code: "EQUIP_NOT_FOUND", Message: "Equipment not found", Status: http.StatusNotFound}
	ErrEquipmentNotOwned        = AppError{Code: "EQUIP_NOT_OWNED", Message: "You do not own this equipment", Status: http.StatusForbidden}
	ErrEquipmentAlreadyEquipped = AppError{Code: "EQUIP_ALREADY_EQUIPPED", Message: "Equipment is already equipped", Status: http.StatusConflict}
	ErrEquipmentSlotOccupied    = AppError{Code: "EQUIP_SLOT_OCCUPIED", Message: "Equipment slot is already occupied", Status: http.StatusConflict}
	ErrEquipmentItemIDRequired  = AppError{Code: "BAD_REQUEST", Message: "itemId is required", Status: http.StatusBadRequest}
	ErrIdempotencyKeyTooLong    = AppError{Code: "BAD_REQUEST", Message: "idempotency key is too long", Status: http.StatusBadRequest}
	ErrEquipmentNotForSale      = AppError{Code: "BAD_REQUEST", Message: "only normal equipment is sold in the shop", Status: http.StatusBadRequest}
	ErrEquipmentNotSellable     = AppError{Code: "EQUIP_NOT_SELLABLE", Message: "This equipment has no sell price", Status: http.StatusBadRequest}
	ErrEquipmentEquipped        = AppError{Code: "EQUIP_EQUIPPED", Message: "Equipment is equipped, unequip it first", Status: http.StatusConflict}
	ErrEquipmentNotEquipped     = AppError{Code: "EQUIP_NOT_EQUIPPED", Message: "Equipment is not equipped", Status: http.StatusConflict}
	ErrEquipmentLocked          = AppError{Code: "EQUIP_LOCKED", Message: "Equipment is locked and cannot be sold", Status: http.StatusConflict}
	ErrEquipmentLevelRequired   = AppError{Code: "EQUIP_LEVEL_REQUIRED", Message: "Character level too low for this equipment", Status: http.StatusBadRequest}
	ErrEquipmentWrongCharacter  = AppError{Code: "EQUIP_WRONG_CHARACTER", Message: "This equipment is exclusive to another character", Status: http.StatusBadRequest}
	ErrEquipmentModified        = AppError{Code: "EQUIP_MODIFIED", Message: "Equipment was changed by another request, please retry", Status: http.StatusConflict}
	ErrCharacterNotOwned        = AppError{Code: "CHARACTER_NOT_FOUND", Message: "Character not found or not owned by this player", Status: http.StatusNotFound}
	ErrEquipmentMaxUpgrade      = AppError{Code: "EQUIP_MAX_UPGRADE", Message: "Equipment is already at max upgrade level", Status: http.StatusBadRequest}
	ErrEquipmentNoStones        = AppError{Code: "EQUIP_NO_STONES", Message: "No upgrade stones provided", Status: http.StatusBadRequest}
	ErrCarefulUnavailable       = AppError{Code: "EQUIP_CAREFUL_UNAVAILABLE", Message: "Careful upgrade is not offered for this level", Status: http.StatusBadRequest}
	ErrInsufficientStones       = AppError{Code: "EQUIP_INSUFFICIENT_STONES", Message: "Not enough upgrade stones", Status: http.StatusBadRequest}
	ErrInsufficientMaterials    = AppError{Code: "EQUIP_INSUFFICIENT_MATERIALS", Message: "Not enough crafting materials", Status: http.StatusBadRequest}
	ErrGemSlotInvalid           = AppError{Code: "EQUIP_GEM_SLOT_INVALID", Message: "Invalid gem slot index", Status: http.StatusBadRequest}
	ErrGemNotOwned              = AppError{Code: "EQUIP_GEM_NOT_OWNED", Message: "You do not own this gem", Status: http.StatusForbidden}
	ErrMergeMinCount            = AppError{Code: "MERGE_MIN_COUNT", Message: "Minimum 2 items required for merging", Status: http.StatusBadRequest}
	ErrMergeMaxCount            = AppError{Code: "MERGE_MAX_COUNT", Message: "Maximum 4 items for merging", Status: http.StatusBadRequest}
	ErrMergeMaxLevel            = AppError{Code: "MERGE_MAX_LEVEL", Message: "Already at maximum level, cannot merge further", Status: http.StatusBadRequest}
	ErrRecipeNotFound           = AppError{Code: "CRAFT_RECIPE_NOT_FOUND", Message: "Crafting recipe not found", Status: http.StatusNotFound}

	ErrMilestoneAlreadyClaimed = AppError{Code: "MILESTONE_ALREADY_CLAIMED", Message: "This milestone reward was already claimed", Status: http.StatusConflict}
	ErrMilestoneNotReached     = AppError{Code: "MILESTONE_NOT_REACHED", Message: "This milestone is not reached yet", Status: http.StatusBadRequest}

	ErrMissionNotFound       = AppError{Code: "MISSION_NOT_FOUND", Message: "Mission not found", Status: http.StatusNotFound}
	ErrMissionNotDone        = AppError{Code: "MISSION_NOT_DONE", Message: "This mission is not completed yet", Status: http.StatusBadRequest}
	ErrMissionAlreadyClaimed = AppError{Code: "MISSION_ALREADY_CLAIMED", Message: "This mission was already claimed", Status: http.StatusConflict}
	ErrChestNotFound         = AppError{Code: "CHEST_NOT_FOUND", Message: "Chest not found", Status: http.StatusNotFound}
	ErrChestLocked           = AppError{Code: "CHEST_LOCKED", Message: "Not enough activity for this chest", Status: http.StatusBadRequest}
	ErrChestAlreadyClaimed   = AppError{Code: "CHEST_ALREADY_CLAIMED", Message: "This chest was already claimed", Status: http.StatusConflict}

	ErrTitleNotOwned = AppError{Code: "TITLE_NOT_OWNED", Message: "You do not own this title", Status: http.StatusForbidden}

	ErrMapNotFound = AppError{Code: "MAP_NOT_FOUND", Message: "Map not found", Status: http.StatusNotFound}

	ErrGameDataVersionNotFound = AppError{Code: "GAMEDATA_VERSION_NOT_FOUND", Message: "No game data was published with this version", Status: http.StatusNotFound}
)
