-- At most one equipped item per (player, character, slot).
-- Unequip duplicates left by the earlier race (keep the newest) before adding the index.

UPDATE player_equipment pe
SET is_equipped = FALSE, equipped_on = NULL
FROM (
    SELECT equipment_id,
           ROW_NUMBER() OVER (
               PARTITION BY player_id, equipped_on, slot
               ORDER BY created_at DESC, equipment_id
           ) AS rn
    FROM player_equipment
    WHERE is_equipped = TRUE
) dup
WHERE pe.equipment_id = dup.equipment_id AND dup.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uq_player_equipment_equipped_slot
    ON player_equipment(player_id, equipped_on, slot)
    WHERE is_equipped = TRUE;
