-- migrations/008_brick_border_v2.down.sql

DROP TABLE IF EXISTS config_brick_types;
CREATE TABLE IF NOT EXISTS config_brick_types (
    brick_type_id TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    destructible  BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

UPDATE config_maps SET tiles = '[]'::jsonb;
