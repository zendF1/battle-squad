package gamedata

import (
	"context"
	"fmt"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/ranked"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// Load reads every config row GET /gamedata is built from (read-only, no transaction).
func (r *Repository) Load(ctx context.Context) (*source, error) {
	src := &source{Settings: map[string]string{}, RankTiers: map[string]string{}}
	pool := r.db.Pool

	if err := each(ctx, r, `SELECT key, value FROM game_settings`, func(rows pgx.Rows) error {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		src.Settings[k] = v
		return nil
	}); err != nil {
		return nil, fmt.Errorf("game_settings: %w", err)
	}
	var err error
	if src.Milestones, err = battlestats.LoadMilestones(ctx, pool); err != nil {
		return nil, err
	}
	// a missing or broken row keeps the defaults, as the match server and the API do
	src.Elo, _ = ranked.LoadEloConfig(ctx, pool)
	src.Matchmaking = matchmaker.LoadMatchmakingConfig(ctx, r.db)

	if err := each(ctx, r, `SELECT character_id, name, role, role_name, playstyle, gun_kind, weapon_id, skill_id,
		hp, damage, mobility, defense, skill_power, terrain_damage, difficulty
		FROM config_characters ORDER BY sort_order, character_id`, func(rows pgx.Rows) error {
		var c Character
		if err := rows.Scan(&c.CharacterID, &c.Name, &c.Role, &c.RoleName, &c.Playstyle, &c.GunKind, &c.WeaponID, &c.SkillID,
			&c.HP, &c.Damage, &c.Mobility, &c.Defense, &c.SkillPower, &c.TerrainDamage, &c.Difficulty); err != nil {
			return err
		}
		src.Characters = append(src.Characters, c)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("config_characters: %w", err)
	}

	if err := each(ctx, r, `SELECT weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight,
		wind_influence, multi_hit FROM config_weapons ORDER BY weapon_id`, func(rows pgx.Rows) error {
		var w Weapon
		if err := rows.Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius, &w.TerrainDamage, &w.ProjectileWeight,
			&w.WindInfluence, &w.MultiHit); err != nil {
			return err
		}
		src.Weapons = append(src.Weapons, w)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("config_weapons: %w", err)
	}

	if err := each(ctx, r, `SELECT s.skill_id, s.character_id, s.name, COALESCE(NULLIF(s.description_template, ''), s.description, ''), s.effect_type,
		COALESCE(s.status_effect_id, ''), s.cooldown_turn, s.projectile_count, s.damage_multiplier
		FROM config_skills s LEFT JOIN config_characters c ON c.character_id = s.character_id
		ORDER BY c.sort_order NULLS LAST, s.skill_id`, func(rows pgx.Rows) error {
		var s Skill
		if err := rows.Scan(&s.SkillID, &s.CharacterID, &s.Name, &s.Description, &s.EffectType,
			&s.StatusEffectID, &s.CooldownTurn, &s.ProjectileCount, &s.DamageMultiplier); err != nil {
			return err
		}
		src.Skills = append(src.Skills, s)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("config_skills: %w", err)
	}

	if err := each(ctx, r, `SELECT item_id, CASE WHEN display_name <> '' THEN display_name ELSE name END, type,
		target_type, value, max_use_per_match, cooldown FROM config_items ORDER BY sort_order, item_id`, func(rows pgx.Rows) error {
		var it Item
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Type, &it.TargetType, &it.Value, &it.MaxUsePerMatch, &it.Cooldown); err != nil {
			return err
		}
		src.Items = append(src.Items, it)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("config_items: %w", err)
	}

	if err := each(ctx, r, `SELECT effect_id, name, marker, banner FROM config_status_effects ORDER BY sort_order, effect_id`,
		func(rows pgx.Rows) error {
			var e statusText
			if err := rows.Scan(&e.EffectID, &e.Name, &e.Marker, &e.Banner); err != nil {
				return err
			}
			src.StatusEffects = append(src.StatusEffects, e)
			return nil
		}); err != nil {
		return nil, fmt.Errorf("config_status_effects: %w", err)
	}

	if err := each(ctx, r, `SELECT tier, name FROM config_rank_tiers`, func(rows pgx.Rows) error {
		var t, n string
		if err := rows.Scan(&t, &n); err != nil {
			return err
		}
		src.RankTiers[t] = n
		return nil
	}); err != nil {
		return nil, fmt.Errorf("config_rank_tiers: %w", err)
	}

	if err := each(ctx, r, `SELECT tier, name, kind, id_suffix FROM config_equipment_tiers ORDER BY sort_order, tier`,
		func(rows pgx.Rows) error {
			var t EquipmentTier
			if err := rows.Scan(&t.Tier, &t.Name, &t.Kind, &t.IDSuffix); err != nil {
				return err
			}
			src.EquipTiers = append(src.EquipTiers, t)
			return nil
		}); err != nil {
		return nil, fmt.Errorf("config_equipment_tiers: %w", err)
	}

	if err := each(ctx, r, `SELECT mode_id, kind, name, description, queue, bot FROM config_game_modes ORDER BY sort_order, mode_id`,
		func(rows pgx.Rows) error {
			var m modeRow
			if err := rows.Scan(&m.ModeID, &m.Kind, &m.Name, &m.Description, &m.Queue, &m.Bot); err != nil {
				return err
			}
			src.Modes = append(src.Modes, m)
			return nil
		}); err != nil {
		return nil, fmt.Errorf("config_game_modes: %w", err)
	}

	if err := each(ctx, r, `SELECT text FROM config_tips WHERE text <> '' ORDER BY sort_order, tip_id`, func(rows pgx.Rows) error {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		src.Tips = append(src.Tips, t)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("config_tips: %w", err)
	}
	return src, nil
}

// each runs query and calls scan for every row.
func each(ctx context.Context, r *Repository, query string, scan func(pgx.Rows) error) error {
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
