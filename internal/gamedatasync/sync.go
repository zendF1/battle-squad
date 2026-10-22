// Package gamedatasync keeps the game data of a server process (cmd/game, cmd/api, cmd/admin)
// in step with the DB: it loads a snapshot (game/gamedata), stamps it with its GET /gamedata
// version, archives that payload, publishes the snapshot and the rank bands, and reloads when
// an admin save notifies it (Redis pub/sub on Channel) or, as a fallback, when the stored
// revision (gamedata_revision, bumped by a trigger on every config table) changes.
//
// The per-match snapshot is the match's own (match.NewMatchWithData): a reload only changes
// what new matches and new API requests see.
package gamedatasync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	apigamedata "battle-squad/internal/api/gamedata"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"
)

const (
	// Channel is the Redis pub/sub channel an admin save publishes on (the payload is the new
	// revision, informational).
	Channel = "gamedata:changed"
	// LoadedKey is the Redis hash where every process reports the game data it serves:
	// "<service>/<node>" → LoadedStatus JSON.
	LoadedKey = "gamedata:loaded"
	// DefaultPollInterval is how often the stored revision is checked when no notification
	// arrives.
	DefaultPollInterval = 10 * time.Second
	// archiveKeep is how many archived payloads (gamedata_versions) outlive the 30 day prune.
	archiveKeep = 50
)

// LoadedStatus is what a process reports in LoadedKey.
type LoadedStatus struct {
	Service  string    `json:"service"`
	Node     string    `json:"node"`
	Version  string    `json:"version"`
	Revision int64     `json:"revision"`
	Source   string    `json:"source"`
	LoadedAt time.Time `json:"loadedAt"`
	// Error is the last reload failure ("" when the last reload worked): the process keeps
	// serving its previous snapshot.
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Reloader keeps one process's game data current.
type Reloader struct {
	db        *database.PostgresDB
	redis     *database.RedisClient // nil: no notifications, poll only
	service   string
	node      string
	configDir string // YAML fallback for the first load ("" none)
	Poll      time.Duration
	// Channel and LoadedKey default to the package constants (tests use their own).
	Channel   string
	LoadedKey string

	mu        sync.Mutex // one reload at a time
	revision  int64      // revision of the last DB read
	lastError string
	onPublish []func(*gamedata.Snapshot)
}

// New is a Reloader for service ("game", "api", "admin") on node.
func New(db *database.PostgresDB, redis *database.RedisClient, service, node, configDir string) *Reloader {
	return &Reloader{db: db, redis: redis, service: service, node: node, configDir: configDir, Poll: DefaultPollInterval,
		Channel: Channel, LoadedKey: LoadedKey}
}

// OnPublish runs f with every snapshot the reloader publishes (on the reloading goroutine),
// e.g. to refresh the matchmaker's map list.
func (r *Reloader) OnPublish(f func(*gamedata.Snapshot)) {
	r.mu.Lock()
	r.onPublish = append(r.onPublish, f)
	r.mu.Unlock()
}

// Start loads the game data (the DB, else the YAML files of configDir) and publishes it, then
// listens for notifications and polls the revision until ctx ends. It fails only when neither
// source loads.
func (r *Reloader) Start(ctx context.Context) error {
	if _, err := r.Reload(ctx); err != nil {
		if r.configDir == "" {
			return err
		}
		observability.Log.Warn().Err(err).Msg("game data: failed to load from the DB, falling back to YAML")
		snap, yerr := gamedata.LoadYAML(r.configDir)
		if yerr != nil {
			return fmt.Errorf("game data: DB: %v; YAML: %w", err, yerr)
		}
		r.mu.Lock()
		r.publish(ctx, snap)
		r.mu.Unlock()
	}
	go r.listen(ctx)
	go r.poll(ctx)
	return nil
}

// Reload reads the DB and publishes the snapshot when its content changed. On failure the
// current snapshot stays (and the failure is reported in LoadedKey).
func (r *Reloader) Reload(ctx context.Context) (*gamedata.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, err := gamedata.LoadFromDB(ctx, r.db)
	if err != nil {
		r.lastError = err.Error()
		observability.Log.Error().Err(err).Str("service", r.service).Msg("game data reload failed; keeping the current game data")
		r.report(ctx, gamedata.Current())
		return nil, err
	}
	r.lastError = ""
	r.revision = snap.Revision
	snap.Version = apigamedata.VersionOf(snap)
	cur := gamedata.Current()
	if cur.Version == snap.Version && cur.Source == "db" {
		r.report(ctx, cur) // same content: matches and the API keep the snapshot they have
		return cur, nil
	}
	r.publish(ctx, snap)
	observability.Log.Info().Str("service", r.service).Str("version", snap.Version).Int64("revision", snap.Revision).
		Str("previous", cur.Version).Msg("game data loaded")
	return snap, nil
}

// publish stamps (when needed), archives and publishes snap with its rank bands.
func (r *Reloader) publish(ctx context.Context, snap *gamedata.Snapshot) {
	if snap.Version == "" {
		snap.Version = apigamedata.VersionOf(snap)
	}
	if err := Archive(ctx, r.db, snap); err != nil {
		observability.Log.Warn().Err(err).Msg("game data: failed to archive the payload")
	}
	if err := ranked.SetBands(snap.RankBands()); err != nil {
		observability.Log.Warn().Err(err).Msg("game data: rank bands not valid, keeping the previous ones")
	}
	gamedata.Publish(snap)
	for _, f := range r.onPublish {
		f(snap)
	}
	r.report(ctx, snap)
}

// listen reloads on every notification (go-redis resubscribes after a lost connection).
func (r *Reloader) listen(ctx context.Context) {
	if r.redis == nil {
		return
	}
	sub := r.redis.Client.Subscribe(ctx, r.Channel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			_, _ = r.Reload(ctx)
		}
	}
}

