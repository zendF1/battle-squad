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
	ErrBadRequest     = AppError{Code: "BAD_REQUEST", Message: "Invalid parameters", Status: http.StatusBadRequest}
	ErrNotFound       = AppError{Code: "NOT_FOUND", Message: "Resource not found", Status: http.StatusNotFound}
	
	ErrBanned         = AppError{Code: "AUTH_ACCOUNT_BANNED", Message: "Your account has been banned", Status: http.StatusForbidden}
	ErrForceUpdate    = AppError{Code: "APP_FORCE_UPDATE", Message: "A newer version of the app is required to play", Status: http.StatusUpgradeRequired}
	
	ErrInsufficientBalance = AppError{Code: "SHOP_INSUFFICIENT_BALANCE", Message: "Insufficient balance for this purchase", Status: http.StatusBadRequest}
	ErrItemOutOfStock      = AppError{Code: "SHOP_ITEM_OUT_OF_STOCK", Message: "Item offer is out of stock", Status: http.StatusConflict}
	
	ErrMatchNotYourTurn    = AppError{Code: "MATCH_NOT_YOUR_TURN", Message: "It is not your turn to play", Status: http.StatusForbidden}
	ErrMatchAlreadyShot    = AppError{Code: "MATCH_ALREADY_SHOT", Message: "You have already fired a shot during this turn", Status: http.StatusConflict}
)
