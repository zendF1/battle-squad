package matchmaker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	mrand "math/rand/v2"
	"sync"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"
)

const leaderKey = "matchmaking:leader"

// RoomCreator is the interface implemented by room.Hub that allows the
// matchmaker to spawn a battle room without importing the room package.
type RoomCreator interface {
	CreateBattleFromMatch(ctx context.Context, result MatchResult, botDiffConfig BotDifficultyConfig, eloConfig EloConfig, mmConfig MatchmakingConfig) error
}

// Matchmaker is a background service that periodically scans the queues
// (Queues), forms their 2v2 matches (planMatches), and creates battle rooms.  Only the
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
	mapTiers    map[string]string
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
	mapTiers map[string]string,
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
		mapTiers:    mapTiers,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetMaps replaces the maps eligible for random selection (after a game data reload).
func (m *Matchmaker) SetMaps(mapIDs []string, mapTiers map[string]string) {
	m.mu.Lock()
	m.mapIDs, m.mapTiers = mapIDs, mapTiers
	m.mu.Unlock()
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

// tick is called every TickInterval seconds on the leader node. For every queue
// (Queues) it plans the matches of the queued entries (planMatches), claims each
// match's entries and hands the match to the room creator.
func (m *Matchmaker) tick() {
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	if err := m.queue.MigrateLegacy(m.ctx); err != nil {
		observability.Log.Warn().Err(err).Msg("matchmaker: tick MigrateLegacy error")
	}
	for _, queue := range Queues {
		entries, err := m.queue.GetAllEntries(m.ctx, queue)
		if err != nil {
			observability.Log.Error().Err(err).Str("queue", queue).Msg("matchmaker: tick GetAllEntries error")
			continue
		}
		if len(entries) == 0 {
			continue
		}
		for _, result := range planMatches(entries, time.Now().Unix(), cfg) {
			result.Queue = queue
			m.startMatch(m.ctx, result)
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

// startMatch claims the match's entries (skipping a match one of whose entries
// was cancelled meanwhile), picks the map and creates the battle room. A room
// that cannot be created puts the entries back in the queue.
func (m *Matchmaker) startMatch(ctx context.Context, result MatchResult) {
	entries := result.Entries()
	ok, err := m.queue.Claim(ctx, entries)
	if err != nil {
		observability.Log.Error().Err(err).Msg("matchmaker: claim entries error")
	}
	if !ok {
		return
	}

	m.mu.RLock()
	eloConfig := m.eloConfig
	botConfig := m.botConfig
	cfg := m.cfg
	m.mu.RUnlock()

	result.MapID = m.randomMapForRating(result.MaxHumanRating())

	if err := m.roomCreator.CreateBattleFromMatch(ctx, result, botConfig, eloConfig, cfg); err != nil {
		observability.Log.Error().Err(err).Str("mapID", result.MapID).Msg("matchmaker: CreateBattleFromMatch error, re-queueing entries")
		for _, e := range entries {
			if err := m.queue.Enqueue(ctx, e); err != nil {
				observability.Log.Error().Err(err).Str("entryID", e.EntryID).Msg("matchmaker: re-queue entry error")
			}
		}
		return
	}

	observability.Log.Info().
		Str("queue", result.Mode()).
		Int("team1Humans", result.Teams[0].Humans()).
		Int("team2Humans", result.Teams[1].Humans()).
		Bool("hasBot", result.HasBot).
		Str("mapID", result.MapID).
		Msg("matchmaker: match formed")
}

// ---------------------------------------------------------------------------
// Public queue API
// ---------------------------------------------------------------------------

// ErrUnknownQueue: EnqueueLobby was given a queue the matchmaker does not run.
var ErrUnknownQueue = errors.New("matchmaker: unknown queue")

// EnqueueLobby builds a QueueEntry from the provided lobby details, calculates
// the party rating, and enqueues it in the queue (ErrUnknownQueue unless one of
// Queues).  Returns the created entry on success.
func (m *Matchmaker) EnqueueLobby(
	ctx context.Context,
	queue string,
	lobbyID string,
	playerIDs []string,
	playerRatings map[string]int,
	playerChars map[string]string,
	playerItems map[string][]string,
	playerNames map[string]string,
) (*QueueEntry, error) {
	if !KnownQueue(queue) {
		return nil, ErrUnknownQueue
	}
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	entryID := generateMatchmakerID()

	entry := QueueEntry{
		EntryID:       entryID,
		Queue:         queue,
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
		Str("queue", queue).
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

// randomMapForRating picks a random map whose MinRankTier is at or below the
// tier derived from maxRating.  Falls back to "default_map" when no maps exist.
func (m *Matchmaker) randomMapForRating(maxRating int) string {
	m.mu.RLock()
	ids := m.mapIDs
	tiers := m.mapTiers
	m.mu.RUnlock()

	playerIdx := ranked.TierIndex(ranked.Tier(maxRating))

	var eligible []string
	for _, id := range ids {
		mapTier, ok := tiers[id]
		if !ok {
			mapTier = "bronze"
		}
		if ranked.TierIndex(mapTier) <= playerIdx {
			eligible = append(eligible, id)
		}
	}

	if len(eligible) == 0 {
		if len(ids) == 0 {
			return "default_map"
		}
		eligible = ids
	}

	return eligible[mrand.IntN(len(eligible))]
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
