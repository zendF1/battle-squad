package dev

import (
	"encoding/json"
	"net/http"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	db    *database.PostgresDB
	redis *database.RedisClient
}

func NewHandler(db *database.PostgresDB, redis *database.RedisClient) *Handler {
	return &Handler{db: db, redis: redis}
}

// ClearRooms removes all active rooms from Redis.
func (h *Handler) ClearRooms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	deleted, err := h.redis.Client.Del(ctx, "rooms:active").Result()
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to clear rooms from Redis")
		http.Error(w, "failed to clear rooms", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "rooms cleared",
		"keysDeleted": deleted,
	})
}

// ResetData clears player-related data for testing.
// WARNING: This deletes ALL player data. Only available in development.
func (h *Handler) ResetData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tables := []string{
		"match_event_logs",
		"match_recovery_logs",
		"match_snapshots",
		"match_histories",
		"season_reward_claims",
		"player_ranks",
		"inventory_reservations",
		"inventory_items",
		"player_characters",
		"economy_transactions",
		"payment_transactions",
		"shop_purchases",
		"mission_progress",
		"gift_code_redemptions",
		"player_reports",
		"account_bans",
		"player_profiles",
		"auth_identities",
		"accounts",
	}

	for _, table := range tables {
		_, err := h.db.Pool.Exec(ctx, "DELETE FROM "+table)
		if err != nil {
			observability.Log.Warn().Err(err).Str("table", table).Msg("failed to clear table (may not exist)")
		}
	}

	// Clear Redis
	h.redis.Client.Del(ctx, "rooms:active")
	h.redis.Client.Del(ctx, "leaderboard:current")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "all data cleared",
	})
}
