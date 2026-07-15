package model

import (
	"encoding/json"
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

type ErrorResponse struct {
	Error struct {
		Code          string                 `json:"code"`
		Message       string                 `json:"message"`
		Details       map[string]interface{} `json:"details,omitempty"`
		CorrelationID string                 `json:"correlationId"`
	} `json:"error"`
}

func WriteError(w http.ResponseWriter, r *http.Request, appErr AppError) {
	corrID, _ := r.Context().Value(observability.CorrelationIDKey).(string)
	
	resp := ErrorResponse{}
	resp.Error.Code = appErr.Code
	resp.Error.Message = appErr.Message
	resp.Error.Details = appErr.Details
	resp.Error.CorrelationID = corrID

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.Status)
	json.NewEncoder(w).Encode(resp)
}

// Error codes registry
var (
	ErrInternalServer = AppError{Code: "INTERNAL_SERVER_ERROR", Message: "Something went wrong on our end", Status: http.StatusInternalServerError}
	ErrUnauthorized   = AppError{Code: "AUTH_UNAUTHORIZED", Message: "Unauthorized access", Status: http.StatusUnauthorized}
	ErrForbidden      = AppError{Code: "AUTH_FORBIDDEN", Message: "Access denied", Status: http.StatusForbidden}
	ErrAdminRequired  = AppError{Code: "AUTH_ADMIN_REQUIRED", Message: "Admin role required", Status: http.StatusForbidden}
	ErrBadRequest     = AppError{Code: "BAD_REQUEST", Message: "Invalid parameters", Status: http.StatusBadRequest}
	ErrNotFound       = AppError{Code: "NOT_FOUND", Message: "Resource not found", Status: http.StatusNotFound}
	
	ErrBanned         = AppError{Code: "AUTH_ACCOUNT_BANNED", Message: "Your account has been banned", Status: http.StatusForbidden}
	ErrForceUpdate    = AppError{Code: "APP_FORCE_UPDATE", Message: "A newer version of the app is required to play", Status: http.StatusUpgradeRequired}
	
	ErrInsufficientBalance = AppError{Code: "SHOP_INSUFFICIENT_BALANCE", Message: "Insufficient balance for this purchase", Status: http.StatusBadRequest}
	ErrItemOutOfStock      = AppError{Code: "SHOP_ITEM_OUT_OF_STOCK", Message: "Item offer is out of stock", Status: http.StatusConflict}
	
	ErrMatchNotYourTurn    = AppError{Code: "MATCH_NOT_YOUR_TURN", Message: "It is not your turn to play", Status: http.StatusForbidden}
	ErrMatchAlreadyShot    = AppError{Code: "MATCH_ALREADY_SHOT", Message: "You have already fired a shot during this turn", Status: http.StatusConflict}

	ErrEquipmentNotFound        = AppError{Code: "EQUIP_NOT_FOUND", Message: "Equipment not found", Status: http.StatusNotFound}
	ErrEquipmentNotOwned        = AppError{Code: "EQUIP_NOT_OWNED", Message: "You do not own this equipment", Status: http.StatusForbidden}
	ErrEquipmentAlreadyEquipped = AppError{Code: "EQUIP_ALREADY_EQUIPPED", Message: "Equipment is already equipped", Status: http.StatusConflict}
	ErrEquipmentSlotOccupied    = AppError{Code: "EQUIP_SLOT_OCCUPIED", Message: "Equipment slot is already occupied", Status: http.StatusConflict}
	ErrEquipmentLocked          = AppError{Code: "EQUIP_LOCKED", Message: "Equipment is locked and cannot be sold", Status: http.StatusConflict}
	ErrEquipmentLevelRequired   = AppError{Code: "EQUIP_LEVEL_REQUIRED", Message: "Character level too low for this equipment", Status: http.StatusBadRequest}
	ErrEquipmentMaxUpgrade      = AppError{Code: "EQUIP_MAX_UPGRADE", Message: "Equipment is already at max upgrade level", Status: http.StatusBadRequest}
	ErrEquipmentNoStones        = AppError{Code: "EQUIP_NO_STONES", Message: "No upgrade stones provided", Status: http.StatusBadRequest}
	ErrInsufficientStones       = AppError{Code: "EQUIP_INSUFFICIENT_STONES", Message: "Not enough upgrade stones", Status: http.StatusBadRequest}
	ErrInsufficientMaterials    = AppError{Code: "EQUIP_INSUFFICIENT_MATERIALS", Message: "Not enough crafting materials", Status: http.StatusBadRequest}
	ErrGemSlotInvalid           = AppError{Code: "EQUIP_GEM_SLOT_INVALID", Message: "Invalid gem slot index", Status: http.StatusBadRequest}
	ErrGemNotOwned              = AppError{Code: "EQUIP_GEM_NOT_OWNED", Message: "You do not own this gem", Status: http.StatusForbidden}
	ErrMergeMinCount            = AppError{Code: "MERGE_MIN_COUNT", Message: "Minimum 2 items required for merging", Status: http.StatusBadRequest}
	ErrMergeMaxCount            = AppError{Code: "MERGE_MAX_COUNT", Message: "Maximum 4 items for merging", Status: http.StatusBadRequest}
	ErrMergeMaxLevel            = AppError{Code: "MERGE_MAX_LEVEL", Message: "Already at maximum level, cannot merge further", Status: http.StatusBadRequest}
	ErrRecipeNotFound           = AppError{Code: "CRAFT_RECIPE_NOT_FOUND", Message: "Crafting recipe not found", Status: http.StatusNotFound}
)
