package character

import (
	"context"
	"fmt"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

// Repository handles all database operations for character progression.
type Repository struct {
	db *database.PostgresDB
}

// NewRepository creates a new Repository.
func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// GetPlayerCharacters returns all characters owned by a player, ordered by unlock time.
func (r *Repository) GetPlayerCharacters(ctx context.Context, playerID string) ([]PlayerCharacter, error) {
	query := `
		SELECT player_id, character_id, unlocked_at, level, exp, stat_points,
		       bonus_hp, bonus_damage, bonus_mobility, bonus_defense, bonus_skill_power, bonus_terrain_damage
		FROM player_characters
		WHERE player_id = $1
		ORDER BY unlocked_at ASC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chars []PlayerCharacter
	for rows.Next() {
		var c PlayerCharacter
		err := rows.Scan(
			&c.PlayerID,
			&c.CharacterID,
			&c.UnlockedAt,
			&c.Level,
			&c.Exp,
			&c.StatPoints,
			&c.BonusHP,
			&c.BonusDamage,
			&c.BonusMobility,
			&c.BonusDefense,
			&c.BonusSkillPower,
			&c.BonusTerrainDmg,
		)
		if err != nil {
			return nil, err
		}
		chars = append(chars, c)
	}
	return chars, nil
}

// GetPlayerCharacter returns a single owned character for a player.
func (r *Repository) GetPlayerCharacter(ctx context.Context, playerID, characterID string) (*PlayerCharacter, error) {
	query := `
		SELECT player_id, character_id, unlocked_at, level, exp, stat_points,
		       bonus_hp, bonus_damage, bonus_mobility, bonus_defense, bonus_skill_power, bonus_terrain_damage
		FROM player_characters
		WHERE player_id = $1 AND character_id = $2
	`
	var c PlayerCharacter
	err := r.db.Pool.QueryRow(ctx, query, playerID, characterID).Scan(
		&c.PlayerID,
		&c.CharacterID,
		&c.UnlockedAt,
		&c.Level,
		&c.Exp,
		&c.StatPoints,
		&c.BonusHP,
		&c.BonusDamage,
		&c.BonusMobility,
		&c.BonusDefense,
		&c.BonusSkillPower,
		&c.BonusTerrainDmg,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AllocateStats increments the specified bonus stats and decrements stat_points atomically.
// The WHERE clause guards against over-spending: stat_points must be >= total points spent.
func (r *Repository) AllocateStats(ctx context.Context, playerID, characterID string, hp, damage, mobility, defense, skillPower, terrainDmg int) error {
	total := hp + damage + mobility + defense + skillPower + terrainDmg
	query := `
		UPDATE player_characters
		SET
			bonus_hp             = bonus_hp             + $3,
			bonus_damage         = bonus_damage         + $4,
			bonus_mobility       = bonus_mobility       + $5,
			bonus_defense        = bonus_defense        + $6,
			bonus_skill_power    = bonus_skill_power    + $7,
			bonus_terrain_damage = bonus_terrain_damage + $8,
			stat_points          = stat_points          - $9
		WHERE player_id = $1 AND character_id = $2 AND stat_points >= $9
	`
	tag, err := r.db.Pool.Exec(ctx, query,
		playerID, characterID,
		hp, damage, mobility, defense, skillPower, terrainDmg,
		total,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insufficient stat points or character not found")
	}
	return nil
}

// ResetStats moves all allocated bonus points back into stat_points and zeroes all bonuses.
// Returns the new stat_points value.
func (r *Repository) ResetStats(ctx context.Context, playerID, characterID string) (int, error) {
	query := `
		UPDATE player_characters
		SET
			stat_points          = stat_points + bonus_hp + bonus_damage + bonus_mobility
			                       + bonus_defense + bonus_skill_power + bonus_terrain_damage,
			bonus_hp             = 0,
			bonus_damage         = 0,
			bonus_mobility       = 0,
			bonus_defense        = 0,
			bonus_skill_power    = 0,
			bonus_terrain_damage = 0
		WHERE player_id = $1 AND character_id = $2
		RETURNING stat_points
	`
	var newStatPoints int
	err := r.db.Pool.QueryRow(ctx, query, playerID, characterID).Scan(&newStatPoints)
	if err != nil {
		return 0, err
	}
	return newStatPoints, nil
}

// GetConfigValue fetches a single value from the game_settings table by key.
func (r *Repository) GetConfigValue(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// GetCharacterBonuses returns the current bonus stat values for a character.
// Used by the match engine to apply player-specific stat offsets.
func (r *Repository) GetCharacterBonuses(ctx context.Context, playerID, characterID string) (bonusHP, bonusDamage, bonusMobility, bonusDefense, bonusSkillPower, bonusTerrainDmg int, err error) {
	query := `
		SELECT bonus_hp, bonus_damage, bonus_mobility, bonus_defense, bonus_skill_power, bonus_terrain_damage
		FROM player_characters
		WHERE player_id = $1 AND character_id = $2
	`
	err = r.db.Pool.QueryRow(ctx, query, playerID, characterID).Scan(
		&bonusHP, &bonusDamage, &bonusMobility, &bonusDefense, &bonusSkillPower, &bonusTerrainDmg,
	)
	return
}

// AddExpAndLevelTx is a package-level function that adds EXP to a character within
// an existing transaction, handles INSERT if the row doesn't exist yet (rookie defaults),
// recalculates the level from the level curve, and awards stat_points for each level gained.
// Returns the new level and how many levels were gained.
func AddExpAndLevelTx(ctx context.Context, tx pgx.Tx, playerID, characterID string, expGained int, levels []LevelEntry, pointsPerLevel int) (newLevel, levelsGained int, err error) {
	// Upsert: insert with rookie defaults if the character row doesn't exist yet,
	// then add the gained EXP and lock the row for this transaction.
	upsert := `
		INSERT INTO player_characters (player_id, character_id, level, exp, stat_points)
		VALUES ($1, $2, 1, 0, 0)
		ON CONFLICT (player_id, character_id) DO NOTHING
	`
	_, err = tx.Exec(ctx, upsert, playerID, characterID)
	if err != nil {
		return 0, 0, fmt.Errorf("upsert player character: %w", err)
	}

	// Lock the row and fetch current state.
	var currentExp, currentLevel int
	lockQuery := `
		SELECT exp, level FROM player_characters
		WHERE player_id = $1 AND character_id = $2
		FOR UPDATE
	`
	err = tx.QueryRow(ctx, lockQuery, playerID, characterID).Scan(&currentExp, &currentLevel)
	if err != nil {
		return 0, 0, fmt.Errorf("lock player character: %w", err)
	}

	newExp := currentExp + expGained
	newLevel, _, _ = CalcLevelFromExp(newExp, levels)

	levelsGained = newLevel - currentLevel
	if levelsGained < 0 {
		levelsGained = 0
	}
	newStatPoints := levelsGained * pointsPerLevel

	update := `
		UPDATE player_characters
		SET exp = $3, level = $4, stat_points = stat_points + $5
		WHERE player_id = $1 AND character_id = $2
	`
	_, err = tx.Exec(ctx, update, playerID, characterID, newExp, newLevel, newStatPoints)
	if err != nil {
		return 0, 0, fmt.Errorf("update exp and level: %w", err)
	}

	return newLevel, levelsGained, nil
}
