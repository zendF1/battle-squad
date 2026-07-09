-- migrations/008_brick_border_v2.up.sql

-- Recreate brick types with SERIAL PK, border, image_id, color
DROP TABLE IF EXISTS config_brick_types;
CREATE TABLE IF NOT EXISTS config_brick_types (
    brick_type_id SERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    image_id      TEXT NOT NULL DEFAULT '',
    destructible  BOOLEAN NOT NULL DEFAULT true,
    border        JSONB NOT NULL DEFAULT '{"top":[{"x":0,"y":16},{"x":16,"y":16}],"right":[{"x":16,"y":16},{"x":16,"y":0}],"bottom":[{"x":16,"y":0},{"x":0,"y":0}],"left":[{"x":0,"y":0},{"x":0,"y":16}]}',
    color         TEXT NOT NULL DEFAULT '#8B4513',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Reset tiles to empty since format changes from string to int
UPDATE config_maps SET tiles = '[]'::jsonb;
