-- migrations/007_map_editor.up.sql

-- Brick type registry
CREATE TABLE config_brick_types (
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
    ('fragile', 'Fragile', true);

-- Add tilemap columns to config_maps
ALTER TABLE config_maps
    ADD COLUMN grid_width  INT NOT NULL DEFAULT 100,
    ADD COLUMN grid_height INT NOT NULL DEFAULT 56,
    ADD COLUMN cell_size   INT NOT NULL DEFAULT 16,
    ADD COLUMN tiles       JSONB NOT NULL DEFAULT '[]';
