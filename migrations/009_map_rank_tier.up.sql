-- migrations/009_map_rank_tier.up.sql
ALTER TABLE config_maps
    ADD COLUMN IF NOT EXISTS min_rank_tier VARCHAR(20) NOT NULL DEFAULT 'bronze';