// poll reloads when the stored revision moved without a notification (a lost message, an
// edit made with SQL) and re-reports the status.
func (r *Reloader) poll(ctx context.Context) {
	if r.Poll <= 0 {
		return
	}
	t := time.NewTicker(r.Poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rev, err := Revision(ctx, r.db)
			r.mu.Lock()
			changed := err == nil && rev != r.revision
			failing := r.lastError != ""
			r.mu.Unlock()
			if changed || failing {
				_, _ = r.Reload(ctx)
			} else {
				r.mu.Lock()
				r.report(ctx, gamedata.Current())
				r.mu.Unlock()
			}
		}
	}
}

// report writes this process's status to LoadedKey (with r.mu held).
func (r *Reloader) report(ctx context.Context, snap *gamedata.Snapshot) {
	if r.redis == nil {
		return
	}
	rev := snap.Revision
	if snap.Source == "db" && r.revision > rev {
		rev = r.revision // the last read had the same content
	}
	st := LoadedStatus{Service: r.service, Node: r.node, Version: snap.Version, Revision: rev, Source: snap.Source,
		LoadedAt: snap.LoadedAt, Error: r.lastError, CheckedAt: time.Now()}
	raw, _ := json.Marshal(st)
	if err := r.redis.Client.HSet(ctx, r.LoadedKey, r.service+"/"+r.node, raw).Err(); err != nil {
		observability.Log.Debug().Err(err).Msg("game data: failed to report the loaded version")
	}
}

// Revision is the stored game data revision.
func Revision(ctx context.Context, db *database.PostgresDB) (int64, error) {
	var rev int64
	err := db.Pool.QueryRow(ctx, `SELECT revision FROM gamedata_revision WHERE id = 1`).Scan(&rev)
	return rev, err
}

// Bump raises the stored revision (a forced reload: nothing else changed) and returns it.
func Bump(ctx context.Context, db *database.PostgresDB) (int64, error) {
	var rev int64
	err := db.Pool.QueryRow(ctx, `UPDATE gamedata_revision SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1 RETURNING revision`).Scan(&rev)
	return rev, err
}

// Notify tells every process to reload (the revision is informational).
func Notify(ctx context.Context, redis *database.RedisClient, revision int64) error {
	return NotifyOn(ctx, redis, Channel, revision)
}

// NotifyOn is Notify on another channel (tests).
func NotifyOn(ctx context.Context, redis *database.RedisClient, channel string, revision int64) error {
	if redis == nil {
		return errors.New("no redis")
	}
	return redis.Client.Publish(ctx, channel, fmt.Sprint(revision)).Err()
}

// Statuses are the statuses every process reported, by "<service>/<node>".
func Statuses(ctx context.Context, redis *database.RedisClient) (map[string]LoadedStatus, error) {
	raw, err := redis.Client.HGetAll(ctx, LoadedKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]LoadedStatus, len(raw))
	for k, v := range raw {
		var st LoadedStatus
		if json.Unmarshal([]byte(v), &st) == nil {
			out[k] = st
		}
	}
	return out, nil
}

// Archive stores snap's GET /gamedata payload under its version (kept as it is: a match that
// started with it can still be served its exact game data, GET /gamedata?version=), and prunes
// payloads older than 30 days beyond the newest archiveKeep.
func Archive(ctx context.Context, db *database.PostgresDB, snap *gamedata.Snapshot) error {
	payload, err := json.Marshal(apigamedata.Stamped(snap))
	if err != nil {
		return err
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO gamedata_versions (version, payload) VALUES ($1, $2)
		ON CONFLICT (version) DO UPDATE SET last_seen_at = CURRENT_TIMESTAMP`, snap.Version, string(payload)); err != nil {
		return fmt.Errorf("archive game data %s: %w", snap.Version, err)
	}
	_, err = db.Pool.Exec(ctx, `DELETE FROM gamedata_versions WHERE last_seen_at < CURRENT_TIMESTAMP - INTERVAL '30 days'
		AND version NOT IN (SELECT version FROM gamedata_versions ORDER BY last_seen_at DESC LIMIT $1)`, archiveKeep)
	return err
}
