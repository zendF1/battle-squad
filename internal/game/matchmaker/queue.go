package matchmaker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"github.com/redis/go-redis/v9"
)

const (
	queueKey        = "matchmaking:queue:2v2"
	entryKeyPrefix  = "matchmaking:entry:"
	playerKeyPrefix = "matchmaking:player:"
	entryTTL        = 120 * time.Second
)

// Queue wraps Redis operations for the matchmaking queue.
type Queue struct {
	redis *database.RedisClient
}

// NewQueue creates a new Queue backed by the given RedisClient.
func NewQueue(redis *database.RedisClient) *Queue {
	return &Queue{redis: redis}
}

// Enqueue adds the entry to the sorted set (score = rating), stores the full
// entry JSON with TTL, and maps each playerID to the entryID with TTL.
func (q *Queue) Enqueue(ctx context.Context, entry QueueEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("matchmaker queue: marshal entry %s: %w", entry.EntryID, err)
	}

	pipe := q.redis.Client.Pipeline()

	// Sorted set: score = rating, member = entryID
	pipe.ZAdd(ctx, queueKey, redis.Z{
		Score:  float64(entry.Rating),
		Member: entry.EntryID,
	})

	// Entry detail key
	pipe.Set(ctx, entryKeyPrefix+entry.EntryID, data, entryTTL)

	// Player → entryID mapping keys
	for _, pid := range entry.PlayerIDs {
		pipe.Set(ctx, playerKeyPrefix+pid, entry.EntryID, entryTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("matchmaker queue: enqueue entry %s: %w", entry.EntryID, err)
	}

	observability.Log.Debug().
		Str("entryID", entry.EntryID).
		Int("rating", entry.Rating).
		Int("players", len(entry.PlayerIDs)).
		Msg("matchmaker queue: enqueued entry")

	return nil
}

// Dequeue removes the entry from the sorted set, deletes the detail key, and
// deletes each player mapping key.
func (q *Queue) Dequeue(ctx context.Context, entryID string, playerIDs []string) error {
	pipe := q.redis.Client.Pipeline()

	pipe.ZRem(ctx, queueKey, entryID)
	pipe.Del(ctx, entryKeyPrefix+entryID)

	for _, pid := range playerIDs {
		pipe.Del(ctx, playerKeyPrefix+pid)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("matchmaker queue: dequeue entry %s: %w", entryID, err)
	}

	observability.Log.Debug().
		Str("entryID", entryID).
		Msg("matchmaker queue: dequeued entry")

	return nil
}

// IsPlayerInQueue checks whether playerID has an active mapping in Redis.
// Returns (true, entryID) when found, (false, "") otherwise.
func (q *Queue) IsPlayerInQueue(ctx context.Context, playerID string) (bool, string) {
	val, err := q.redis.Client.Get(ctx, playerKeyPrefix+playerID).Result()
	if err != nil {
		// redis.Nil means key does not exist — not an error worth logging.
		if err != redis.Nil {
			observability.Log.Warn().
				Err(err).
				Str("playerID", playerID).
				Msg("matchmaker queue: IsPlayerInQueue redis error")
		}
		return false, ""
	}
	return true, val
}

// GetAllEntries returns all QueueEntry values from the sorted set ordered by
// rating (ascending). If an entry's detail key has expired (orphan), it is
// removed from the sorted set and skipped.
func (q *Queue) GetAllEntries(ctx context.Context) ([]QueueEntry, error) {
	members, err := q.redis.Client.ZRangeWithScores(ctx, queueKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("matchmaker queue: ZRangeWithScores: %w", err)
	}

	entries := make([]QueueEntry, 0, len(members))

	for _, m := range members {
		entryID, ok := m.Member.(string)
		if !ok {
			continue
		}

		raw, err := q.redis.Client.Get(ctx, entryKeyPrefix+entryID).Result()
		if err != nil {
			if err == redis.Nil {
				// Orphan: entry expired from Redis but still in the sorted set.
				observability.Log.Warn().
					Str("entryID", entryID).
					Msg("matchmaker queue: orphan entry detected, removing from sorted set")
				if remErr := q.redis.Client.ZRem(ctx, queueKey, entryID).Err(); remErr != nil {
					observability.Log.Warn().
						Err(remErr).
						Str("entryID", entryID).
						Msg("matchmaker queue: failed to remove orphan from sorted set")
				}
			} else {
				observability.Log.Warn().
					Err(err).
					Str("entryID", entryID).
					Msg("matchmaker queue: failed to fetch entry detail")
			}
			continue
		}

		var entry QueueEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			observability.Log.Warn().
				Err(err).
				Str("entryID", entryID).
				Msg("matchmaker queue: failed to unmarshal entry detail, skipping")
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// CancelByPlayer looks up the entry associated with playerID, loads the full
// entry, calls Dequeue for all players in that entry, and returns the entry.
// Returns nil, nil when the player is not in the queue.
func (q *Queue) CancelByPlayer(ctx context.Context, playerID string) (*QueueEntry, error) {
	inQueue, entryID := q.IsPlayerInQueue(ctx, playerID)
	if !inQueue {
		return nil, nil
	}

	raw, err := q.redis.Client.Get(ctx, entryKeyPrefix+entryID).Result()
	if err != nil {
		if err == redis.Nil {
			// Entry already expired; clean up the stale player key and return nil.
			q.redis.Client.Del(ctx, playerKeyPrefix+playerID) //nolint:errcheck
			return nil, nil
		}
		return nil, fmt.Errorf("matchmaker queue: CancelByPlayer fetch entry %s: %w", entryID, err)
	}

	var entry QueueEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return nil, fmt.Errorf("matchmaker queue: CancelByPlayer unmarshal entry %s: %w", entryID, err)
	}

	if err := q.Dequeue(ctx, entry.EntryID, entry.PlayerIDs); err != nil {
		return nil, fmt.Errorf("matchmaker queue: CancelByPlayer dequeue entry %s: %w", entryID, err)
	}

	observability.Log.Info().
		Str("playerID", playerID).
		Str("entryID", entryID).
		Msg("matchmaker queue: cancelled entry by player request")

	return &entry, nil
}
