# Map Editor Design

## Overview

Tilemap-based map editor integrated into the Admin Dashboard (port 9000). Allows admin/game designers to visually create and edit maps using a grid-based brick system (similar to Unity Tilemap). Maps are saved to DB and exported as YAML for use by game server and client.

## Data Model

### Brick Type Registry

New table `config_brick_types`:

| Column | Type | Description |
|--------|------|-------------|
| `brick_type_id` | TEXT PK | e.g. "grass", "stone", "ice" |
| `name` | TEXT | Display name |
| `destructible` | BOOLEAN | Whether explosions can destroy this brick |

Server stores only gameplay-relevant data. Client maps `brick_type_id` to sprite/image.

### Map Config (replaces current `config_maps` structure)

| Column | Type | Description |
|--------|------|-------------|
| `map_id` | TEXT PK | Unique identifier |
| `name` | TEXT | Display name |
| `grid_width` | INT | Number of horizontal cells (e.g. 100 = 1600px / 16px) |
| `grid_height` | INT | Number of vertical cells (e.g. 56 = 896px / 16px) |
| `cell_size` | INT | Fixed at 16px |
| `default_wind_power_range` | JSONB | `[0.0, 4.0]` — float values |
| `tiles` | JSONB | 2D array `[row][col]` = brick_type_id or null |
| `spawn_points` | JSONB | `[{x: 200.5, y: 400.0}, ...]` — pixel coords, float |

Removed fields: `terrain_layers`, `width`, `height` (computed from `grid_width * cell_size`, `grid_height * cell_size`).

### Tiles Format

```json
[
  [null, null, "grass", "grass", null],
  [null, "stone", "stone", "grass", null],
  ...
]
```

Row 0 = top of map. Each cell is either a `brick_type_id` string or `null` (air).

### Export YAML Format

```yaml
- mapId: grassland_valley
  name: Grassland Valley
  gridWidth: 100
  gridHeight: 56
  cellSize: 16
  defaultWindPowerRange: [0.0, 4.0]
  tiles:
    - [null, null, "grass", "grass"]
    - [null, "stone", "stone", "grass"]
  spawnPoints:
    - x: 200.5
      y: 400.0
    - x: 1400.0
      y: 400.0
```

## Admin Dashboard — Editor UI

### Page

New route: `/admin/maps/:id/editor`

### Layout

- **Left panel (toolbar):** Brick type selector, tool selector (Paint/Erase/Fill/Select/Spawn Point)
- **Center (canvas):** HTML5 Canvas grid, color-coded rectangles per brick type (no sprites)
- **Right panel (properties):** Map name, grid size, wind range, spawn points list
- **Top bar:** Save, Export YAML, Undo/Redo buttons

### Tools

| Tool | Behavior |
|------|----------|
| Paint | Click/drag to place selected brick type on cells |
| Erase | Click/drag to clear cells (set null) |
| Fill | Click on contiguous region of same type → flood fill with selected brick |
| Select/Move | Drag to select rectangular region → drag to move selection |
| Spawn Point | Click anywhere to add spawn point (pixel coords, no snap to grid) |

### Canvas Interaction

- Zoom in/out: scroll wheel
- Pan: middle-click drag or Space + drag
- Grid lines: toggle show/hide
- Hover: display cell coords (col, row) and pixel coords (x, y)

### Undo/Redo

History stack of tiles snapshots. Each action (paint stroke, erase, fill, move) creates one entry. Ctrl+Z / Ctrl+Y or toolbar buttons.

### Tech Stack

Vanilla JS + HTML5 Canvas. No JS framework — consistent with existing admin dashboard (server-rendered HTML templates). Editor is a Go template with separate JS file.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/maps/:id/editor` | Render editor page |
| GET | `/admin/api/maps/:id/tiles` | Return tiles + spawn points + map config (JSON) |
| PUT | `/admin/api/maps/:id/tiles` | Save tiles + spawn points |
| GET | `/admin/api/maps/:id/export` | Export map as YAML file download |
| GET | `/admin/api/brick-types` | List all brick types |
| POST | `/admin/api/brick-types` | Create/update brick type |
| DELETE | `/admin/api/brick-types/:id` | Delete brick type |

### Brick Types Management

New CRUD page at `/admin/brick-types` — same style as existing config pages. Fields: brick_type_id, name, destructible.

### Export Flow

1. Admin designs map in editor → Save (persists to DB)
2. Click "Export YAML" → server reads from DB, generates YAML, returns file download
3. Dev commits YAML to `configs/maps.yaml`
4. Game server loads YAML at startup (or directly from DB)

## Game Server Impact

### MapConfig Struct Changes

```go
type MapConfig struct {
    MapID                 string
    Name                  string
    GridWidth             int
    GridHeight            int
    CellSize              int
    DefaultWindPowerRange []float64  // float instead of int
    Tiles                 [][]string // [row][col] = brick_type_id or ""
    SpawnPoints           []SpawnPoint
}
```

Removed: `TerrainLayers`, `Width`, `Height`.

### terrain.go — NewTerrain()

Signature changes to `NewTerrain(mapCfg MapConfig) *Terrain`.

- Iterate `mapCfg.Tiles` (2D array)
- Cell with brick_type_id → set `Mask` = true for corresponding 16x16 pixel block
- Cell null → false (air)
- New `DestructibleMask []bool` — only cells with destructible brick types can be destroyed

### terrain.go — DestroyCircle()

Before setting `Mask[idx] = false`, check `DestructibleMask[idx]`. Non-destructible cells are skipped.

### Backward Compatibility

During transition: if `Tiles` is empty → fallback to current hardcoded math functions. Remove fallback after all maps are migrated to tilemap.

## Database Migration

### New table

```sql
CREATE TABLE config_brick_types (
    brick_type_id TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    destructible  BOOLEAN NOT NULL DEFAULT true
);
```

### Alter config_maps

```sql
ALTER TABLE config_maps
    ADD COLUMN grid_width  INT NOT NULL DEFAULT 100,
    ADD COLUMN grid_height INT NOT NULL DEFAULT 56,
    ADD COLUMN cell_size   INT NOT NULL DEFAULT 16,
    ADD COLUMN tiles       JSONB NOT NULL DEFAULT '[]';

ALTER TABLE config_maps
    DROP COLUMN IF EXISTS terrain_layers;
```

### Seed default brick types

```sql
INSERT INTO config_brick_types (brick_type_id, name, destructible) VALUES
    ('dirt', 'Dirt', true),
    ('rock', 'Rock', false),
    ('ice', 'Ice', true),
    ('lava', 'Lava', false),
    ('fragile', 'Fragile', true);
```
