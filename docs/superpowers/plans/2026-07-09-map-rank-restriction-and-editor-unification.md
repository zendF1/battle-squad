# Map Rank Restriction & Editor Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add rank-based map unlocking in matchmaking, unify the map edit/editor pages into a single canvas-based editor with full metadata editing, and render bricks using actual polygon borders from DB.

**Architecture:** Migration adds `min_rank_tier` column. Admin backend merges the two map editing endpoints into one. Map editor JS is rewritten to include settings form, polygon brick rendering, and proper spawn point loading. Matchmaker filters maps by tier before random selection.

**Tech Stack:** Go (chi router, pgx), PostgreSQL, vanilla JavaScript (Canvas API), HTML templates

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `migrations/009_map_rank_tier.up.sql` | Create | Add `min_rank_tier` column |
| `migrations/009_map_rank_tier.down.sql` | Create | Remove `min_rank_tier` column |
| `internal/admin/repository.go` | Modify | Add `MinRankTier` to `ConfigMap`, `MapTilesData`; expand `SaveMapTiles` → `SaveMapFull` |
| `internal/admin/handlers_map_editor.go` | Modify | Expand editor handler for new maps; expand save to accept all fields |
| `internal/admin/handlers_config.go` | Modify | Remove maps edit/save cases; keep list/delete |
| `internal/admin/server.go` | Modify | Remove `/maps/edit` and `/maps/save` routes |
| `internal/admin/templates/config_list.html` | Modify | Add Min Rank column, remove Edit button, point Add New to editor |
| `internal/admin/templates/map_editor.html` | Rewrite | Add settings form, support new map creation |
| `internal/admin/static/map_editor.js` | Rewrite | Polygon rendering, form handling, spawn fix, full save |
| `internal/game/gamedata/loader.go` | Modify | Add `MinRankTier` to `MapConfig`, load from DB |
| `internal/game/matchmaker/matchmaker.go` | Modify | Add tier-based map filtering |
| `cmd/game/main.go` | Modify | Pass map configs with tier info to matchmaker |
| `configs/maps.yaml` | Modify | Add `minRankTier` field |

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/009_map_rank_tier.up.sql`
- Create: `migrations/009_map_rank_tier.down.sql`

- [ ] **Step 1: Create up migration**

```sql
-- migrations/009_map_rank_tier.up.sql
ALTER TABLE config_maps
    ADD COLUMN IF NOT EXISTS min_rank_tier VARCHAR(20) NOT NULL DEFAULT 'bronze';
```

- [ ] **Step 2: Create down migration**

```sql
-- migrations/009_map_rank_tier.down.sql
ALTER TABLE config_maps
    DROP COLUMN IF EXISTS min_rank_tier;
```

- [ ] **Step 3: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration 009 applied successfully

- [ ] **Step 4: Commit**

```bash
git add migrations/009_map_rank_tier.up.sql migrations/009_map_rank_tier.down.sql
git commit -m "feat: add min_rank_tier column to config_maps"
```

---

### Task 2: Repository — Add MinRankTier to ConfigMap and expand save

**Files:**
- Modify: `internal/admin/repository.go:474-690`

- [ ] **Step 1: Add MinRankTier field to ConfigMap struct**

At `repository.go:474-488`, add `MinRankTier` field:

```go
type ConfigMap struct {
	MapID                 string
	Name                  string
	Width                 int
	Height                int
	GridWidth             int
	GridHeight            int
	CellSize              int
	DefaultWindPowerRange json.RawMessage
	TerrainLayers         json.RawMessage
	SpawnPoints           json.RawMessage
	Tiles                 json.RawMessage
	Description           string
	MinRankTier           string
}
```

- [ ] **Step 2: Update GetMaps to include min_rank_tier**

At `repository.go:491-513`, add `min_rank_tier` to the SELECT and Scan:

```go
func (r *Repository) GetMaps(ctx context.Context) ([]ConfigMap, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT map_id, name, width, height, grid_width, grid_height, cell_size,
		        default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier
		 FROM config_maps ORDER BY map_id`)
	if err != nil {
		return nil, fmt.Errorf("query maps: %w", err)
	}
	defer rows.Close()

	var maps []ConfigMap
	for rows.Next() {
		var m ConfigMap
		if err := rows.Scan(&m.MapID, &m.Name, &m.Width, &m.Height,
			&m.GridWidth, &m.GridHeight, &m.CellSize,
			&m.DefaultWindPowerRange, &m.TerrainLayers, &m.SpawnPoints,
			&m.Tiles, &m.Description, &m.MinRankTier); err != nil {
			return nil, fmt.Errorf("scan map: %w", err)
		}
		maps = append(maps, m)
	}
	return maps, rows.Err()
}
```

- [ ] **Step 3: Update GetMap to include min_rank_tier**

At `repository.go:516-530`:

```go
func (r *Repository) GetMap(ctx context.Context, id string) (*ConfigMap, error) {
	var m ConfigMap
	err := r.db.Pool.QueryRow(ctx,
		`SELECT map_id, name, width, height, grid_width, grid_height, cell_size,
		        default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier
		 FROM config_maps WHERE map_id = $1`, id).
		Scan(&m.MapID, &m.Name, &m.Width, &m.Height,
			&m.GridWidth, &m.GridHeight, &m.CellSize,
			&m.DefaultWindPowerRange, &m.TerrainLayers, &m.SpawnPoints,
			&m.Tiles, &m.Description, &m.MinRankTier)
	if err != nil {
		return nil, fmt.Errorf("get map %s: %w", id, err)
	}
	return &m, nil
}
```

- [ ] **Step 4: Update UpsertMap to include min_rank_tier**

At `repository.go:533-553`:

```go
func (r *Repository) UpsertMap(ctx context.Context, m *ConfigMap) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_maps
		 (map_id, name, width, height, grid_width, grid_height, cell_size,
		  default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, CURRENT_TIMESTAMP)
		 ON CONFLICT (map_id) DO UPDATE SET
		   name=EXCLUDED.name, width=EXCLUDED.width, height=EXCLUDED.height,
		   grid_width=EXCLUDED.grid_width, grid_height=EXCLUDED.grid_height,
		   cell_size=EXCLUDED.cell_size,
		   default_wind_power_range=EXCLUDED.default_wind_power_range,
		   terrain_layers=EXCLUDED.terrain_layers, spawn_points=EXCLUDED.spawn_points,
		   tiles=EXCLUDED.tiles,
		   description=EXCLUDED.description, min_rank_tier=EXCLUDED.min_rank_tier,
		   updated_at=CURRENT_TIMESTAMP`,
		m.MapID, m.Name, m.Width, m.Height, m.GridWidth, m.GridHeight, m.CellSize,
		m.DefaultWindPowerRange, m.TerrainLayers, m.SpawnPoints, m.Tiles, m.Description, m.MinRankTier)
	if err != nil {
		return fmt.Errorf("upsert map %s: %w", m.MapID, err)
	}
	return nil
}
```

- [ ] **Step 5: Add MinRankTier and Description to MapTilesData, expand GetMapTiles**

At `repository.go:654-678`, add fields and update query:

```go
type MapTilesData struct {
	MapID                 string          `json:"mapId"`
	Name                  string          `json:"name"`
	GridWidth             int             `json:"gridWidth"`
	GridHeight            int             `json:"gridHeight"`
	CellSize              int             `json:"cellSize"`
	DefaultWindPowerRange json.RawMessage `json:"defaultWindPowerRange"`
	Tiles                 json.RawMessage `json:"tiles"`
	SpawnPoints           json.RawMessage `json:"spawnPoints"`
	MinRankTier           string          `json:"minRankTier"`
	Description           string          `json:"description"`
}

