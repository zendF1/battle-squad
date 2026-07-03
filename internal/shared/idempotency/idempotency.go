package idempotency

import (
	"context"
	"time"

	"battle-squad/internal/shared/database"
)

type Manager struct {
	redis *database.RedisClient
}

func NewManager(redis *database.RedisClient) *Manager {
	return &Manager{redis: redis}
}

// CheckAndSet returns true if the key already exists (duplicate),
// and false if it was successfully set (unique).
func (m *Manager) CheckAndSet(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if m.redis == nil {
		return false, nil // Bypass if redis is nil
	}

	redisKey := "idempotency:" + key
	// SETNX key value EX ttl
	isNew, err := m.redis.Client.SetNX(ctx, redisKey, "processing", ttl).Result()
	if err != nil {
		return false, err
	}

	return !isNew, nil
}

func (m *Manager) Complete(ctx context.Context, key string, ttl time.Duration) error {
	if m.redis == nil {
		return nil
	}
	redisKey := "idempotency:" + key
	return m.redis.Client.Set(ctx, redisKey, "completed", ttl).Err()
}
