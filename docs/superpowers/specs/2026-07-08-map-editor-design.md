# Map Editor Design (v2)

## Overview

Tilemap-based map editor integrated into the Admin Dashboard (port 9000). Maps use a grid of brick IDs. Each brick type defines a polygon border (not just a square) for organic terrain shapes. Maps and brick types are stored in DB — game server and client load them by ID.

## Data Model

### Brick Type Registry

Table `config_brick_types`:

| Column | Type | Description |
|--------|------|-------------|
| `brick_type_id` | SERIAL PK | Auto-increment integer: 1, 2, 3, ... |
| `name` | TEXT | Display name, e.g. "Grass Top", "Dirt", "Rock" |
| `image_id` | TEXT | Reference to sprite/image asset (can be empty initially) |
| `destructible` | BOOLEAN | Whether explosions can destroy this brick |
| `border` | JSONB | Polygon border definition (see below) |
| `color` | TEXT | Hex color for editor preview, e.g. "#8B4513" |

**Border format** — 4 edges defined by polyline points. Origin (0,0) = bottom-left of 16x16 cell:

```json
{
  "top":    [{"x":16,"y":16}, {"x":12,"y":14}, {"x":8,"y":16}, {"x":4,"y":13}, {"x":0,"y":16}],
  "right":  [{"x":16,"y":0}, {"x":16,"y":16}],
  "bottom": [{"x":0,"y":0}, {"x":16,"y":0}],
  "left":   [{"x":0,"y":16}, {"x":0,"y":0}]
}
```

A full square brick has straight-line borders on all 4 edges (default).

The closed polygon is formed by joining: bottom → right → top → left. This polygon defines both the visual shape and the collision mask (via scanline fill).

### Map Config

Table `config_maps`:

| Column | Type | Description |
|--------|------|-------------|
| `map_id` | TEXT PK | Unique identifier |
| `name` | TEXT | Display name |
| `grid_width` | INT | Number of horizontal cells |
| `grid_height` | INT | Number of vertical cells |
| `cell_size` | INT | 16 (fixed) |
| `default_wind_power_range` | JSONB | `[0.0, 4.0]` — float |
| `tiles` | JSONB | 2D array `[row][col]` = brick_type_id (int) or 0 (air) |
| `spawn_points` | JSONB | `[{x, y}, ...]` — pixel coords, float |

### Tiles Format

```json
[
  [0, 0, 0, 0, 0],
  [0, 3, 3, 3, 0],
  [0, 1, 1, 1, 0],
  [2, 2, 2, 2, 2]
]
```

`0` = air. Integer > 0 = `brick_type_id`. Lightweight — all visual/physics data resolved by looking up brick type.

### Room Creation

When creating a room, only `mapID` is needed. Server loads map tiles from DB (via `gamedata.Data.Maps[mapID]`), looks up brick borders from `gamedata.BrickTypes`, and generates collision mask.

## Admin Dashboard

### Brick Types Page (`/admin/brick-types`)

List page showing all brick types in DB with columns: ID, Name, Image ID, Destructible, Color, Preview (small polygon thumbnail).

**"+ Add Brick" button** → opens the Brick Editor page.

### Brick Editor Page (`/admin/brick-types/editor`)

A 16x16 pixel canvas editor for designing brick polygon borders.

**Layout:**
- **Center:** 16x16 grid canvas (zoomed large, ~400x400px display). Each pixel cell is clickable.
- **Left panel:** Tool selector, brick properties (name, image_id, destructible, color)
- **Bottom:** Preview of the polygon shape at actual size

**Editor workflow:**
1. Admin clicks on grid cells to place polygon vertices
2. Points are added to the current edge (top/right/bottom/left — selectable)
3. Polygon preview updates live
4. Set name, color, destructible flag
5. Image ID is a text field (image is loaded by client, not uploaded to server)
6. Save → stores to DB

**Tools:**
- **Add Point:** Click on grid to add vertex to selected edge
- **Move Point:** Drag existing vertex
- **Delete Point:** Click to remove vertex
- **Edge selector:** Choose which edge to edit (top/right/bottom/left)

