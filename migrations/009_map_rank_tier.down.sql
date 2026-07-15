-- migrations/009_map_rank_tier.down.sql
ALTER TABLE config_maps
    DROP COLUMN IF EXISTS min_rank_tier;
