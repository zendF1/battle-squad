package observability

import (
	"encoding/json"
	"net/http"
	"time"

	"battle-squad/internal/shared/database"
)

type HealthHandler struct {
	db    *database.PostgresDB
	redis *database.RedisClient
}

func NewHealthHandler(db *database.PostgresDB, redis *database.RedisClient) *HealthHandler {
	return &HealthHandler{db: db, redis: redis}
}

func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	// Liveness: always OK if process is running
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *HealthHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	// Readiness: check database and redis connections
	dbErr := h.db.Ping(r.Context())
	redisErr := h.redis.Ping(r.Context())

	if dbErr != nil || redisErr != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

func (h *HealthHandler) Livez(w http.ResponseWriter, r *http.Request) {
	// Detailed health checks
	start := time.Now()
	dbStatus := "ok"
	dbErr := h.db.Ping(r.Context())
	if dbErr != nil {
		dbStatus = dbErr.Error()
	}

	redisStatus := "ok"
	redisErr := h.redis.Ping(r.Context())
	if redisErr != nil {
		redisStatus = redisErr.Error()
	}

	status := "ok"
	if dbErr != nil || redisErr != nil {
		status = "degraded"
	}

	resp := map[string]interface{}{
		"status": status,
		"checks": map[string]interface{}{
			"postgresql": map[string]interface{}{
				"status": dbStatus,
			},
			"redis": map[string]interface{}{
				"status": redisStatus,
			},
		},
		"latency_ms": time.Since(start).Milliseconds(),
	}

	w.Header().Set("Content-Type", "application/json")
	if status == "degraded" {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(resp)
}
