package match

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

const (
	eventBatchSize     = 50
	eventFlushInterval = 500 * time.Millisecond
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

func (el *EventLogger) Start(ctx context.Context) {
	go func() {
		buffer := make([]eventLogEntry, 0, eventBatchSize)
		ticker := time.NewTicker(eventFlushInterval)
		defer ticker.Stop()

		flush := func() {
			if len(buffer) == 0 {
				return
			}
			el.insertBatch(buffer)
			buffer = buffer[:0]
		}

		for {
			select {
			case entry, ok := <-el.events:
				if !ok {
					flush()
					return
				}
				buffer = append(buffer, entry)
				if len(buffer) >= eventBatchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-ctx.Done():
				for {
					select {
					case entry := <-el.events:
						buffer = append(buffer, entry)
					default:
						flush()
						return
					}
				}
			}
		}
	}()
}

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

func (el *EventLogger) insertBatch(entries []eventLogEntry) {
	if len(entries) == 0 {
		return
	}

	ctx := context.Background()

	var b strings.Builder
	b.WriteString("INSERT INTO match_event_logs (match_id, seq, event_type, player_id, data, created_at) VALUES ")

	args := make([]interface{}, 0, len(entries)*5)
	for i, entry := range entries {
		seq := atomic.AddInt64(&el.seq, 1)
		if i > 0 {
			b.WriteString(", ")
		}
		base := i * 5
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, CURRENT_TIMESTAMP)", base+1, base+2, base+3, base+4, base+5)
		args = append(args, el.matchID, seq, entry.eventType, entry.playerID, entry.data)
	}

	_, err := el.db.Pool.Exec(ctx, b.String(), args...)
	if err != nil {
		observability.Log.Error().Err(err).
			Str("matchId", el.matchID).
			Int("batchSize", len(entries)).
			Msg("eventlog: failed to insert batch")
	}
}
