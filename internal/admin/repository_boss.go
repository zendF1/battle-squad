package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"battle-squad/internal/game/gamedata"

	"github.com/jackc/pgx/v5"
)

// BossRow is a config_bosses row as the admin Boss page edits it: the scalar columns and the
// JSON columns as indented text.
type BossRow struct {
	Config                                          gamedata.BossConfig
	SkillsJSON, MinionsJSON, WavesJSON, RewardsJSON string
}

const bossColumns = `boss_id, name, description, recommended_level, enabled, sort_order, map_id, hp, defense, damage,
	body_size, turn_limit, shielded_damage_factor, out_of_map, skills, minions, waves, rewards, daily_bonus_runs`

func scanBoss(row pgx.Row) (BossRow, error) {
	var b BossRow
	c := &b.Config
	var skills, minions, waves, rewards []byte
	if err := row.Scan(&c.BossID, &c.Name, &c.Description, &c.RecommendedLevel, &c.Enabled, &c.SortOrder, &c.MapID, &c.HP,
		&c.Defense, &c.Damage, &c.BodySize, &c.TurnLimit, &c.ShieldedDamageFactor, &c.OutOfMap, &skills, &minions, &waves, &rewards,
		&c.DailyBonusRuns); err != nil {
		return b, err
	}
	b.SkillsJSON, b.MinionsJSON, b.WavesJSON, b.RewardsJSON = indentJSON(skills), indentJSON(minions), indentJSON(waves), indentJSON(rewards)
	return b, nil
}

func indentJSON(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	return string(out)
}

// Bosses lists config_bosses in display order.
func (r *Repository) Bosses(ctx context.Context) ([]BossRow, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT `+bossColumns+` FROM config_bosses ORDER BY sort_order, boss_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BossRow
	for rows.Next() {
		b, err := scanBoss(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Boss is one config_bosses row.
func (r *Repository) Boss(ctx context.Context, id string) (BossRow, error) {
	return scanBoss(r.db.Pool.QueryRow(ctx, `SELECT `+bossColumns+` FROM config_bosses WHERE boss_id = $1`, id))
}

// SaveBoss validates b against the config tables (characters, items, the arena map) and
// upserts it, in one transaction (the revision trigger reloads the servers).
func (r *Repository) SaveBoss(ctx context.Context, b gamedata.BossConfig) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	chars := map[string]gamedata.CharacterConfig{}
	items := map[string]gamedata.ItemConfig{}
	if err := collectIDs(ctx, tx, `SELECT character_id FROM config_characters`, func(id string) { chars[id] = gamedata.CharacterConfig{} }); err != nil {
		return err
	}
	if err := collectIDs(ctx, tx, `SELECT item_id FROM config_items`, func(id string) { items[id] = gamedata.ItemConfig{} }); err != nil {
		return err
	}
	if err := gamedata.ValidateBoss(b, chars, items); err != nil {
		return err
	}
	var one int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM config_maps WHERE map_id = $1`, b.MapID).Scan(&one); errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("map %q is not in config_maps", b.MapID)
	} else if err != nil {
		return err
	}
	skills, _ := json.Marshal(b.Skills)
	minions, _ := json.Marshal(b.Minions)
	waves, _ := json.Marshal(b.Waves)
	rewards, _ := json.Marshal(b.Rewards)
	tag, err := tx.Exec(ctx, `
		INSERT INTO config_bosses (`+bossColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (boss_id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
			recommended_level = EXCLUDED.recommended_level, enabled = EXCLUDED.enabled, sort_order = EXCLUDED.sort_order,
			map_id = EXCLUDED.map_id, hp = EXCLUDED.hp, defense = EXCLUDED.defense, damage = EXCLUDED.damage,
			body_size = EXCLUDED.body_size, turn_limit = EXCLUDED.turn_limit, shielded_damage_factor = EXCLUDED.shielded_damage_factor,
			out_of_map = EXCLUDED.out_of_map, skills = EXCLUDED.skills, minions = EXCLUDED.minions, waves = EXCLUDED.waves,
			rewards = EXCLUDED.rewards, daily_bonus_runs = EXCLUDED.daily_bonus_runs, updated_at = CURRENT_TIMESTAMP`,
		b.BossID, b.Name, b.Description, b.RecommendedLevel, b.Enabled, b.SortOrder, b.MapID, b.HP, b.Defense, b.Damage,
		b.BodySize, b.TurnLimit, b.ShieldedDamageFactor, b.OutOfMap, skills, minions, waves, rewards, b.DailyBonusRuns)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("boss %s: %d rows written", b.BossID, tag.RowsAffected())
	}
	return tx.Commit(ctx)
}

// DeleteBoss deletes a boss no game mode uses.
func (r *Repository) DeleteBoss(ctx context.Context, id string) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var mode string
	err = tx.QueryRow(ctx, `SELECT mode_id FROM config_game_modes WHERE queue = $1 LIMIT 1`, gamedata.BossQueue(id)).Scan(&mode)
	if err == nil {
		return fmt.Errorf("game mode %s uses boss %s: delete or change it first", mode, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM config_bosses WHERE boss_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func collectIDs(ctx context.Context, tx pgx.Tx, query string, add func(string)) error {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		add(id)
	}
	return rows.Err()
}
