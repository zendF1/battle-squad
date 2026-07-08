-- migrations/007_map_editor.up.sql

-- Brick type registry
CREATE TABLE IF NOT EXISTS config_brick_types (
    brick_type_id TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    destructible  BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seed default brick types
INSERT INTO config_brick_types (brick_type_id, name, destructible) VALUES
    ('dirt', 'Dirt', true),
    ('rock', 'Rock', false),
    ('ice', 'Ice', true),
    ('lava', 'Lava', false),
    ('fragile', 'Fragile', true)
ON CONFLICT (brick_type_id) DO NOTHING;

-- Add tilemap columns to config_maps
ALTER TABLE config_maps
    ADD COLUMN IF NOT EXISTS grid_width  INT NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS grid_height INT NOT NULL DEFAULT 56,
    ADD COLUMN IF NOT EXISTS cell_size   INT NOT NULL DEFAULT 16,
    ADD COLUMN IF NOT EXISTS tiles       JSONB NOT NULL DEFAULT '[]';