**Default border:** New brick starts with a full square (straight lines on all 4 edges).

### Map Editor Page (`/admin/maps/editor?id=...`)

Same as current implementation. Changes:
- Brick palette shows brick types from DB (with color + name)
- Tiles are stored as integer IDs instead of strings
- Color-coded rectangles in editor (using brick's `color` field)

### DevTools Reset Data

`/devtools/reset-data` does NOT delete `config_brick_types` or `config_maps`. These contain hand-crafted content. Currently already safe — `ResetAllData` only deletes player-related tables.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| **Brick Types** | | |
| GET | `/admin/brick-types` | List page |
| GET | `/admin/brick-types/editor` | Brick polygon editor page |
| POST | `/admin/brick-types/save` | Save brick type (form or JSON) |
| POST | `/admin/brick-types/delete` | Delete brick type |
| GET | `/admin/api/brick-types` | List all brick types (JSON) |
| **Map Editor** | | |
| GET | `/admin/maps/editor` | Map editor page |
| GET | `/admin/api/maps/tiles` | Get map tiles + config (JSON) |
| PUT | `/admin/api/maps/tiles` | Save map tiles |
| GET | `/admin/api/maps/export` | Export map as YAML |

## Game Server Impact

### BrickTypeConfig Struct

```go
type BrickTypeConfig struct {
    BrickTypeID  int
    Name         string
    ImageID      string
    Destructible bool
    Border       BrickBorder
    Color        string
}

type BorderPoint struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}

type BrickBorder struct {
    Top    []BorderPoint `json:"top"`
    Right  []BorderPoint `json:"right"`
    Bottom []BorderPoint `json:"bottom"`
    Left   []BorderPoint `json:"left"`
}
```

### MapConfig Struct

```go
type MapConfig struct {
    MapID                 string
    Name                  string
    GridWidth             int
    GridHeight            int
    CellSize              int
    DefaultWindPowerRange []float64
    Tiles                 [][]int      // 0 = air, >0 = brick_type_id
    SpawnPoints           []SpawnPoint
}
```

### terrain.go — NewTerrain()

For each cell with a brick_type_id > 0:
1. Look up `BrickTypes[id]` to get `BrickBorder`
2. Build closed polygon from border points: bottom → right → top → left
3. **Scanline fill** the polygon into the 16x16 pixel area of the cell
4. Set `Mask[px] = true` and `DestructibleMask[px] = destructible` for filled pixels

For air cells (0): skip.

Fallback: if `Tiles` is empty, use legacy hardcoded terrain.

### terrain.go — DestroyCircle()

No change from current — still pixel-based. Check `DestructibleMask` before destroying.

### Client Rendering

1. Load all brick types (border + image_id) once at game start
2. For each cell: draw image sprite clipped to polygon shape
3. Border lines drawn only on edges exposed to air (check 4 neighbors)
4. After destruction: render image with mask clipping (destroyed pixels become transparent)
5. Post-destruction border: simple crater edge or marching squares on mask

## Database Migration

### Alter `config_brick_types`

```sql
-- Change PK from TEXT to SERIAL
-- If migrating from existing TEXT PK, need to recreate table
DROP TABLE IF EXISTS config_brick_types;
CREATE TABLE config_brick_types (
    brick_type_id SERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    image_id      TEXT NOT NULL DEFAULT '',
    destructible  BOOLEAN NOT NULL DEFAULT true,
    border        JSONB NOT NULL DEFAULT '{"top":[{"x":0,"y":16},{"x":16,"y":16}],"right":[{"x":16,"y":16},{"x":16,"y":0}],"bottom":[{"x":16,"y":0},{"x":0,"y":0}],"left":[{"x":0,"y":0},{"x":0,"y":16}]}',
    color         TEXT NOT NULL DEFAULT '#8B4513',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Update `config_maps.tiles`

Change tiles from `[][]string` to `[][]int`. Existing data with string IDs needs migration or reset.