func (r *Repository) GetMapTiles(ctx context.Context, id string) (*MapTilesData, error) {
	var d MapTilesData
	err := r.db.Pool.QueryRow(ctx,
		`SELECT map_id, name, grid_width, grid_height, cell_size,
		        default_wind_power_range, tiles, spawn_points, min_rank_tier, description
		 FROM config_maps WHERE map_id = $1`, id).
		Scan(&d.MapID, &d.Name, &d.GridWidth, &d.GridHeight, &d.CellSize,
			&d.DefaultWindPowerRange, &d.Tiles, &d.SpawnPoints, &d.MinRankTier, &d.Description)
	if err != nil {
		return nil, fmt.Errorf("get map tiles %s: %w", id, err)
	}
	return &d, nil
}
```

- [ ] **Step 6: Replace SaveMapTiles with SaveMapFull**

Replace `repository.go:681-690` with:

```go
func (r *Repository) SaveMapFull(ctx context.Context, m *ConfigMap) error {
	return r.UpsertMap(ctx, m)
}
```

- [ ] **Step 7: Verify build**

Run: `go build ./internal/admin/...`
Expected: Build succeeds

- [ ] **Step 8: Commit**

```bash
git add internal/admin/repository.go
git commit -m "feat: add MinRankTier to ConfigMap and expand map queries"
```

---

### Task 3: Admin Handlers — Remove maps edit route, expand editor handler

**Files:**
- Modify: `internal/admin/server.go:83-86`
- Modify: `internal/admin/handlers_config.go:177-198,313-348`
- Modify: `internal/admin/handlers_map_editor.go:147-217`

- [ ] **Step 1: Remove maps edit/save routes from server.go**

At `server.go:83-86`, remove the `/maps/edit` and `/maps/save` lines. Keep `/maps` list and `/maps/delete`. Change the API route from `/api/maps/tiles` PUT to `/api/maps/save`:

```go
	r.Get("/maps", s.handleConfigList("maps"))
	r.Post("/maps/delete", s.handleConfigDelete("maps"))
```

Update map editor routes at lines 95-97:

```go
	// Map Editor
	r.Get("/maps/editor", s.handleMapEditor)
	r.Get("/api/maps/tiles", s.handleMapTilesGet)
	r.Put("/api/maps/save", s.handleMapSave)
	r.Get("/api/maps/export", s.handleMapExport)
	r.Get("/api/brick-types", s.handleBrickTypesAPI)
```

- [ ] **Step 2: Remove maps cases from handleConfigEdit and handleConfigSave in handlers_config.go**

Remove the `case "maps":` block at lines 177-198 from `handleConfigEdit`.

Remove the `case "maps":` block at lines 313-348 from `handleConfigSave`.

- [ ] **Step 3: Expand handleMapEditor to support new maps (no id param)**

Replace `handlers_map_editor.go:147-174`:

```go
func (s *Server) handleMapEditor(w http.ResponseWriter, r *http.Request) {
	mapID := r.URL.Query().Get("id")
	ctx := r.Context()

	var mapName string
	var isNew bool
	if mapID == "" {
		isNew = true
		mapName = "New Map"
	} else {
		m, err := s.repo.GetMap(ctx, mapID)
		if err != nil {
			http.Redirect(w, r, "/maps?error=Map+not+found", http.StatusSeeOther)
			return
		}
		mapName = m.Name
	}

	brickTypes, err := s.repo.GetBrickTypes(ctx)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to load brick types for editor")
	}

	brickTypesJSON, _ := json.Marshal(brickTypes)

	s.render(w, "map_editor", map[string]interface{}{
		"ActivePage":     "maps",
		"MapID":          mapID,
		"MapName":        mapName,
		"IsNew":          isNew,
		"BrickTypesJSON": string(brickTypesJSON),
	})
}
```

- [ ] **Step 4: Replace handleMapTilesSave with handleMapSave**

Replace `handlers_map_editor.go:193-217` with a new handler that accepts all fields:

```go
func (s *Server) handleMapSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MapID                 string          `json:"mapId"`
		Name                  string          `json:"name"`
		GridWidth             int             `json:"gridWidth"`
		GridHeight            int             `json:"gridHeight"`
		CellSize              int             `json:"cellSize"`
		DefaultWindPowerRange json.RawMessage `json:"defaultWindPowerRange"`
		MinRankTier           string          `json:"minRankTier"`
		Description           string          `json:"description"`
		Tiles                 json.RawMessage `json:"tiles"`
		SpawnPoints           json.RawMessage `json:"spawnPoints"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// For existing map, id comes from query param; for new map, from body
	mapID := r.URL.Query().Get("id")
	if mapID == "" {
		mapID = strings.TrimSpace(body.MapID)
	}
	if mapID == "" {
		http.Error(w, `{"error":"map ID is required"}`, http.StatusBadRequest)
		return
	}

	gridWidth := body.GridWidth
	if gridWidth == 0 {
		gridWidth = 100
	}
	gridHeight := body.GridHeight
	if gridHeight == 0 {
		gridHeight = 56
	}
	cellSize := body.CellSize
	if cellSize == 0 {
		cellSize = 16
	}
	minRankTier := body.MinRankTier
	if minRankTier == "" {
		minRankTier = "bronze"
	}

	windRange := body.DefaultWindPowerRange
	if len(windRange) == 0 {
		windRange = json.RawMessage(`[0,4]`)
	}
	tiles := body.Tiles
	if len(tiles) == 0 {
		tiles = json.RawMessage(`[]`)
	}
	spawnPoints := body.SpawnPoints
	if len(spawnPoints) == 0 {
		spawnPoints = json.RawMessage(`[]`)
	}

	m := &ConfigMap{
		MapID:                 mapID,
		Name:                  body.Name,
		Width:                 gridWidth * cellSize,
		Height:                gridHeight * cellSize,
		GridWidth:             gridWidth,
		GridHeight:            gridHeight,
		CellSize:              cellSize,
		DefaultWindPowerRange: windRange,
		TerrainLayers:         json.RawMessage(`[]`),
		SpawnPoints:           spawnPoints,
		Tiles:                 tiles,
		Description:           body.Description,
		MinRankTier:           minRankTier,
	}

	if err := s.repo.UpsertMap(r.Context(), m); err != nil {
		observability.Log.Error().Err(err).Str("mapId", mapID).Msg("failed to save map")
		http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "mapId": mapID})
}
```

- [ ] **Step 5: Verify build**

Run: `go build ./internal/admin/...`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/admin/server.go internal/admin/handlers_config.go internal/admin/handlers_map_editor.go
git commit -m "feat: remove maps edit route, expand editor to handle all map fields"
```

---

### Task 4: Config List Template — Add Min Rank column, fix actions

**Files:**
- Modify: `internal/admin/templates/config_list.html:74-90`

- [ ] **Step 1: Update maps section in config_list.html**

Replace lines 74-90 with:

```html
{{else if eq .ConfigType "maps"}}
<thead><tr><th>ID</th><th>Name</th><th>Grid</th><th>Cell Size</th><th>Min Rank</th><th>Description</th><th>Actions</th></tr></thead>
<tbody>
{{range .Items}}
<tr>
    <td>{{.MapID}}</td><td>{{.Name}}</td><td>{{.GridWidth}}x{{.GridHeight}}</td><td>{{.CellSize}}px</td><td style="text-transform:capitalize">{{.MinRankTier}}</td><td>{{.Description}}</td>
    <td>
        <a href="/maps/editor?id={{.MapID}}" class="btn btn-primary btn-sm">Edit</a>
        <form method="POST" action="/maps/delete" style="display:inline" onsubmit="return confirm('Delete this map?')">
            <input type="hidden" name="id" value="{{.MapID}}">
            <button type="submit" class="btn btn-danger btn-sm">Delete</button>
        </form>
    </td>
