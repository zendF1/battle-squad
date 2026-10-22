package matchmaker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"github.com/redis/go-redis/v9"
)

const (
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

// Enqueue adds the entry to its queue's sorted set (score = rating), stores the
// full entry JSON with TTL, and maps each playerID to the entryID with TTL.
func (q *Queue) Enqueue(ctx context.Context, entry QueueEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("matchmaker queue: marshal entry %s: %w", entry.EntryID, err)
	}

	pipe := q.redis.Client.Pipeline()

	// Sorted set: score = rating, member = entryID
	pipe.ZAdd(ctx, queueKey(queueOf(entry)), redis.Z{
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
		Str("queue", queueOf(entry)).
		Int("rating", entry.Rating).
		Int("players", len(entry.PlayerIDs)).
		Msg("matchmaker queue: enqueued entry")

	return nil
}

// Dequeue removes the entry from its queue's sorted set, deletes the detail key,
// and deletes each player mapping key.
func (q *Queue) Dequeue(ctx context.Context, entry QueueEntry) error {
	entryID := entry.EntryID
	pipe := q.redis.Client.Pipeline()

	pipe.ZRem(ctx, queueKey(queueOf(entry)), entryID)
	pipe.Del(ctx, entryKeyPrefix+entryID)

	for _, pid := range entry.PlayerIDs {
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

// GetAllEntries returns all QueueEntry values of the queue's sorted set ordered
// by rating (ascending). If an entry's detail key has expired (orphan), it is
// removed from the sorted set and skipped.
func (q *Queue) GetAllEntries(ctx context.Context, queue string) ([]QueueEntry, error) {
	key := queueKey(queue)
	members, err := q.redis.Client.ZRangeWithScores(ctx, key, 0, -1).Result()
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
				if remErr := q.redis.Client.ZRem(ctx, key, entryID).Err(); remErr != nil {
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

		entry.Queue = queue
		entries = append(entries, entry)
	}

	return entries, nil
}

// MigrateLegacy moves the entries of the sorted set the ranked queue used before
// there were several queues (legacyQueueKey) into the ranked queue, so a node
// still running the old code during a deploy keeps queueing ranked players.
func (q *Queue) MigrateLegacy(ctx context.Context) error {
	members, err := q.redis.Client.ZRangeWithScores(ctx, legacyQueueKey, 0, -1).Result()
	if err != nil || len(members) == 0 {
		return err
	}
	pipe := q.redis.Client.TxPipeline()
	pipe.ZAdd(ctx, queueKey(RankedQueue), members...)
	for _, m := range members {
		pipe.ZRem(ctx, legacyQueueKey, m.Member)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// ErrAlreadyMatched: the entry was already taken by the matchmaker (its match is
// being created), so it can no longer be cancelled.
var ErrAlreadyMatched = errors.New("matchmaker queue: entry already matched")

// Claim takes the entries out of the queue for one match. It removes each from
// the sorted set first: an entry that is no longer there was cancelled (or
// claimed) meanwhile, and then the entries already taken are put back and
// Claim returns false. Claimed entries lose their detail and player keys.
func (q *Queue) Claim(ctx context.Context, entries []QueueEntry) (bool, error) {
	var taken []QueueEntry
	for _, e := range entries {
		n, err := q.redis.Client.ZRem(ctx, queueKey(queueOf(e)), e.EntryID).Result()
		if err != nil || n == 0 {
			for _, t := range taken {
				q.redis.Client.ZAdd(ctx, queueKey(queueOf(t)), redis.Z{Score: float64(t.Rating), Member: t.EntryID}) //nolint:errcheck
			}
			if err != nil {
				return false, fmt.Errorf("matchmaker queue: claim entry %s: %w", e.EntryID, err)
			}
			return false, nil
		}
		taken = append(taken, e)
	}
	for _, e := range taken {
		if err := q.Dequeue(ctx, e); err != nil {
			return true, err
		}
	}
	return true, nil
}

// CancelByPlayer looks up the entry associated with playerID, removes it from
// the queue and returns it. Returns nil, nil when the player is not in the
// queue, and ErrAlreadyMatched when the matchmaker already claimed the entry.
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

	removed, err := q.redis.Client.ZRem(ctx, queueKey(queueOf(entry)), entry.EntryID).Result()
	if err != nil {
		return nil, fmt.Errorf("matchmaker queue: CancelByPlayer remove entry %s: %w", entryID, err)
	}
	if removed == 0 {
		return nil, ErrAlreadyMatched
	}
	if err := q.Dequeue(ctx, entry); err != nil {
		return nil, fmt.Errorf("matchmaker queue: CancelByPlayer dequeue entry %s: %w", entryID, err)
	}

	observability.Log.Info().
		Str("playerID", playerID).
		Str("entryID", entryID).
		Msg("matchmaker queue: cancelled entry by player request")

	return &entry, nil
}
