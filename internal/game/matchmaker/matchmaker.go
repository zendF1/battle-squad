package matchmaker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
	"sync"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

const leaderKey = "matchmaking:leader"

// MatchResult carries the two queue entries that have been matched, the chosen
// map, and whether one side is a bot team.
type MatchResult struct {
	Entry1 QueueEntry
	Entry2 QueueEntry
	MapID  string
	HasBot bool
}

// RoomCreator is the interface implemented by room.Hub that allows the
// matchmaker to spawn a battle room without importing the room package.
type RoomCreator interface {
	CreateBattleFromMatch(ctx context.Context, result MatchResult, botDiffConfig BotDifficultyConfig, eloConfig EloConfig) error
}

// Matchmaker is a background service that periodically scans the queue,
// pairs entries by rating proximity, and creates battle rooms.  Only the
// leader node (determined via a Redis lock) runs the matching logic.
type Matchmaker struct {
	queue       *Queue
	db          *database.PostgresDB
	redis       *database.RedisClient
	nodeID      string
	cfg         MatchmakingConfig
	eloConfig   EloConfig
	botConfig   BotDifficultyConfig
	roomCreator RoomCreator
	mapIDs      []string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
}

// NewMatchmaker creates a Matchmaker, loading all three config blocks from the
// DB.  mapIDs is the list of map identifiers eligible for random selection.
func NewMatchmaker(
	db *database.PostgresDB,
	redis *database.RedisClient,
	nodeID string,
	roomCreator RoomCreator,
	mapIDs []string,
) *Matchmaker {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := LoadMatchmakingConfig(ctx, db)
	eloConfig := LoadEloConfig(ctx, db)
	botConfig := LoadBotDifficultyConfig(ctx, db)

	return &Matchmaker{
		queue:       NewQueue(redis),
		db:          db,
		redis:       redis,
		nodeID:      nodeID,
		cfg:         cfg,
		eloConfig:   eloConfig,
		botConfig:   botConfig,
		roomCreator: roomCreator,
		mapIDs:      mapIDs,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Run starts the matchmaker goroutine.  It should be called with `go`.
// The goroutine runs three independent tickers:
//
//   - tickTicker  — main matching logic (TickInterval seconds)
//   - renewTicker — leader lock renewal (5 s)
//   - cfgTicker   — config reload from DB (30 s)
func (m *Matchmaker) Run() {
	m.mu.RLock()
	tickInterval := time.Duration(m.cfg.TickInterval) * time.Second
	m.mu.RUnlock()

	tickTicker := time.NewTicker(tickInterval)
	renewTicker := time.NewTicker(5 * time.Second)
	cfgTicker := time.NewTicker(30 * time.Second)

	defer tickTicker.Stop()
	defer renewTicker.Stop()
	defer cfgTicker.Stop()

	observability.Log.Info().
		Str("nodeID", m.nodeID).
		Msg("matchmaker: started")

	for {
		select {
		case <-m.ctx.Done():
			observability.Log.Info().
				Str("nodeID", m.nodeID).
				Msg("matchmaker: stopped")
			return

		case <-renewTicker.C:
			m.renewLeader()

		case <-cfgTicker.C:
			m.reloadConfig()

		case <-tickTicker.C:
			if m.tryAcquireLeader() {
				m.tick()
			}
		}
	}
}

// Stop cancels the matchmaker context, causing Run to return.
func (m *Matchmaker) Stop() {
	m.cancel()
}

// ---------------------------------------------------------------------------
// Leader election
// ---------------------------------------------------------------------------

// tryAcquireLeader attempts to obtain or confirm leadership via a Redis
// SET NX with a 10-second TTL.  Returns true when this node is the leader.
func (m *Matchmaker) tryAcquireLeader() bool {
	set, err := m.redis.Client.SetNX(m.ctx, leaderKey, m.nodeID, 10*time.Second).Result()
	if err != nil {
		observability.Log.Warn().Err(err).Msg("matchmaker: tryAcquireLeader SetNX error")
		return false
	}
	if set {
		// We just became the leader.
		return true
	}

	// Key already exists — check whether it's us.
	val, err := m.redis.Client.Get(m.ctx, leaderKey).Result()
	if err != nil {
		observability.Log.Warn().Err(err).Msg("matchmaker: tryAcquireLeader Get error")
		return false
	}
	return val == m.nodeID
}

// renewLeader extends the TTL of the leader key when we are the current
// leader.  This prevents the lock from expiring between ticks.
func (m *Matchmaker) renewLeader() {
	val, err := m.redis.Client.Get(m.ctx, leaderKey).Result()
	if err != nil {
		// Key absent or Redis error — nothing to renew.
		return
	}
	if val != m.nodeID {
		return
	}
	if err := m.redis.Client.Expire(m.ctx, leaderKey, 10*time.Second).Err(); err != nil {
		observability.Log.Warn().Err(err).Msg("matchmaker: renewLeader Expire error")
	}
}

// ---------------------------------------------------------------------------
// Config reload
// ---------------------------------------------------------------------------

// reloadConfig re-fetches all three config blocks from the DB and swaps them
// under the write lock.
func (m *Matchmaker) reloadConfig() {
	cfg := LoadMatchmakingConfig(m.ctx, m.db)
	eloConfig := LoadEloConfig(m.ctx, m.db)
	botConfig := LoadBotDifficultyConfig(m.ctx, m.db)

	m.mu.Lock()
	m.cfg = cfg
	m.eloConfig = eloConfig
	m.botConfig = botConfig
	m.mu.Unlock()

	observability.Log.Debug().Msg("matchmaker: config reloaded")
}

// GetConfig returns a snapshot of the current MatchmakingConfig.
func (m *Matchmaker) GetConfig() MatchmakingConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// ---------------------------------------------------------------------------
// Core matching tick
// ---------------------------------------------------------------------------

// tick is called every TickInterval seconds on the leader node.  It fetches
// all queue entries (sorted by rating ascending), pairs them greedily within
// an expanding rating range, and falls back to bots for entries that have
// waited too long.
func (m *Matchmaker) tick() {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	entries, err := m.queue.GetAllEntries(m.ctx)
	if err != nil {
		observability.Log.Error().Err(err).Msg("matchmaker: tick GetAllEntries error")
		return
	}
	if len(entries) == 0 {
		return
	}

	now := time.Now().Unix()
	matched := make([]bool, len(entries))

	for i := 0; i < len(entries); i++ {
		if matched[i] {
			continue
		}
		e1 := entries[i]
		waitSec := now - e1.QueuedAt

		// Bot fallback: entry has waited longer than MaxWaitTime.
		if waitSec >= int64(cfg.MaxWaitTime) {
			matched[i] = true
			m.matchWithBot(m.ctx, e1)
			continue
		}

		allowedRange := calculateAllowedRange(int(waitSec), cfg)

		for j := i + 1; j < len(entries); j++ {
			if matched[j] {
				continue
			}
			e2 := entries[j]
			diff := e2.Rating - e1.Rating
			if diff < 0 {
				diff = -diff
			}
			if diff <= allowedRange {
				matched[i] = true
				matched[j] = true
				m.matchEntries(m.ctx, e1, e2)
				break
			}
		}
	}
}

// calculateAllowedRange returns the current rating window for an entry that
// has been waiting waitTimeSec seconds.
func calculateAllowedRange(waitTimeSec int, cfg MatchmakingConfig) int {
	expansions := 0
	if cfg.ExpandInterval > 0 {
		expansions = waitTimeSec / cfg.ExpandInterval
	}
	r := cfg.BaseRatingRange + expansions*cfg.ExpandStep
	if r > cfg.MaxRatingRange {
		r = cfg.MaxRatingRange
	}
	return r
}

// matchEntries dequeues both entries and hands the pair to the room creator.
func (m *Matchmaker) matchEntries(ctx context.Context, e1, e2 QueueEntry) {
	if err := m.queue.Dequeue(ctx, e1.EntryID, e1.PlayerIDs); err != nil {
		observability.Log.Error().Err(err).Str("entryID", e1.EntryID).Msg("matchmaker: matchEntries dequeue e1 error")
		return
	}
	if err := m.queue.Dequeue(ctx, e2.EntryID, e2.PlayerIDs); err != nil {
		observability.Log.Error().Err(err).Str("entryID", e2.EntryID).Msg("matchmaker: matchEntries dequeue e2 error")
		return
	}

	m.mu.RLock()
	eloConfig := m.eloConfig
	botConfig := m.botConfig
	m.mu.RUnlock()

	result := MatchResult{
		Entry1: e1,
		Entry2: e2,
		MapID:  m.randomMap(),
		HasBot: false,
	}

	if err := m.roomCreator.CreateBattleFromMatch(ctx, result, botConfig, eloConfig); err != nil {
		observability.Log.Error().
			Err(err).
			Str("entry1", e1.EntryID).
			Str("entry2", e2.EntryID).
			Msg("matchmaker: CreateBattleFromMatch error")
		return
	}

	observability.Log.Info().
		Str("entry1", e1.EntryID).
		Str("entry2", e2.EntryID).
		Int("rating1", e1.Rating).
		Int("rating2", e2.Rating).
		Str("mapID", result.MapID).
		Msg("matchmaker: matched two entries")
}

// matchWithBot dequeues the entry and pairs it against an empty bot team.
func (m *Matchmaker) matchWithBot(ctx context.Context, entry QueueEntry) {
	if err := m.queue.Dequeue(ctx, entry.EntryID, entry.PlayerIDs); err != nil {
		observability.Log.Error().Err(err).Str("entryID", entry.EntryID).Msg("matchmaker: matchWithBot dequeue error")
		return
	}

	m.mu.RLock()
	eloConfig := m.eloConfig
	botConfig := m.botConfig
	m.mu.RUnlock()

	// Bot team is represented by an empty QueueEntry (TeamSize = 0, no PlayerIDs).
	botEntry := QueueEntry{
		PlayerIDs: []string{},
		TeamSize:  0,
	}

	result := MatchResult{
		Entry1: entry,
		Entry2: botEntry,
		MapID:  m.randomMap(),
		HasBot: true,
	}

	if err := m.roomCreator.CreateBattleFromMatch(ctx, result, botConfig, eloConfig); err != nil {
		observability.Log.Error().
			Err(err).
			Str("entry", entry.EntryID).
			Msg("matchmaker: CreateBattleFromMatch (bot) error")
		return
	}

	observability.Log.Info().
		Str("entry", entry.EntryID).
		Int("rating", entry.Rating).
		Str("mapID", result.MapID).
		Msg("matchmaker: matched entry with bot")
}

// ---------------------------------------------------------------------------
// Public queue API
// ---------------------------------------------------------------------------

// EnqueueLobby builds a QueueEntry from the provided lobby details, calculates
// the party rating, and enqueues it.  Returns the created entry on success.
func (m *Matchmaker) EnqueueLobby(
	ctx context.Context,
	lobbyID string,
	playerIDs []string,
	playerRatings map[string]int,
	playerChars map[string]string,
	playerItems map[string][]string,
	playerNames map[string]string,
) (*QueueEntry, error) {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	entryID := generateMatchmakerID()

	entry := QueueEntry{
		EntryID:       entryID,
		LobbyID:       lobbyID,
		PlayerIDs:     playerIDs,
		Rating:        calculatePartyRating(playerRatings, cfg),
		PlayerRatings: playerRatings,
		PlayerChars:   playerChars,
		PlayerItems:   playerItems,
		PlayerNames:   playerNames,
		TeamSize:      len(playerIDs),
		QueuedAt:      time.Now().Unix(),
		NodeID:        m.nodeID,
	}

	if err := m.queue.Enqueue(ctx, entry); err != nil {
		return nil, err
	}

	observability.Log.Info().
		Str("entryID", entryID).
		Str("lobbyID", lobbyID).
		Int("rating", entry.Rating).
		Int("teamSize", entry.TeamSize).
		Msg("matchmaker: lobby enqueued")

	return &entry, nil
}

// CancelQueue removes the queue entry that contains playerID.
// Returns the removed entry (or nil if the player was not queued).
func (m *Matchmaker) CancelQueue(ctx context.Context, playerID string) (*QueueEntry, error) {
	return m.queue.CancelByPlayer(ctx, playerID)
}

// IsPlayerInQueue reports whether playerID currently has an active queue entry.
func (m *Matchmaker) IsPlayerInQueue(ctx context.Context, playerID string) bool {
	inQueue, _ := m.queue.IsPlayerInQueue(ctx, playerID)
	return inQueue
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// calculatePartyRating derives a single rating value from the individual
// player ratings according to the configured strategy:
//
//   - "average"  — arithmetic mean
//   - "weighted" — WeightedRatio * max + (1-WeightedRatio) * average
//   - "max" (default) — highest individual rating
func calculatePartyRating(ratings map[string]int, cfg MatchmakingConfig) int {
	if len(ratings) == 0 {
		return cfg.BaseRatingRange // sensible fallback
	}

	maxRating := 0
	sum := 0
	for _, r := range ratings {
		sum += r
		if r > maxRating {
			maxRating = r
		}
	}
	avg := sum / len(ratings)

	switch cfg.PartyRatingStrategy {
	case "average":
		return avg
	case "weighted":
		return int(cfg.WeightedRatio*float64(maxRating) + (1-cfg.WeightedRatio)*float64(avg))
	default: // "max"
		return maxRating
	}
}

// randomMap picks a random map ID from the configured list.
// Falls back to "default_map" when the list is empty.
func (m *Matchmaker) randomMap() string {
	m.mu.RLock()
	ids := m.mapIDs
	m.mu.RUnlock()

	if len(ids) == 0 {
		return "default_map"
	}
	//nolint:gosec // non-cryptographic randomness is fine for map selection
	return ids[mrand.IntN(len(ids))]
}

// generateMatchmakerID produces a 32-character lowercase hex string using
// 16 cryptographically random bytes — the same pattern used elsewhere in the
// codebase (room.generateID, lobby.generateID).
func generateMatchmakerID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("matchmaker: failed to generate random ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
