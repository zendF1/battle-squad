package milestone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

// querier is a pool or a tx: reads run on the pool outside a claim and on the tx inside it.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// Milestones returns the active milestones with the player's claim flag; title items carry
// the name and rarity of their active config_titles row (none: left empty, so unsupported).
func (r *Repository) Milestones(ctx context.Context, q querier, playerID string) ([]Milestone, error) {
	ms, err := r.milestones(ctx, q, playerID)
	if err != nil {
		return nil, err
	}
	hasTitle := false
	for _, m := range ms {
		for _, it := range m.Rewards.Items {
			hasTitle = hasTitle || it.Kind == RewardKindTitle
		}
	}
	if !hasTitle {
		return ms, nil
	}
	titles, err := activeTitles(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		for j := range ms[i].Rewards.Items {
			it := &ms[i].Rewards.Items[j]
			if it.Kind != RewardKindTitle {
				continue
			}
			t := titles[it.ID]
			it.Name, it.Rarity, it.Qty = t.Name, t.Rarity, 0
		}
	}
	return ms, nil
}

type titleInfo struct{ Name, Rarity string }

// activeTitles maps the active config_titles rows by id.
func activeTitles(ctx context.Context, q querier) (map[string]titleInfo, error) {
	rows, err := q.Query(ctx, `SELECT title_id, name, rarity FROM config_titles WHERE is_active = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]titleInfo{}
	for rows.Next() {
		var id string
		var t titleInfo
		if err := rows.Scan(&id, &t.Name, &t.Rarity); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

func (r *Repository) milestones(ctx context.Context, q querier, playerID string) ([]Milestone, error) {
	rows, err := q.Query(ctx, `
		SELECT m.mission_id, m.track, m.target, m.required_value, m.sort_order, m.description,
		       m.reward_coin, m.reward_gem, m.reward_items, COALESCE(p.is_claimed, FALSE)
		FROM missions m
		LEFT JOIN mission_progress p ON p.mission_id = m.mission_id AND p.player_id = $1
		WHERE m.type = $2 AND m.is_active = TRUE`, playerID, MissionType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Milestone
	for rows.Next() {
		var m Milestone
		var items []byte
		if err := rows.Scan(&m.ID, &m.Track, &m.Target, &m.Required, &m.SortOrder, &m.Description,
			&m.Rewards.Coin, &m.Rewards.Gem, &items, &m.Claimed); err != nil {
			return nil, err
		}
		if len(items) > 0 {
			if err := json.Unmarshal(items, &m.Rewards.Items); err != nil {
				// Malformed reward_items: an unknown kind keeps the row out of the list
				m.Rewards.Items = []RewardItem{{Kind: "invalid"}}
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Progress loads what node progress is derived from.
func (r *Repository) Progress(ctx context.Context, q querier, playerID string) (PlayerProgress, error) {
	p := PlayerProgress{Counters: map[string]int{}, CharLevels: map[string]int{}}

	rows, err := q.Query(ctx, `SELECT counter, value FROM player_stat_counters WHERE player_id = $1`, playerID)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var k string
		var v int
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return p, err
		}
		p.Counters[k] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, err
	}

	rows, err = q.Query(ctx, `SELECT character_id, level FROM player_characters WHERE player_id = $1`, playerID)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var id string
		var lv int
		if err := rows.Scan(&id, &lv); err != nil {
			rows.Close()
			return p, err
		}
		p.CharLevels[id] = lv
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, err
	}

	rows, err = q.Query(ctx, `
		SELECT slot, upgrade_level, CASE WHEN is_equipped THEN COALESCE(equipped_on, '') ELSE '' END
		FROM player_equipment WHERE player_id = $1`, playerID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var pc Piece
		if err := rows.Scan(&pc.Slot, &pc.Level, &pc.EquippedOn); err != nil {
			return p, err
		}
		p.Pieces = append(p.Pieces, pc)
	}
	return p, rows.Err()
}

// LockProfileTx locks the player's profile row, serializing the player's claims.
func (r *Repository) LockProfileTx(ctx context.Context, tx pgx.Tx, playerID string) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM player_profiles WHERE player_id = $1 FOR UPDATE`, playerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// InsertClaimTx records the claim; false when it was already claimed.
func (r *Repository) InsertClaimTx(ctx context.Context, tx pgx.Tx, playerID, id string, progress int) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO mission_progress (player_id, mission_id, current_value, is_claimed) VALUES ($1, $2, $3, TRUE)
		ON CONFLICT (player_id, mission_id) DO UPDATE
		SET is_claimed = TRUE, current_value = EXCLUDED.current_value, updated_at = CURRENT_TIMESTAMP
		WHERE mission_progress.is_claimed = FALSE`, playerID, id, progress)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ItemSource is inventory_items.source of an item granted by a milestone.
const ItemSource = "milestone_reward"

// GrantItemTx grants one milestone reward item.
func (r *Repository) GrantItemTx(ctx context.Context, tx pgx.Tx, playerID string, it RewardItem) error {
	return GrantRewardItemTx(ctx, tx, playerID, it, ItemSource)
}

// GrantRewardItemTx grants one reward item inside tx; source is inventory_items.source of an
// item. Milestones and the daily mission chests grant through it. This switch is the
// extension point for new reward kinds: add the kind to supportedRewardKinds and a case here.
func GrantRewardItemTx(ctx context.Context, tx pgx.Tx, playerID string, it RewardItem, source string) error {
	switch it.Kind {
	case RewardKindStone:
		tag, err := tx.Exec(ctx, `
			INSERT INTO player_stones (player_id, stone_level, quantity) VALUES ($1, $2, $3)
			ON CONFLICT (player_id, stone_level) DO UPDATE SET quantity = player_stones.quantity + EXCLUDED.quantity`,
			playerID, it.Level, it.Qty)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("grant stone %d: %d rows affected", it.Level, tag.RowsAffected())
		}
		return nil
	case RewardKindItem:
		tag, err := tx.Exec(ctx, `
			INSERT INTO inventory_items (player_id, item_id, quantity, source) VALUES ($1, $2, $3, $4)
			ON CONFLICT (player_id, item_id) DO UPDATE SET quantity = inventory_items.quantity + EXCLUDED.quantity`,
			playerID, it.ID, it.Qty, source)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("grant item %s: %d rows affected", it.ID, tag.RowsAffected())
		}
		return nil
	case RewardKindTitle:
		// Owned once: 0 rows (already owned, e.g. granted by another milestone) is fine
		_, err := tx.Exec(ctx, `
			INSERT INTO player_titles (player_id, title_id) VALUES ($1, $2)
			ON CONFLICT (player_id, title_id) DO NOTHING`, playerID, it.ID)
		return err
	}
	return fmt.Errorf("unsupported reward kind %q", it.Kind)
}

// BalancesTx returns the player's coin, gem and stones.
func (r *Repository) BalancesTx(ctx context.Context, tx pgx.Tx, playerID string) (int, int, []StoneBalance, error) {
	var coin, gem int
	if err := tx.QueryRow(ctx, `SELECT coin, gem FROM player_profiles WHERE player_id = $1`, playerID).Scan(&coin, &gem); err != nil {
		return 0, 0, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT stone_level, quantity FROM player_stones WHERE player_id = $1 ORDER BY stone_level`, playerID)
	if err != nil {
		return 0, 0, nil, err
	}
	defer rows.Close()
	stones := []StoneBalance{}
	for rows.Next() {
		var s StoneBalance
		if err := rows.Scan(&s.StoneLevel, &s.Quantity); err != nil {
			return 0, 0, nil, err
		}
		stones = append(stones, s)
	}
	return coin, gem, stones, rows.Err()
}