</tr>
{{end}}
</tbody>
```

- [ ] **Step 2: Update the Add New link for maps**

At line 4, the Add New button uses a conditional href. Add a maps case so it points to `/maps/editor` instead of `/maps/edit`:

```html
<a href="{{if eq .ConfigType "characters"}}/characters/detail{{else if eq .ConfigType "maps"}}/maps/editor{{else}}/{{.ConfigType}}/edit{{end}}" class="btn btn-primary">+ Add New</a>
```

- [ ] **Step 3: Commit**

```bash
git add internal/admin/templates/config_list.html
git commit -m "feat: add Min Rank column to maps list, point actions to editor"
```

---

### Task 5: Map Editor Template — Rebuild with settings form

**Files:**
- Rewrite: `internal/admin/templates/map_editor.html`

- [ ] **Step 1: Rewrite map_editor.html**

```html
{{define "content"}}
<style>
.editor-container { display: flex; gap: 12px; height: calc(100vh - 90px); }
.editor-toolbar { width: 180px; flex-shrink: 0; }
.editor-canvas-wrap { flex: 1; overflow: hidden; position: relative; background: #1a1a2e; border-radius: 8px; }
.editor-canvas-wrap canvas { display: block; cursor: crosshair; }
.editor-properties { width: 260px; flex-shrink: 0; overflow-y: auto; }
.tool-btn { display: block; width: 100%; padding: 8px 12px; margin-bottom: 4px; border: 1px solid #ccc; border-radius: 4px; background: #fff; cursor: pointer; text-align: left; font-size: 13px; }
.tool-btn.active { background: #4a6cf7; color: #fff; border-color: #4a6cf7; }
.brick-btn { display: inline-block; width: 36px; height: 36px; margin: 2px; border: 2px solid #ccc; border-radius: 4px; cursor: pointer; position: relative; overflow: hidden; }
.brick-btn.active { border-color: #4a6cf7; box-shadow: 0 0 0 2px #4a6cf7; }
.brick-btn canvas { display: block; width: 100%; height: 100%; }
.editor-top-bar { display: flex; gap: 8px; margin-bottom: 12px; align-items: center; flex-wrap: wrap; }
.editor-top-bar h1 { font-size: 18px; margin: 0; }
.coords-display { position: absolute; bottom: 8px; left: 8px; background: rgba(0,0,0,0.7); color: #fff; padding: 4px 8px; border-radius: 4px; font-size: 12px; font-family: monospace; }
.spawn-list { max-height: 200px; overflow-y: auto; }
.spawn-item { display: flex; justify-content: space-between; align-items: center; padding: 4px 0; font-size: 12px; border-bottom: 1px solid #eee; }
.props-section { margin-bottom: 16px; }
.props-section h3 { font-size: 14px; margin: 0 0 8px; }
.form-group { margin-bottom: 8px; }
.form-group label { display: block; font-size: 12px; font-weight: 600; margin-bottom: 2px; }
.form-group input, .form-group select, .form-group textarea { width: 100%; padding: 4px 8px; border: 1px solid #ccc; border-radius: 4px; font-size: 13px; box-sizing: border-box; }
.form-group textarea { resize: vertical; min-height: 50px; }
</style>

<div class="editor-top-bar">
    {{if .IsNew}}
    <div class="form-group" style="margin:0">
        <input type="text" id="propMapId" placeholder="map_id (e.g. desert_ruins)" style="width:200px">
    </div>
    {{else}}
    <h1>{{.MapName}}</h1>
    {{end}}
    <div style="flex:1"></div>
    <button class="btn btn-primary" onclick="editor.save()">Save</button>
    <button class="btn btn-success" onclick="editor.exportYAML()">Export YAML</button>
    <button class="btn btn-warning" onclick="editor.undo()">Undo</button>
    <button class="btn btn-warning" onclick="editor.redo()">Redo</button>
    <label style="font-size:13px"><input type="checkbox" id="gridToggle" checked onchange="editor.toggleGrid(this.checked)"> Grid</label>
</div>

<div class="editor-container">
    <div class="editor-toolbar card">
        <h3 style="font-size:14px;margin-bottom:8px">Tools</h3>
        <button class="tool-btn active" data-tool="paint" onclick="editor.setTool('paint',this)">Paint</button>
        <button class="tool-btn" data-tool="erase" onclick="editor.setTool('erase',this)">Erase</button>
        <button class="tool-btn" data-tool="fill" onclick="editor.setTool('fill',this)">Fill</button>
        <button class="tool-btn" data-tool="select" onclick="editor.setTool('select',this)">Select/Move</button>
        <button class="tool-btn" data-tool="spawn" onclick="editor.setTool('spawn',this)">Spawn Point</button>
        <h3 style="font-size:14px;margin:12px 0 8px">Bricks</h3>
        <div id="brickPalette"></div>
    </div>

    <div class="editor-canvas-wrap" id="canvasWrap">
        <canvas id="editorCanvas"></canvas>
        <div class="coords-display" id="coordsDisplay">Cell: 0,0 | Px: 0,0</div>
    </div>

    <div class="editor-properties card">
        <div class="props-section">
            <h3>Map Settings</h3>
            <div class="form-group">
                <label>Name</label>
                <input type="text" id="propName" value="">
            </div>
            <div class="form-group">
                <label>Grid Width</label>
                <input type="number" id="propGridWidth" value="100" min="1">
            </div>
            <div class="form-group">
                <label>Grid Height</label>
                <input type="number" id="propGridHeight" value="56" min="1">
            </div>
            <div class="form-group">
                <label>Cell Size</label>
                <input type="number" id="propCellSize" value="16" min="1">
            </div>
            <div class="form-group">
                <label>Wind Power Min</label>
                <input type="number" id="propWindMin" value="0" step="0.1" min="0">
            </div>
            <div class="form-group">
                <label>Wind Power Max</label>
                <input type="number" id="propWindMax" value="4" step="0.1" min="0">
            </div>
            <div class="form-group">
                <label>Min Rank Tier</label>
                <select id="propMinRankTier">
                    <option value="bronze">Bronze</option>
                    <option value="silver">Silver</option>
                    <option value="gold">Gold</option>
                    <option value="platinum">Platinum</option>
                    <option value="diamond">Diamond</option>
                    <option value="master">Master</option>
                </select>
            </div>
            <div class="form-group">
                <label>Description</label>
                <textarea id="propDescription"></textarea>
            </div>
        </div>
        <div class="props-section">
            <h3>Spawn Points</h3>
            <div class="spawn-list" id="spawnList"></div>
        </div>
    </div>
</div>

<script>
var MAP_ID = '{{.MapID}}';
var IS_NEW = {{.IsNew}};
var BRICK_TYPES = JSON.parse('{{safeJS .BrickTypesJSON}}');
</script>
<script src="/static/map_editor.js"></script>
{{end}}
```

- [ ] **Step 2: Commit**

```bash
git add internal/admin/templates/map_editor.html
git commit -m "feat: rebuild map editor template with settings form and rank tier"
```

---

### Task 6: Map Editor JavaScript — Polygon rendering, form handling, spawn fix

**Files:**
- Rewrite: `internal/admin/static/map_editor.js`

- [ ] **Step 1: Rewrite map_editor.js**

```javascript
// Map Editor - Canvas-based tilemap editor with polygon brick rendering
(function() {
    'use strict';

    // Build brick lookup maps
    var BRICK_COLORS = {};
    var BRICK_BORDERS = {};
    BRICK_TYPES.forEach(function(bt) {
        BRICK_COLORS[bt.BrickTypeID] = bt.Color || '#888';
        if (bt.Border) {
            var border = typeof bt.Border === 'string' ? JSON.parse(bt.Border) : bt.Border;
            if (border && border.top && border.top.length > 0) {
                BRICK_BORDERS[bt.BrickTypeID] = border;
            }
        }
    });

    // Build a closed polygon path from border edges.
    // Border is defined in a 16x16 coordinate space.
    // Returns array of {x, y} points scaled to cellSize and offset.
    function buildPolygonPoints(border, cellSize, offsetX, offsetY) {
        var scale = cellSize / 16;
        var points = [];
        var edges = ['bottom', 'right', 'top', 'left'];
        for (var i = 0; i < edges.length; i++) {
            var edge = border[edges[i]];
            if (!edge) continue;
            for (var j = 0; j < edge.length; j++) {
                // Skip last point of each edge (it's the first point of next edge)
                if (j === edge.length - 1 && i < edges.length - 1) continue;
                points.push({
                    x: offsetX + edge[j].x * scale,
                    y: offsetY + (16 - edge[j].y) * scale  // flip Y: border uses bottom-left origin
                });
            }
        }
        return points;
    }

    function drawBrickPolygon(ctx, border, cellSize, offsetX, offsetY, color) {
        var points = buildPolygonPoints(border, cellSize, offsetX, offsetY);
        if (points.length < 3) return;

        ctx.beginPath();
        ctx.moveTo(points[0].x, points[0].y);
        for (var i = 1; i < points.length; i++) {
            ctx.lineTo(points[i].x, points[i].y);
        }
        ctx.closePath();
        ctx.fillStyle = color;
        ctx.fill();
        ctx.strokeStyle = darkenColor(color, 0.3);
        ctx.lineWidth = 0.5;
        ctx.stroke();
    }

    function darkenColor(hex, amount) {
        hex = hex.replace('#', '');
        if (hex.length === 3) hex = hex[0]+hex[0]+hex[1]+hex[1]+hex[2]+hex[2];
        var r = Math.max(0, parseInt(hex.substr(0, 2), 16) * (1 - amount));
        var g = Math.max(0, parseInt(hex.substr(2, 2), 16) * (1 - amount));
        var b = Math.max(0, parseInt(hex.substr(4, 2), 16) * (1 - amount));
        return 'rgb(' + Math.round(r) + ',' + Math.round(g) + ',' + Math.round(b) + ')';
    }

    // Draw a small polygon preview for the brick palette
    function drawPalettePreview(canvas, border, color) {
        var ctx = canvas.getContext('2d');
        var size = canvas.width;
        canvas.height = size;
        ctx.clearRect(0, 0, size, size);

        if (border) {
            drawBrickPolygon(ctx, border, size, 0, 0, color);
        } else {
            ctx.fillStyle = color;
            ctx.fillRect(0, 0, size, size);
        }
    }

    function MapEditor(canvasId, wrapId) {
        this.canvas = document.getElementById(canvasId);
        this.ctx = this.canvas.getContext('2d');
        this.wrap = document.getElementById(wrapId);

        // Map data
        this.gridWidth = 100;
        this.gridHeight = 56;
        this.cellSize = 16;
        this.tiles = [];
        this.spawnPoints = [];

        // Editor state
        this.tool = 'paint';
        this.selectedBrick = BRICK_TYPES.length > 0 ? BRICK_TYPES[0].BrickTypeID : null;
        this.showGrid = true;
        this.zoom = 1;
        this.panX = 0;
        this.panY = 0;
        this.isPanning = false;
        this.isDrawing = false;
        this.lastPanX = 0;
        this.lastPanY = 0;
        this.spaceHeld = false;

        // Selection state
        this.selection = null;
        this.selectionTiles = null;
        this.isSelecting = false;
        this.isDraggingSelection = false;
        this.dragOffsetCol = 0;
        this.dragOffsetRow = 0;

        // Undo/Redo
        this.undoStack = [];
        this.redoStack = [];
        this.currentStroke = null;

        this.init();
    }

    MapEditor.prototype.init = function() {
        var self = this;
        this.buildPalette();
        this.resizeCanvas();

        if (IS_NEW) {
            this.initNewMap();
        } else {
            this.load();
        }

        window.addEventListener('resize', function() { self.resizeCanvas(); self.render(); });

        this.canvas.addEventListener('mousedown', function(e) { self.onMouseDown(e); });
        this.canvas.addEventListener('mousemove', function(e) { self.onMouseMove(e); });
        this.canvas.addEventListener('mouseup', function(e) { self.onMouseUp(e); });
        this.canvas.addEventListener('wheel', function(e) { self.onWheel(e); e.preventDefault(); }, {passive: false});
        this.canvas.addEventListener('contextmenu', function(e) { e.preventDefault(); });

        document.addEventListener('keydown', function(e) {
            if (e.code === 'Space') { self.spaceHeld = true; e.preventDefault(); }
            if ((e.ctrlKey || e.metaKey) && e.key === 'z') { e.preventDefault(); self.undo(); }
            if ((e.ctrlKey || e.metaKey) && e.key === 'y') { e.preventDefault(); self.redo(); }
        });
        document.addEventListener('keyup', function(e) {
            if (e.code === 'Space') { self.spaceHeld = false; }
        });
    };

    MapEditor.prototype.initNewMap = function() {
        this.gridWidth = 100;
        this.gridHeight = 56;
        this.cellSize = 16;
        this.tiles = [];
        for (var row = 0; row < this.gridHeight; row++) {
            this.tiles.push(new Array(this.gridWidth).fill(0));
        }
        this.spawnPoints = [];
        this.updateFormFromState();
        this.fitView();
        this.renderSpawnList();
        this.render();
    };

    MapEditor.prototype.buildPalette = function() {
        var palette = document.getElementById('brickPalette');
        var self = this;
        BRICK_TYPES.forEach(function(bt) {
            var id = bt.BrickTypeID;
            var btn = document.createElement('div');
            btn.className = 'brick-btn' + (id === self.selectedBrick ? ' active' : '');
            btn.title = bt.Name + (bt.Destructible ? ' (D)' : '');
            btn.setAttribute('data-id', id);

            // Draw polygon preview on a small canvas
            var cvs = document.createElement('canvas');
            cvs.width = 32;
            cvs.height = 32;
            var border = BRICK_BORDERS[id] || null;
            var color = BRICK_COLORS[id];
            drawPalettePreview(cvs, border, color);
            btn.appendChild(cvs);

            btn.onclick = function() {
                document.querySelectorAll('.brick-btn').forEach(function(b) { b.classList.remove('active'); });
                btn.classList.add('active');
                self.selectedBrick = id;
            };
            palette.appendChild(btn);
        });
    };

    MapEditor.prototype.resizeCanvas = function() {
        this.canvas.width = this.wrap.clientWidth;
        this.canvas.height = this.wrap.clientHeight;
    };

    MapEditor.prototype.load = function() {
        var self = this;
        fetch('/api/maps/tiles?id=' + MAP_ID)
            .then(function(r) { return r.json(); })
            .then(function(data) {
                self.gridWidth = data.gridWidth;
                self.gridHeight = data.gridHeight;
                self.cellSize = data.cellSize;

                if (data.tiles && data.tiles.length > 0) {
                    self.tiles = data.tiles;
                } else {
                    self.tiles = [];
                    for (var row = 0; row < self.gridHeight; row++) {
                        self.tiles.push(new Array(self.gridWidth).fill(0));
                    }
                }

                // Normalize spawn points: handle both {x,y} and {X,Y}
                self.spawnPoints = [];
                var rawSpawns = data.spawnPoints || [];
                for (var i = 0; i < rawSpawns.length; i++) {
                    var sp = rawSpawns[i];
                    self.spawnPoints.push({
                        x: sp.x !== undefined ? sp.x : sp.X,
                        y: sp.y !== undefined ? sp.y : sp.Y
                    });
                }

                // Populate form fields
                document.getElementById('propName').value = data.name || '';
                document.getElementById('propGridWidth').value = data.gridWidth;
                document.getElementById('propGridHeight').value = data.gridHeight;
                document.getElementById('propCellSize').value = data.cellSize;
                document.getElementById('propMinRankTier').value = data.minRankTier || 'bronze';
                document.getElementById('propDescription').value = data.description || '';

                var windRange = [0, 4];
                try { windRange = JSON.parse(data.defaultWindPowerRange); } catch(e) {}
                if (Array.isArray(windRange) && windRange.length >= 2) {
                    document.getElementById('propWindMin').value = windRange[0];
                    document.getElementById('propWindMax').value = windRange[1];
                }

                self.fitView();
                self.renderSpawnList();
                self.render();
            });
    };

    MapEditor.prototype.fitView = function() {
        var scaleX = this.canvas.width / (this.gridWidth * this.cellSize);
        var scaleY = this.canvas.height / (this.gridHeight * this.cellSize);
        this.zoom = Math.min(scaleX, scaleY) * 0.9;
        this.panX = (this.canvas.width - this.gridWidth * this.cellSize * this.zoom) / 2;
        this.panY = (this.canvas.height - this.gridHeight * this.cellSize * this.zoom) / 2;
    };

    MapEditor.prototype.updateFormFromState = function() {
        document.getElementById('propName').value = '';
        document.getElementById('propGridWidth').value = this.gridWidth;
        document.getElementById('propGridHeight').value = this.gridHeight;
        document.getElementById('propCellSize').value = this.cellSize;
        document.getElementById('propWindMin').value = 0;
        document.getElementById('propWindMax').value = 4;
        document.getElementById('propMinRankTier').value = 'bronze';
        document.getElementById('propDescription').value = '';
    };

    MapEditor.prototype.save = function() {
        var mapId = IS_NEW ? (document.getElementById('propMapId') ? document.getElementById('propMapId').value.trim() : '') : MAP_ID;
        if (!mapId) {
            alert('Map ID is required');
            return;
        }

        var gridWidth = parseInt(document.getElementById('propGridWidth').value) || 100;
        var gridHeight = parseInt(document.getElementById('propGridHeight').value) || 56;
        var cellSize = parseInt(document.getElementById('propCellSize').value) || 16;

        // Resize tiles array if grid dimensions changed
        if (gridWidth !== this.gridWidth || gridHeight !== this.gridHeight) {
            var newTiles = [];
            for (var r = 0; r < gridHeight; r++) {
                var row = [];
                for (var c = 0; c < gridWidth; c++) {
                    row.push((r < this.tiles.length && c < (this.tiles[r] || []).length) ? this.tiles[r][c] : 0);
                }
                newTiles.push(row);
            }
            this.tiles = newTiles;
            this.gridWidth = gridWidth;
            this.gridHeight = gridHeight;
            this.cellSize = cellSize;
        }

        var windMin = parseFloat(document.getElementById('propWindMin').value) || 0;
        var windMax = parseFloat(document.getElementById('propWindMax').value) || 4;

        var body = {
            mapId: mapId,
            name: document.getElementById('propName').value,
            gridWidth: gridWidth,
            gridHeight: gridHeight,
            cellSize: cellSize,
            defaultWindPowerRange: [windMin, windMax],
            minRankTier: document.getElementById('propMinRankTier').value,
            description: document.getElementById('propDescription').value,
            tiles: this.tiles,
            spawnPoints: this.spawnPoints
        };

        var url = '/api/maps/save';
        if (!IS_NEW) {
            url += '?id=' + MAP_ID;
        }

        fetch(url, {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(body)
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.ok) {
                alert('Saved!');
                if (IS_NEW && data.mapId) {
                    // Redirect to editor with the new map ID
                    window.location.href = '/maps/editor?id=' + data.mapId;
                }
            } else {
                alert('Save failed: ' + (data.error || 'unknown'));
            }
        });
    };

    MapEditor.prototype.exportYAML = function() {
        if (IS_NEW) { alert('Save the map first before exporting.'); return; }
        window.open('/api/maps/export?id=' + MAP_ID, '_blank');
    };

    MapEditor.prototype.screenToWorld = function(sx, sy) {
        return {
            x: (sx - this.panX) / this.zoom,
            y: (sy - this.panY) / this.zoom
        };
    };

    MapEditor.prototype.screenToCell = function(sx, sy) {
        var w = this.screenToWorld(sx, sy);
        return {
            col: Math.floor(w.x / this.cellSize),
            row: Math.floor(w.y / this.cellSize)
        };
    };

    MapEditor.prototype.inBounds = function(col, row) {
        return col >= 0 && col < this.gridWidth && row >= 0 && row < this.gridHeight;
    };

    MapEditor.prototype.pushUndo = function(changes) {
        if (!changes || Object.keys(changes).length === 0) return;
        this.undoStack.push(changes);
        this.redoStack = [];
    };

    MapEditor.prototype.undo = function() {
        if (this.undoStack.length === 0) return;
        var changes = this.undoStack.pop();
        var redo = {};
        for (var key in changes) {
            var parts = key.split(',');
            var row = parseInt(parts[0]), col = parseInt(parts[1]);
            redo[key] = this.tiles[row][col];
            this.tiles[row][col] = changes[key];
        }
        this.redoStack.push(redo);
        this.render();
    };

    MapEditor.prototype.redo = function() {
        if (this.redoStack.length === 0) return;
        var changes = this.redoStack.pop();
        var undo = {};
        for (var key in changes) {
            var parts = key.split(',');
            var row = parseInt(parts[0]), col = parseInt(parts[1]);
            undo[key] = this.tiles[row][col];
            this.tiles[row][col] = changes[key];
        }
        this.undoStack.push(undo);
        this.render();
    };

    MapEditor.prototype.setTool = function(tool, btn) {
        this.tool = tool;
        this.selection = null;
        this.selectionTiles = null;
        document.querySelectorAll('.tool-btn').forEach(function(b) { b.classList.remove('active'); });
        if (btn) btn.classList.add('active');
        this.render();
    };

    MapEditor.prototype.paintCell = function(col, row) {
        if (!this.inBounds(col, row)) return;
        var old = this.tiles[row][col];
        var val = this.tool === 'erase' ? 0 : this.selectedBrick;
        if (old === val) return;
        if (!this.currentStroke) this.currentStroke = {};
        var key = row + ',' + col;
        if (!(key in this.currentStroke)) this.currentStroke[key] = old;
        this.tiles[row][col] = val;
    };

    MapEditor.prototype.floodFill = function(col, row) {
        if (!this.inBounds(col, row)) return;
        var target = this.tiles[row][col];
        var fill = this.selectedBrick;
        if (target === fill) return;

        var changes = {};
        var stack = [[col, row]];
        var visited = {};

        while (stack.length > 0) {
            var pos = stack.pop();
            var c = pos[0], r = pos[1];
            var key = r + ',' + c;
            if (visited[key]) continue;
            if (!this.inBounds(c, r)) continue;
            if (this.tiles[r][c] !== target) continue;

            visited[key] = true;
            changes[key] = this.tiles[r][c];
            this.tiles[r][c] = fill;

            stack.push([c + 1, r]);
            stack.push([c - 1, r]);
            stack.push([c, r + 1]);
            stack.push([c, r - 1]);
        }

        this.pushUndo(changes);
    };

    MapEditor.prototype.onMouseDown = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;

        if (e.button === 1 || (this.spaceHeld && e.button === 0)) {
            this.isPanning = true;
            this.lastPanX = e.clientX;
            this.lastPanY = e.clientY;
            return;
        }

        var cell = this.screenToCell(sx, sy);

        if (this.tool === 'paint' || this.tool === 'erase') {
            this.isDrawing = true;
            this.currentStroke = {};
            this.paintCell(cell.col, cell.row);
            this.render();
        } else if (this.tool === 'fill') {
            this.floodFill(cell.col, cell.row);
            this.render();
        } else if (this.tool === 'select') {
            if (this.selection && this.selectionTiles) {
                var s = this.selection;
                var minCol = Math.min(s.startCol, s.endCol);
                var maxCol = Math.max(s.startCol, s.endCol);
                var minRow = Math.min(s.startRow, s.endRow);
                var maxRow = Math.max(s.startRow, s.endRow);
                if (cell.col >= minCol && cell.col <= maxCol && cell.row >= minRow && cell.row <= maxRow) {
                    this.isDraggingSelection = true;
                    this.dragOffsetCol = cell.col - minCol;
                    this.dragOffsetRow = cell.row - minRow;
                    return;
                }
            }
            this.isSelecting = true;
            this.selection = {startCol: cell.col, startRow: cell.row, endCol: cell.col, endRow: cell.row};
            this.selectionTiles = null;
            this.render();
        } else if (this.tool === 'spawn') {
            var world = this.screenToWorld(sx, sy);
            this.spawnPoints.push({x: Math.round(world.x * 10) / 10, y: Math.round(world.y * 10) / 10});
            this.renderSpawnList();
            this.render();
        }
    };

    MapEditor.prototype.onMouseMove = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;

        var cell = this.screenToCell(sx, sy);
        var world = this.screenToWorld(sx, sy);
        document.getElementById('coordsDisplay').textContent =
            'Cell: ' + cell.col + ',' + cell.row + ' | Px: ' + Math.round(world.x) + ',' + Math.round(world.y);

        if (this.isPanning) {
            this.panX += e.clientX - this.lastPanX;
            this.panY += e.clientY - this.lastPanY;
            this.lastPanX = e.clientX;
            this.lastPanY = e.clientY;
            this.render();
            return;
        }

        if (this.isDrawing && (this.tool === 'paint' || this.tool === 'erase')) {
            this.paintCell(cell.col, cell.row);
            this.render();
        }

        if (this.isSelecting && this.tool === 'select') {
            this.selection.endCol = cell.col;
            this.selection.endRow = cell.row;
            this.render();
        }

        if (this.isDraggingSelection && this.selectionTiles) {
            var newMinCol = cell.col - this.dragOffsetCol;
            var newMinRow = cell.row - this.dragOffsetRow;
            var w = this.selectionTiles[0].length;
            var h = this.selectionTiles.length;
            this.selection = {
                startCol: newMinCol, startRow: newMinRow,
                endCol: newMinCol + w - 1, endRow: newMinRow + h - 1
            };
            this.render();
        }
    };

    MapEditor.prototype.onMouseUp = function(e) {
        if (this.isPanning) {
            this.isPanning = false;
            return;
        }

        if (this.isDrawing) {
            this.isDrawing = false;
            this.pushUndo(this.currentStroke);
            this.currentStroke = null;
        }

        if (this.isSelecting) {
            this.isSelecting = false;
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var maxCol = Math.max(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            var maxRow = Math.max(s.startRow, s.endRow);
            this.selectionTiles = [];
            for (var r = minRow; r <= maxRow; r++) {
                var row = [];
                for (var c = minCol; c <= maxCol; c++) {
                    row.push(this.inBounds(c, r) ? this.tiles[r][c] : 0);
                }
                this.selectionTiles.push(row);
            }
            this.selection = {startCol: minCol, startRow: minRow, endCol: maxCol, endRow: maxRow};
        }

        if (this.isDraggingSelection && this.selectionTiles) {
            this.isDraggingSelection = false;
            var changes = {};
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            for (var r = 0; r < this.selectionTiles.length; r++) {
                for (var c = 0; c < this.selectionTiles[r].length; c++) {
                    var tr = minRow + r;
                    var tc = minCol + c;
                    if (this.inBounds(tc, tr)) {
                        var key = tr + ',' + tc;
                        changes[key] = this.tiles[tr][tc];
                        this.tiles[tr][tc] = this.selectionTiles[r][c];
                    }
                }
            }
            this.pushUndo(changes);
            this.selectionTiles = null;
            this.selection = null;
            this.render();
        }
    };

    MapEditor.prototype.onWheel = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var mx = e.clientX - rect.left;
        var my = e.clientY - rect.top;

        var oldZoom = this.zoom;
        var factor = e.deltaY < 0 ? 1.1 : 0.9;
        this.zoom = Math.max(0.1, Math.min(10, this.zoom * factor));

        this.panX = mx - (mx - this.panX) * (this.zoom / oldZoom);
        this.panY = my - (my - this.panY) * (this.zoom / oldZoom);

        this.render();
    };

    MapEditor.prototype.toggleGrid = function(show) {
        this.showGrid = show;
        this.render();
    };

    MapEditor.prototype.renderSpawnList = function() {
        var list = document.getElementById('spawnList');
        var self = this;
        list.innerHTML = '';
        this.spawnPoints.forEach(function(sp, i) {
            var div = document.createElement('div');
            div.className = 'spawn-item';
            div.innerHTML = '<span>#' + (i+1) + ': (' + sp.x + ', ' + sp.y + ')</span>' +
                '<button class="btn btn-danger btn-sm" style="padding:2px 6px;font-size:10px">X</button>';
            div.querySelector('button').onclick = function() {
                self.spawnPoints.splice(i, 1);
                self.renderSpawnList();
                self.render();
            };
            list.appendChild(div);
        });
    };

    MapEditor.prototype.render = function() {
        var ctx = this.ctx;
        var w = this.canvas.width;
        var h = this.canvas.height;
        var cs = this.cellSize;

        ctx.clearRect(0, 0, w, h);
        ctx.save();
        ctx.translate(this.panX, this.panY);
        ctx.scale(this.zoom, this.zoom);

        // Background
        ctx.fillStyle = '#2a2a4e';
        ctx.fillRect(0, 0, this.gridWidth * cs, this.gridHeight * cs);

        // Tiles with polygon rendering
        for (var row = 0; row < this.gridHeight; row++) {
            for (var col = 0; col < this.gridWidth; col++) {
                var tile = this.tiles[row] ? this.tiles[row][col] : null;
                if (tile > 0) {
                    var border = BRICK_BORDERS[tile];
                    var color = BRICK_COLORS[tile] || '#888';
                    var ox = col * cs;
                    var oy = row * cs;

                    if (border) {
                        drawBrickPolygon(ctx, border, cs, ox, oy, color);
                    } else {
                        ctx.fillStyle = color;
                        ctx.fillRect(ox, oy, cs, cs);
                    }
                }
            }
        }

        // Grid lines
        if (this.showGrid && this.zoom > 0.3) {
            ctx.strokeStyle = 'rgba(255,255,255,0.1)';
            ctx.lineWidth = 0.5 / this.zoom;
            for (var x = 0; x <= this.gridWidth; x++) {
                ctx.beginPath();
                ctx.moveTo(x * cs, 0);
                ctx.lineTo(x * cs, this.gridHeight * cs);
                ctx.stroke();
            }
            for (var y = 0; y <= this.gridHeight; y++) {
                ctx.beginPath();
                ctx.moveTo(0, y * cs);
                ctx.lineTo(this.gridWidth * cs, y * cs);
                ctx.stroke();
            }
        }

        // Selection
        if (this.selection) {
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var maxCol = Math.max(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            var maxRow = Math.max(s.startRow, s.endRow);
            ctx.strokeStyle = '#4a6cf7';
            ctx.lineWidth = 2 / this.zoom;
            ctx.setLineDash([4 / this.zoom, 4 / this.zoom]);
            ctx.strokeRect(minCol * cs, minRow * cs, (maxCol - minCol + 1) * cs, (maxRow - minRow + 1) * cs);
            ctx.setLineDash([]);

            if (this.isDraggingSelection && this.selectionTiles) {
                ctx.globalAlpha = 0.6;
                for (var r = 0; r < this.selectionTiles.length; r++) {
                    for (var c = 0; c < this.selectionTiles[r].length; c++) {
                        var tile = this.selectionTiles[r][c];
                        if (tile) {
                            var border2 = BRICK_BORDERS[tile];
                            var color2 = BRICK_COLORS[tile] || '#888';
                            var ox2 = (minCol + c) * cs;
                            var oy2 = (minRow + r) * cs;
                            if (border2) {
                                drawBrickPolygon(ctx, border2, cs, ox2, oy2, color2);
                            } else {
                                ctx.fillStyle = color2;
                                ctx.fillRect(ox2, oy2, cs, cs);
                            }
                        }
                    }
                }
                ctx.globalAlpha = 1.0;
            }
        }

        // Spawn points
        ctx.fillStyle = '#FFD700';
        ctx.strokeStyle = '#000';
        ctx.lineWidth = 1 / this.zoom;
        for (var i = 0; i < this.spawnPoints.length; i++) {
            var sp = this.spawnPoints[i];
            ctx.beginPath();
            ctx.arc(sp.x, sp.y, 6 / this.zoom, 0, Math.PI * 2);
            ctx.fill();
            ctx.stroke();
            ctx.fillStyle = '#000';
            ctx.font = (10 / this.zoom) + 'px sans-serif';
            ctx.fillText('' + (i + 1), sp.x + 8 / this.zoom, sp.y + 4 / this.zoom);
            ctx.fillStyle = '#FFD700';
        }

        // Map border
        ctx.strokeStyle = '#fff';
        ctx.lineWidth = 1 / this.zoom;
        ctx.strokeRect(0, 0, this.gridWidth * cs, this.gridHeight * cs);

        ctx.restore();
    };

    window.editor = new MapEditor('editorCanvas', 'canvasWrap');
})();
```

- [ ] **Step 2: Verify the admin server starts and editor page loads**

Run: `go run cmd/admin/main.go`
Open: `http://localhost:9000/maps/editor?id=grassland_valley`
Expected: Editor loads with map settings form on right, brick polygons rendered on canvas, spawn points visible

- [ ] **Step 3: Commit**

```bash
git add internal/admin/static/map_editor.js
git commit -m "feat: rewrite map editor JS with polygon rendering and full form handling"
```

---

### Task 7: GameData — Add MinRankTier to MapConfig

**Files:**
- Modify: `internal/game/gamedata/loader.go:74-88,311-351`

- [ ] **Step 1: Add MinRankTier to MapConfig struct**

At `loader.go:74-88`, add field:

```go
type MapConfig struct {
	MapID                 string       `yaml:"mapId"`
	Name                  string       `yaml:"name"`
	GridWidth             int          `yaml:"gridWidth"`
	GridHeight            int          `yaml:"gridHeight"`
	CellSize              int          `yaml:"cellSize"`
	DefaultWindPowerRange []float64    `yaml:"defaultWindPowerRange"`
	Tiles                 [][]int      `yaml:"tiles"`
	SpawnPoints           []SpawnPoint `yaml:"spawnPoints"`
	MinRankTier           string       `yaml:"minRankTier"`

	// Legacy fields (for backward compatibility during transition)
	Width         int            `yaml:"width,omitempty"`
	Height        int            `yaml:"height,omitempty"`
	TerrainLayers []TerrainLayer `yaml:"terrainLayers,omitempty"`
}
```

- [ ] **Step 2: Update LoadGameDataFromDB to load min_rank_tier**

At `loader.go:312` (the maps SELECT query), add `min_rank_tier` to the query and scan:

```go
	rows, err = db.Pool.Query(ctx, `SELECT map_id, name, grid_width, grid_height, cell_size,
		default_wind_power_range, tiles, spawn_points,
		width, height, terrain_layers, min_rank_tier
		FROM config_maps`)
```

At the `rows.Scan` call, add `&m.MinRankTier`:

```go
		if err := rows.Scan(&m.MapID, &m.Name, &m.GridWidth, &m.GridHeight, &m.CellSize,
			&windRangeJSON, &tilesJSON, &spawnJSON,
			&legacyWidth, &legacyHeight, &terrainJSON, &m.MinRankTier); err != nil {
```

- [ ] **Step 3: Update configs/maps.yaml with minRankTier**

Add `minRankTier` field to each map in `configs/maps.yaml`:

```yaml
- mapId: grassland_valley
  name: Grassland Valley
  # ... existing fields ...
  minRankTier: bronze

- mapId: frozen_peak
  name: Frozen Peak
  # ... existing fields ...
  minRankTier: silver

- mapId: steel_base
  name: Steel Base
  # ... existing fields ...
  minRankTier: gold
```

- [ ] **Step 4: Verify build**

Run: `go build ./internal/game/...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/game/gamedata/loader.go configs/maps.yaml
git commit -m "feat: add MinRankTier to MapConfig and load from DB"
```

---

### Task 8: Matchmaker — Tier-based map selection

**Files:**
- Modify: `internal/game/matchmaker/matchmaker.go:35-78,274-352,450-462`
- Modify: `cmd/game/main.go:97-104`

- [ ] **Step 1: Add tier ordering and map tier data to matchmaker**

At the top of `matchmaker.go`, after the imports, add:

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

func ratingToTier(rating int) string {
	switch {
	case rating < 1000:
		return "bronze"
	case rating < 1200:
		return "silver"
	case rating < 1500:
		return "gold"
	case rating < 1800:
		return "platinum"
	case rating < 2200:
		return "diamond"
	default:
		return "master"
	}
}
```

- [ ] **Step 2: Add mapTiers field to Matchmaker struct**

At `matchmaker.go:35-48`, add `mapTiers` field:

```go
type Matchmaker struct {
	queue       *Queue
	db          *database.PostgresDB
	redis       *database.RedisClient
	nodeID      string
	cfg         MatchmakingConfig
	eloConfig   EloConfig
	botConfig   BotDifficultyConfig
	roomCreator RoomCreator
	mapIDs      []string
	mapTiers    map[string]string // mapID → minRankTier
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
}
```

- [ ] **Step 3: Update NewMatchmaker to accept mapTiers**

Change the constructor signature and body at `matchmaker.go:52-78`:

```go
func NewMatchmaker(
	db *database.PostgresDB,
	redis *database.RedisClient,
	nodeID string,
	roomCreator RoomCreator,
	mapIDs []string,
	mapTiers map[string]string,
) *Matchmaker {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := LoadMatchmakingConfig(ctx, db)
	eloConfig := LoadEloConfig(ctx, db)
	botConfig := LoadBotDifficultyConfig(ctx, db)

	return &Matchmaker{
		queue:       NewQueue(redis),
		db:          db,
		redis:       redis,
		nodeID:      nodeID,
		cfg:         cfg,
		eloConfig:   eloConfig,
		botConfig:   botConfig,
		roomCreator: roomCreator,
		mapIDs:      mapIDs,
		mapTiers:    mapTiers,
		ctx:         ctx,
		cancel:      cancel,
	}
}
```

- [ ] **Step 4: Replace randomMap with randomMapForRating**

Replace `matchmaker.go:450-462`:

```go
func (m *Matchmaker) randomMapForRating(maxRating int) string {
	m.mu.RLock()
	ids := m.mapIDs
	tiers := m.mapTiers
	m.mu.RUnlock()

	playerTier := ratingToTier(maxRating)
	playerIdx := tierIndex(playerTier)

	var eligible []string
	for _, id := range ids {
		mapTier, ok := tiers[id]
		if !ok {
			mapTier = "bronze"
		}
		if tierIndex(mapTier) <= playerIdx {
			eligible = append(eligible, id)
		}
	}

	if len(eligible) == 0 {
		if len(ids) == 0 {
			return "default_map"
		}
		eligible = ids
	}

	return eligible[mrand.IntN(len(eligible))]
}
```

- [ ] **Step 5: Update matchEntries to use randomMapForRating**

At `matchmaker.go:289-294`, compute max rating and pass to randomMapForRating:

```go
	maxRating := e1.Rating
	if e2.Rating > maxRating {
		maxRating = e2.Rating
	}

	result := MatchResult{
		Entry1: e1,
		Entry2: e2,
		MapID:  m.randomMapForRating(maxRating),
		HasBot: false,
	}
```

- [ ] **Step 6: Update matchWithBot to use randomMapForRating**

At `matchmaker.go:332-337`:

```go
	result := MatchResult{
		Entry1: entry,
		Entry2: botEntry,
		MapID:  m.randomMapForRating(entry.Rating),
		HasBot: true,
	}
```

- [ ] **Step 7: Update cmd/game/main.go to pass mapTiers**

At `cmd/game/main.go:97-104`, build mapTiers map:

```go
	// Collect available map IDs and their tier requirements
	mapIDs := make([]string, 0)
	mapTiers := make(map[string]string)
	for mapID, mapCfg := range gamedata.Data.Maps {
		mapIDs = append(mapIDs, mapID)
		tier := mapCfg.MinRankTier
		if tier == "" {
			tier = "bronze"
		}
		mapTiers[mapID] = tier
	}

	// Matchmaker
	mm := matchmaker.NewMatchmaker(db, redisClient, nodeID, roomHub, mapIDs, mapTiers)
	go mm.Run()
```

- [ ] **Step 8: Verify build**

Run: `go build ./...`
Expected: Build succeeds with no errors

- [ ] **Step 9: Commit**

```bash
git add internal/game/matchmaker/matchmaker.go cmd/game/main.go
git commit -m "feat: add tier-based map selection in matchmaker"
```

---

### Task 9: Tests

**Files:**
- Modify: `internal/game/match/terrain_test.go` (verify existing tests still pass)
- Create: `internal/game/matchmaker/map_selection_test.go`

- [ ] **Step 1: Write test for tier-based map selection**

```go
package matchmaker

import "testing"

func TestTierIndex(t *testing.T) {
	tests := []struct {
		tier string
		want int
	}{
		{"bronze", 0},
		{"silver", 1},
		{"gold", 2},
		{"platinum", 3},
		{"diamond", 4},
		{"master", 5},
		{"unknown", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := tierIndex(tt.tier); got != tt.want {
			t.Errorf("tierIndex(%q) = %d, want %d", tt.tier, got, tt.want)
		}
	}
}

func TestRatingToTier(t *testing.T) {
	tests := []struct {
		rating int
		want   string
	}{
		{500, "bronze"},
		{999, "bronze"},
		{1000, "silver"},
		{1199, "silver"},
		{1200, "gold"},
		{1499, "gold"},
		{1500, "platinum"},
		{1800, "diamond"},
		{2200, "master"},
		{3000, "master"},
	}
	for _, tt := range tests {
		if got := ratingToTier(tt.rating); got != tt.want {
			t.Errorf("ratingToTier(%d) = %q, want %q", tt.rating, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/game/matchmaker/... -v -run TestTierIndex`
Run: `go test ./internal/game/matchmaker/... -v -run TestRatingToTier`
Expected: PASS

- [ ] **Step 3: Run existing terrain tests**

Run: `go test ./internal/game/match/... -v`
Expected: All existing tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/game/matchmaker/map_selection_test.go
git commit -m "test: add tier index and rating-to-tier tests"
```

---

### Task 10: Update seed.go to include min_rank_tier

**Files:**
- Modify: `internal/admin/seed.go:158-199`

- [ ] **Step 1: Add min_rank_tier to the map seed INSERT query**

At `seed.go:159-163`, update the query to include `min_rank_tier`:

```go
	mapQuery := `INSERT INTO config_maps
		(map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points,
		 grid_width, grid_height, cell_size, tiles, min_rank_tier)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (map_id) DO NOTHING`
```

At `seed.go:193-196`, add `MinRankTier` to the Exec call:

```go
		minRankTier := m.MinRankTier
		if minRankTier == "" {
			minRankTier = "bronze"
		}
		if _, err := db.Pool.Exec(ctx, mapQuery,
			m.MapID, m.Name, m.Width, m.Height, windRange, terrainLayers, spawnPoints,
			gridWidth, gridHeight, cellSize, tiles, minRankTier,
		); err != nil {
			return fmt.Errorf("insert map %s: %w", m.MapID, err)
		}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/admin/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/admin/seed.go
git commit -m "feat: include min_rank_tier in map seed data"
```

---

### Task 11: Final integration verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: All tests PASS

- [ ] **Step 2: Verify full build**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 3: Final commit if any remaining changes**

```bash
git add -A
git status
# If there are changes:
git commit -m "chore: final cleanup for map rank restriction and editor unification"
```
