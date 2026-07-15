# Map Rank Restriction & Editor Unification

## Overview

Three changes to the map system:

1. **Rank-based map unlocking** - Each map has a `min_rank_tier`. Matchmaker only selects maps the highest-ranked player in the match has unlocked.
2. **Unified map editor** - Remove the separate `/maps/edit` form page. All map editing (metadata + tiles + spawn points) happens in the canvas-based `/maps/editor` page.
3. **Accurate brick rendering** - Map editor renders tiles using actual polygon border shapes from `config_brick_types`, not just solid color squares.

## 1. Database

### Migration

Add column to `config_maps`:

```sql
ALTER TABLE config_maps ADD COLUMN min_rank_tier VARCHAR(20) NOT NULL DEFAULT 'bronze';
```

Valid values: `bronze`, `silver`, `gold`, `platinum`, `diamond`, `master`.

No new tables needed. No changes to `config_brick_types`.

## 2. Backend (Go)

### GameData

`internal/game/gamedata/loader.go`:

- Add `MinRankTier string` field to `MapConfig` struct (`yaml:"minRankTier"` / DB column `min_rank_tier`)
- Load from DB in `LoadGameDataFromDB` (add `min_rank_tier` to SELECT query for `config_maps`)

### Admin Handlers

**Remove:**
- Remove `handleConfigEdit("maps")` route (`GET /maps/edit`)
- Remove maps from `handleConfigSave` route (`POST /maps/save` for form-based save)
- Remove maps from the config edit form field definitions

**Modify `/maps/editor` handler** (`handleMapEditor`):
- Load all map fields (name, grid_width, grid_height, cell_size, default_wind_power_range, min_rank_tier, description, tiles, spawn_points) and pass to template
- For new map (no `id` param): render empty editor with defaults

**Modify `GET /api/maps/tiles`** → return all fields including `name`, `min_rank_tier`, `description`, `default_wind_power_range`.

**Modify `PUT /api/maps/tiles`** → **rename to `PUT /api/maps/save`**:
- Accept full payload:
  ```json
  {
    "mapId": "string (only for create)",
    "name": "string",
    "gridWidth": 100,
    "gridHeight": 56,
    "cellSize": 16,
    "defaultWindPowerRange": [0.0, 4.0],
    "minRankTier": "bronze",
    "description": "string",
    "tiles": [[0, 1, 2], [1, 0, 3]],
    "spawnPoints": [{"x": 100, "y": 200}]
  }
  ```
- If creating new map (POST or no existing record): INSERT with all fields
- If updating: UPDATE all fields
- Return `{"ok": true}`

**Modify `/maps` list page:**
- Add "Min Rank" column to the maps table, displaying tier name (e.g., "Bronze", "Silver")
- Read-only in list view; edit via editor page

### Admin Repository

- `UpsertMap`: include `min_rank_tier` in INSERT/UPDATE
- `GetMap` / `GetMaps`: include `min_rank_tier` in SELECT
- `GetMapTiles`: return `min_rank_tier`, `name`, `description`, `default_wind_power_range` alongside tiles/spawn data
- `SaveMapTiles` → `SaveMap`: update all fields, not just tiles + spawn_points

### Matchmaker

`internal/game/matchmaker/matchmaker.go`:

**Add tier ordering helper:**
```go
var tierOrder = map[string]int{
    "bronze": 0, "silver": 1, "gold": 2,
    "platinum": 3, "diamond": 4, "master": 5,
}

func tierIndex(tier string) int {
    if idx, ok := tierOrder[tier]; ok {
        return idx
    }
    return 0
}
```

**Replace `randomMap()`** with `randomMapForTier(playerTier string)`:
- Filter `m.mapIDs` → only maps where `tierIndex(map.MinRankTier) <= tierIndex(playerTier)`
- Random pick from filtered list
- Fallback: if filtered list empty, use all maps

**Store MinRankTier in matchmaker state:**
- Matchmaker needs access to `MapConfig.MinRankTier` for filtering
- Store `map[string]string` mapping mapID → minRankTier alongside `mapIDs`
- Update when game data reloads

**Modify `tick()`:**
- When creating `MatchResult`, compute `maxRating = max(entry1.Rating, entry2.Rating)`
- Convert to tier via existing `ratingToTier(maxRating)`
- Call `randomMapForTier(tier)` instead of `randomMap()`

`ratingToTier` function (already exists in `room/hub.go`):
- Duplicate in matchmaker package to avoid circular dependencies (hub imports matchmaker, so matchmaker cannot import hub)

## 3. Frontend (Map Editor Page)

### Template: `map_editor.html`

**Layout restructure:**

