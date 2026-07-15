-- migrations/007_map_editor.down.sql

ALTER TABLE config_maps
    DROP COLUMN IF EXISTS tiles,
    DROP COLUMN IF EXISTS cell_size,
    DROP COLUMN IF EXISTS grid_height,
    DROP COLUMN IF EXISTS grid_width;

DROP TABLE IF EXISTS config_brick_types;
