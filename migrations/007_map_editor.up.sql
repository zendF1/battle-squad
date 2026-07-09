-- migrations/007_map_editor.up.sql

-- Add tilemap columns to config_maps
-- (config_brick_types is created by 008_brick_border_v2.up.sql)
ALTER TABLE config_maps
    ADD COLUMN IF NOT EXISTS grid_width  INT NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS grid_height INT NOT NULL DEFAULT 56,
    ADD COLUMN IF NOT EXISTS cell_size   INT NOT NULL DEFAULT 16,
    ADD COLUMN IF NOT EXISTS tiles       JSONB NOT NULL DEFAULT '[]';