```
+------------------------------------------------------------------+
| Top Bar: [Map ID (input/readonly)] [Map Name input]    [SaveBtn] |
+------------------------------------------------------------------+
| Left: Canvas (paint/erase/fill/spawn/select tools)  | Right:     |
|                                                      | - Settings |
|   Grid + painted tiles with polygon shapes           |   GridW/H  |
|   Spawn points as golden circles                     |   CellSize |
|                                                      |   WindRange|
|                                                      |   MinRank  |
|                                                      |   Desc     |
|                                                      | - Bricks   |
|                                                      |   palette  |
|                                                      | - Spawns   |
|                                                      |   list     |
+------------------------------------------------------------------+
```

**Map Settings section (right panel):**
- Grid Width: number input
- Grid Height: number input
- Cell Size: number input
- Wind Power Min/Max: two number inputs (not JSON textarea)
- Min Rank Tier: dropdown select with options: Bronze, Silver, Gold, Platinum, Diamond, Master
- Description: textarea

**Map ID handling:**
- New map: editable text input for map_id
- Existing map: readonly display (map_id is PK, can't change)

### JavaScript: `map_editor.js`

**Brick polygon rendering:**

Current: fills entire cell rectangle with solid color.

New: for each tile with `brickTypeId > 0`:
1. Get brick type's `border` data (top/right/bottom/left polylines)
2. Build closed polygon path from border points, scaled by `cellSize` and offset by cell position (`col * cellSize`, `row * cellSize`)
3. Fill polygon with brick `color`, stroke with darker outline
4. Area outside polygon within the cell remains empty (shows grid background)

**Polygon rendering function:**
```javascript
function drawBrickPolygon(ctx, border, cellSize, offsetX, offsetY, color) {
    // Chain border edges: bottom → right → top → left to form closed path
    // Scale points: x * (cellSize/16), y * (cellSize/16) (border defined in 16x16 space)
    // Translate by (offsetX, offsetY)
    // ctx.fill() + ctx.stroke()
}
```

**Brick palette:**
- Each brick in palette shows small polygon preview (like brick_types list page already does)
- Loaded from `/api/brick-types` on page load
- No caching — always fresh from DB

**Load existing map:**
- `GET /api/maps/tiles?id=<mapId>` returns all fields
- Populate form inputs (name, grid size, cell size, wind range, min_rank_tier, description)
- Parse `tiles` 2D array, render each cell with polygon
- Parse `spawnPoints` array, render on canvas + populate list

**Save:**
- Collect all form values + tiles array + spawn points
- `PUT /api/maps/save?id=<mapId>` with full JSON body
- For new map: include `mapId` from input field

**Spawn points fix:**
- Current bug: spawn points show as `(undefined, undefined)` because the JSON uses uppercase `X`/`Y` but JS expects lowercase `x`/`y`
- Fix: normalize to lowercase `x`/`y` on load, and always save as lowercase

## 4. Maps List Page

### Template: `config_list.html` (maps section)

Add column "Min Rank" after existing columns. Display capitalized tier name (e.g., "Silver").

Remove "Edit" link that pointed to `/maps/edit`. The "Editor" link (pointing to `/maps/editor`) becomes the only edit action. Rename button label from "Editor" to "Edit".

"New Map" button → links to `/maps/editor` (no id param) → opens blank editor.

## 5. Example: Map Selection by Rank

Given maps:
| Map | min_rank_tier |
|-----|---------------|
| grassland_valley | bronze |
| frozen_peak | silver |
| steel_base | gold |

Match scenarios:
- Team1 bronze + Team2 bronze → max tier = bronze → pool: [grassland_valley]
- Team1 silver + Team2 bronze → max tier = silver → pool: [grassland_valley, frozen_peak]
- Team1 gold + Team2 silver → max tier = gold → pool: [grassland_valley, frozen_peak, steel_base]

## 6. Files to Modify

| File | Change |
|------|--------|
| `migrations/new_migration.sql` | Add `min_rank_tier` column |
| `internal/game/gamedata/loader.go` | Add `MinRankTier` to `MapConfig`, load from DB |
| `internal/admin/handlers_config.go` | Remove maps edit route, add min_rank_tier to list display |
| `internal/admin/handlers_map_editor.go` | Modify editor handler to load all fields, modify save to accept all fields |
| `internal/admin/repository.go` | Update map queries for min_rank_tier, expand SaveMapTiles → SaveMap |
| `internal/admin/server.go` | Remove `/maps/edit` route, update `/maps/save` route |
| `internal/admin/templates/map_editor.html` | Rebuild: add settings form, polygon rendering |
| `internal/admin/static/map_editor.js` | Polygon rendering, form handling, spawn point fix |
| `internal/admin/templates/config_list.html` | Add Min Rank column for maps, remove Edit link |
| `internal/game/matchmaker/matchmaker.go` | `randomMapForTier()`, tier ordering, store map tier data |
| `cmd/game/main.go` | Pass map configs (with MinRankTier) to matchmaker |
| `configs/maps.yaml` | Add `minRankTier` field to existing maps |
