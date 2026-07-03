package match

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

type eventLogEntry struct {
	eventType string
	playerID  string
	data      json.RawMessage
}

type EventLogger struct {
	matchID string
	db      *database.PostgresDB
	events  chan eventLogEntry
	seq     int64
}

func NewEventLogger(matchID string, db *database.PostgresDB) *EventLogger {
	return &EventLogger{
		matchID: matchID,
		db:      db,
		events:  make(chan eventLogEntry, 256),
	}
}

// Start launches a goroutine that reads from the channel and inserts events into the DB.
// It runs until the channel is drained after ctx is cancelled.
func (el *EventLogger) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case entry, ok := <-el.events:
				if !ok {
					return
				}
				el.insert(entry)
			case <-ctx.Done():
				// Drain remaining events before exiting
				for {
					select {
					case entry := <-el.events:
						el.insert(entry)
					default:
						return
					}
				}
			}
		}
	}()
}

// Log marshals data and sends an event to the async channel.
func (el *EventLogger) Log(eventType, playerID string, data interface{}) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			observability.Log.Warn().Err(err).Str("eventType", eventType).Msg("eventlog: failed to marshal data")
			return
		}
		raw = b
	}

	entry := eventLogEntry{
		eventType: eventType,
		playerID:  playerID,
		data:      raw,
	}

	select {
	case el.events <- entry:
	default:
		observability.Log.Warn().Str("matchId", el.matchID).Str("eventType", eventType).Msg("eventlog: channel full, dropping event")
	}
}

func (el *EventLogger) insert(entry eventLogEntry) {
	seq := atomic.AddInt64(&el.seq, 1)
	ctx := context.Background()

	_, err := el.db.Pool.Exec(ctx,
		`INSERT INTO match_event_logs (match_id, seq, event_type, player_id, data, created_at)
		 VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)`,
		el.matchID,
		seq,
		entry.eventType,
		entry.playerID,
		entry.data,
	)
	if err != nil {
		observability.Log.Error().Err(err).
			Str("matchId", el.matchID).
			Str("eventType", entry.eventType).
			Msg("eventlog: failed to insert event")
	}
}
