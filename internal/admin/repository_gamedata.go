package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The game data columns and tables the client reads through GET /gamedata (texts, skill
// tuning, rules). Every write fires the gamedata_revision trigger; the router's middleware then
// notifies the servers (notifyOnChange).

// CharacterTexts are the display columns of a config_characters row.
type CharacterTexts struct {
	RoleName, Playstyle, GunKind string
	SortOrder                    int
}

// GetCharacterTexts returns a character's display columns.
func (r *Repository) GetCharacterTexts(ctx context.Context, id string) (CharacterTexts, error) {
	var t CharacterTexts
	err := r.db.Pool.QueryRow(ctx, `SELECT role_name, playstyle, gun_kind, sort_order FROM config_characters WHERE character_id = $1`, id).
		Scan(&t.RoleName, &t.Playstyle, &t.GunKind, &t.SortOrder)
	return t, err
}

// UpdateCharacterTexts sets a character's display columns.
func (r *Repository) UpdateCharacterTexts(ctx context.Context, id string, t CharacterTexts) error {
	_, err := r.db.Pool.Exec(ctx, `UPDATE config_characters SET role_name = $2, playstyle = $3, gun_kind = $4, sort_order = $5,
		updated_at = CURRENT_TIMESTAMP WHERE character_id = $1`, id, t.RoleName, t.Playstyle, t.GunKind, t.SortOrder)
	if err != nil {
		return fmt.Errorf("update character texts %s: %w", id, err)
	}
	return nil
}

// ItemTexts are the display columns of a config_items row.
type ItemTexts struct {
	DisplayName string
	SortOrder   int
}

// GetItemTexts returns an item's display columns.
func (r *Repository) GetItemTexts(ctx context.Context, id string) (ItemTexts, error) {
	var t ItemTexts
	err := r.db.Pool.QueryRow(ctx, `SELECT display_name, sort_order FROM config_items WHERE item_id = $1`, id).Scan(&t.DisplayName, &t.SortOrder)
	return t, err
}

// UpdateItemTexts sets an item's display columns.
func (r *Repository) UpdateItemTexts(ctx context.Context, id string, t ItemTexts) error {
	_, err := r.db.Pool.Exec(ctx, `UPDATE config_items SET display_name = $2, sort_order = $3, updated_at = CURRENT_TIMESTAMP
		WHERE item_id = $1`, id, t.DisplayName, t.SortOrder)
	if err != nil {
		return fmt.Errorf("update item texts %s: %w", id, err)
	}
	return nil
}

// SkillTuning is a skill's description template and tuning (config_skills.params).
type SkillTuning struct {
	SkillID, Name, EffectType, Description, Template string
	Params                                           map[string]float64
}

// GetSkillTuning returns a skill's tuning columns.
func (r *Repository) GetSkillTuning(ctx context.Context, id string) (*SkillTuning, error) {
	t := &SkillTuning{}
	var raw []byte
	err := r.db.Pool.QueryRow(ctx, `SELECT skill_id, name, effect_type, COALESCE(description, ''), description_template, params
		FROM config_skills WHERE skill_id = $1`, id).Scan(&t.SkillID, &t.Name, &t.EffectType, &t.Description, &t.Template, &raw)
	if err != nil {
		return nil, fmt.Errorf("get skill tuning %s: %w", id, err)
	}
	t.Params = map[string]float64{}
	_ = json.Unmarshal(raw, &t.Params)
	return t, nil
}

// UpdateSkillTuning sets a skill's description template and params.
func (r *Repository) UpdateSkillTuning(ctx context.Context, id, template string, params map[string]float64) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	tag, err := r.db.Pool.Exec(ctx, `UPDATE config_skills SET description_template = $2, params = $3::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE skill_id = $1`, id, template, string(raw))
	if err != nil {
		return fmt.Errorf("update skill tuning %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("skill %s not found", id)
	}
	return nil
}

// SettingsMap is every game_settings row (key → value).
func (r *Repository) SettingsMap(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT key, value FROM game_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSetting writes a game_settings row (inserting it when missing).
func (r *Repository) SetSetting(ctx context.Context, key, value, valueType, description, category string) error {
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO game_settings (key, value, value_type, description, category) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP`, key, value, valueType, description, category)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// TextRows are the rows of a text table (keyed by column name), in its order.
func (r *Repository) TextRows(ctx context.Context, spec *textTable) ([]map[string]string, error) {
	cols := append([]string{spec.Key}, spec.colNames()...)
	rows, err := r.db.Pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM %s ORDER BY %s`, castText(cols), spec.Table, spec.Order))
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", spec.Table, err)
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan %s: %w", spec.Table, err)
		}
		row := map[string]string{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// castText selects every column as text.
func castText(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c + "::text"
	}
	return strings.Join(out, ", ")
}

// UpsertTextRow inserts or updates one row of a text table (values already validated), then
// runs the table's whole-table check in the same transaction.
func (r *Repository) UpsertTextRow(ctx context.Context, spec *textTable, key string, values map[string]any) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	cols := spec.colNames()
	args := []any{key}
	ph := []string{"$1"}
	set := []string{}
	for i, c := range cols {
		args = append(args, values[c])
		ph = append(ph, fmt.Sprintf("$%d", i+2))
		set = append(set, fmt.Sprintf("%s = EXCLUDED.%s", c, c))
	}
	set = append(set, "updated_at = CURRENT_TIMESTAMP")
	q := fmt.Sprintf(`INSERT INTO %s (%s, %s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s`,
		spec.Table, spec.Key, strings.Join(cols, ", "), strings.Join(ph, ", "), spec.Key, strings.Join(set, ", "))
	if _, err := tx.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("save %s %s: %w", spec.Table, key, err)
	}
	if spec.Check != nil {
		if err := spec.Check(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteTextRow deletes one row of a text table.
func (r *Repository) DeleteTextRow(ctx context.Context, spec *textTable, key string) error {
	tag, err := r.db.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, spec.Table, spec.Key), key)
	if err != nil {
		return fmt.Errorf("delete %s %s: %w", spec.Table, key, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%s %s not found", spec.Table, key)
	}
	return nil
}
