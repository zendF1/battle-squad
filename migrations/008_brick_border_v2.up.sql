-- migrations/008_brick_border_v2.up.sql

-- Create brick types table with SERIAL PK if it does not exist
CREATE TABLE IF NOT EXISTS config_brick_types (
    brick_type_id SERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    image_id      TEXT NOT NULL DEFAULT '',
    destructible  BOOLEAN NOT NULL DEFAULT true,
    border        JSONB NOT NULL DEFAULT '{"bottom":[{"x":0,"y":0},{"x":16,"y":0}],"right":[{"x":16,"y":0},{"x":16,"y":16}],"top":[{"x":16,"y":16},{"x":0,"y":16}],"left":[{"x":0,"y":16},{"x":0,"y":0}]}',
    color         TEXT NOT NULL DEFAULT '#8B4513',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Add columns that may be missing if table was created by an older migration
ALTER TABLE config_brick_types ADD COLUMN IF NOT EXISTS image_id TEXT NOT NULL DEFAULT '';
ALTER TABLE config_brick_types ADD COLUMN IF NOT EXISTS border JSONB NOT NULL DEFAULT '{"bottom":[{"x":0,"y":0},{"x":16,"y":0}],"right":[{"x":16,"y":0},{"x":16,"y":16}],"top":[{"x":16,"y":16},{"x":0,"y":16}],"left":[{"x":0,"y":16},{"x":0,"y":0}]}';
ALTER TABLE config_brick_types ADD COLUMN IF NOT EXISTS color TEXT NOT NULL DEFAULT '#8B4513';
