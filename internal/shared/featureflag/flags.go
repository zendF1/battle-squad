package featureflag

import (
	"context"

	"battle-squad/internal/shared/database"
)

type Manager struct {
	redis *database.RedisClient
}

func NewManager(redis *database.RedisClient) *Manager {
	return &Manager{redis: redis}
}

func (m *Manager) IsEnabled(ctx context.Context, flagName string) bool {
	if m.redis == nil {
		return true // Fallback to true if redis is nil
	}

	// Fetch from Redis
	val, err := m.redis.Client.Get(ctx, "ff:"+flagName).Result()
	if err != nil {
		// If key not found or redis is down, fallback to default behaviors
		return getDefaultValue(flagName)
	}

	return val == "true"
}

func (m *Manager) Set(ctx context.Context, flagName string, enabled bool) error {
	val := "false"
	if enabled {
		val = "true"
	}
	return m.redis.Client.Set(ctx, "ff:"+flagName, val, 0).Err()
}

func getDefaultValue(flagName string) bool {
	// Define default values if not explicitly set in Redis
	switch flagName {
	case "shop_enabled":
		return true
	case "iap_ios_enabled":
		return true
	case "iap_android_enabled":
		return true
	case "ranked_mode":
		return true
	default:
		return true
	}
}
