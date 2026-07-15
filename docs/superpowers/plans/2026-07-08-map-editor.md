# Map Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tilemap-based map editor to the admin dashboard, replacing hardcoded terrain generation with grid-based tile data.

**Architecture:** New DB table `config_brick_types` for brick definitions. Existing `config_maps` gets new columns (`grid_width`, `grid_height`, `cell_size`, `tiles`). Admin dashboard gets a canvas-based visual editor at `/admin/maps/:id/editor`. Game server's `terrain.go` reads tile grid instead of using hardcoded math functions.

**Tech Stack:** Go (chi router, pgx), HTML5 Canvas + Vanilla JS, PostgreSQL (JSONB), YAML export

---

### File Structure

**New files:**
- `migrations/007_map_editor.up.sql` — DB migration
- `migrations/007_map_editor.down.sql` — Rollback migration
- `internal/admin/handlers_map_editor.go` — Map editor + brick type HTTP handlers
- `internal/admin/templates/map_editor.html` — Canvas editor page template
- `internal/admin/templates/brick_types.html` — Brick types CRUD page
- `internal/admin/templates/brick_type_edit.html` — Brick type edit form
- `internal/admin/static/map_editor.js` — Canvas editor JavaScript
- `internal/game/match/terrain_test.go` — New tests for tilemap terrain

**Modified files:**
- `internal/admin/repository.go` — Add brick type CRUD + map tiles CRUD + export methods
- `internal/admin/server.go` — Register new routes, serve static JS
- `internal/admin/seed.go` — Seed default brick types
- `internal/admin/templates/layout.html` — Add "Brick Types" nav link
- `internal/admin/templates/config_list.html` — Add "Open Editor" button for maps
- `internal/game/gamedata/loader.go` — Update `MapConfig` struct + YAML/DB loading
- `internal/game/match/terrain.go` — Rewrite `NewTerrain()` for tilemap, add `DestructibleMask`
- `internal/game/match/match.go` — Update `NewTerrain()` call, update wind range to float
- `internal/game/match/model.go` — Wind power to float
- `internal/game/match/physics_test.go` — Update `NewTerrain()` call
- `configs/maps.yaml` — Update to new tilemap format

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/007_map_editor.up.sql`
- Create: `migrations/007_map_editor.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- migrations/007_map_editor.up.sql

-- Brick type registry
CREATE TABLE config_brick_types (
    brick_type_id TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    destructible  BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seed default brick types
INSERT INTO config_brick_types (brick_type_id, name, destructible) VALUES
    ('dirt', 'Dirt', true),
    ('rock', 'Rock', false),
    ('ice', 'Ice', true),
    ('lava', 'Lava', false),
    ('fragile', 'Fragile', true);

-- Add tilemap columns to config_maps
ALTER TABLE config_maps
    ADD COLUMN grid_width  INT NOT NULL DEFAULT 100,
    ADD COLUMN grid_height INT NOT NULL DEFAULT 56,
    ADD COLUMN cell_size   INT NOT NULL DEFAULT 16,
    ADD COLUMN tiles       JSONB NOT NULL DEFAULT '[]';
```

- [ ] **Step 2: Write the down migration**

```sql
-- migrations/007_map_editor.down.sql

ALTER TABLE config_maps
    DROP COLUMN IF EXISTS tiles,
    DROP COLUMN IF EXISTS cell_size,
    DROP COLUMN IF EXISTS grid_height,
    DROP COLUMN IF EXISTS grid_width;

DROP TABLE IF EXISTS config_brick_types;
```

- [ ] **Step 3: Run the migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration 007 applied successfully

- [ ] **Step 4: Commit**

```bash
git add migrations/007_map_editor.up.sql migrations/007_map_editor.down.sql
git commit -m "feat(map-editor): add brick types table and tilemap columns"
```

---

### Task 2: Brick Types Repository

**Files:**
- Modify: `internal/admin/repository.go`

- [ ] **Step 1: Add ConfigBrickType struct and CRUD methods**

Add after the Maps CRUD section (after line 553) in `repository.go`:

```go
// ---------------------------------------------------------------------------
// Brick Types CRUD
// ---------------------------------------------------------------------------

// ConfigBrickType represents a row in config_brick_types.
type ConfigBrickType struct {
	BrickTypeID  string
	Name         string
	Destructible bool
}

// GetBrickTypes returns all brick types ordered by brick_type_id.
func (r *Repository) GetBrickTypes(ctx context.Context) ([]ConfigBrickType, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT brick_type_id, name, destructible FROM config_brick_types ORDER BY brick_type_id`)
	if err != nil {
		return nil, fmt.Errorf("query brick types: %w", err)
	}
	defer rows.Close()

	var types []ConfigBrickType
	for rows.Next() {
		var bt ConfigBrickType
		if err := rows.Scan(&bt.BrickTypeID, &bt.Name, &bt.Destructible); err != nil {
			return nil, fmt.Errorf("scan brick type: %w", err)
		}
		types = append(types, bt)
	}
	return types, rows.Err()
}

// GetBrickType returns a single brick type by ID.
func (r *Repository) GetBrickType(ctx context.Context, id string) (*ConfigBrickType, error) {
	var bt ConfigBrickType
	err := r.db.Pool.QueryRow(ctx,
		`SELECT brick_type_id, name, destructible FROM config_brick_types WHERE brick_type_id = $1`, id).
		Scan(&bt.BrickTypeID, &bt.Name, &bt.Destructible)
	if err != nil {
		return nil, fmt.Errorf("get brick type %s: %w", id, err)
	}
	return &bt, nil
}

// UpsertBrickType inserts or updates a brick type.
func (r *Repository) UpsertBrickType(ctx context.Context, bt *ConfigBrickType) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_brick_types (brick_type_id, name, destructible, updated_at)
		 VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		 ON CONFLICT (brick_type_id) DO UPDATE SET
		   name=EXCLUDED.name, destructible=EXCLUDED.destructible, updated_at=CURRENT_TIMESTAMP`,
		bt.BrickTypeID, bt.Name, bt.Destructible)
	if err != nil {
		return fmt.Errorf("upsert brick type %s: %w", bt.BrickTypeID, err)
	}
	return nil
}

// DeleteBrickType deletes a brick type by ID.
func (r *Repository) DeleteBrickType(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_brick_types WHERE brick_type_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete brick type %s: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 2: Add map tiles get/save and YAML export methods**

Add after the brick types section:

```go
// ---------------------------------------------------------------------------
// Map Tiles (Editor data)
// ---------------------------------------------------------------------------

// MapTilesData holds the tile grid and spawn points for the editor API.
type MapTilesData struct {
	MapID                 string          `json:"mapId"`
	Name                  string          `json:"name"`
	GridWidth             int             `json:"gridWidth"`
	GridHeight            int             `json:"gridHeight"`
	CellSize              int             `json:"cellSize"`
	DefaultWindPowerRange json.RawMessage `json:"defaultWindPowerRange"`
	Tiles                 json.RawMessage `json:"tiles"`
	SpawnPoints           json.RawMessage `json:"spawnPoints"`
}

// GetMapTiles returns tiles data for the map editor.
func (r *Repository) GetMapTiles(ctx context.Context, id string) (*MapTilesData, error) {
	var d MapTilesData
	err := r.db.Pool.QueryRow(ctx,
		`SELECT map_id, name, grid_width, grid_height, cell_size,
		        default_wind_power_range, tiles, spawn_points
		 FROM config_maps WHERE map_id = $1`, id).
		Scan(&d.MapID, &d.Name, &d.GridWidth, &d.GridHeight, &d.CellSize,
			&d.DefaultWindPowerRange, &d.Tiles, &d.SpawnPoints)
	if err != nil {
		return nil, fmt.Errorf("get map tiles %s: %w", id, err)
	}
	return &d, nil
}

// SaveMapTiles saves tiles and spawn points for a map.
func (r *Repository) SaveMapTiles(ctx context.Context, id string, tiles, spawnPoints json.RawMessage) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE config_maps SET tiles = $1, spawn_points = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE map_id = $3`,
		tiles, spawnPoints, id)
	if err != nil {
		return fmt.Errorf("save map tiles %s: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/admin/repository.go
git commit -m "feat(map-editor): add brick types and map tiles repository methods"
```

---

### Task 3: Brick Types Admin Handlers & Templates

**Files:**
- Create: `internal/admin/templates/brick_types.html`
- Create: `internal/admin/templates/brick_type_edit.html`
- Modify: `internal/admin/handlers_map_editor.go` (will be created here)
- Modify: `internal/admin/server.go`
- Modify: `internal/admin/templates/layout.html`

- [ ] **Step 1: Create brick_types.html list template**

```html
{{define "content"}}
<div class="top-bar">
    <h1>Brick Types</h1>
    <a href="/brick-types/edit" class="btn btn-primary">+ Add New</a>
</div>
{{if .Flash}}<div class="flash flash-success">{{.Flash}}</div>{{end}}
{{if .Error}}<div class="flash flash-error">{{.Error}}</div>{{end}}
<div class="card">
<table>
<thead><tr><th>ID</th><th>Name</th><th>Destructible</th><th>Actions</th></tr></thead>
<tbody>
{{range .Items}}
<tr>
    <td>{{.BrickTypeID}}</td>
    <td>{{.Name}}</td>
    <td>{{if .Destructible}}<span class="badge badge-success">Yes</span>{{else}}<span class="badge badge-danger">No</span>{{end}}</td>
    <td>
        <a href="/brick-types/edit?id={{.BrickTypeID}}" class="btn btn-primary btn-sm">Edit</a>
        <form method="POST" action="/brick-types/delete" style="display:inline" onsubmit="return confirm('Delete this brick type?')">
            <input type="hidden" name="id" value="{{.BrickTypeID}}">
            <button type="submit" class="btn btn-danger btn-sm">Delete</button>
        </form>
    </td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}
```

- [ ] **Step 2: Create brick_type_edit.html form template**

```html
{{define "content"}}
<div class="top-bar">
    <h1>{{if .IsNew}}New{{else}}Edit{{end}} Brick Type</h1>
    <a href="/brick-types" class="btn btn-primary">Back to List</a>
</div>
{{if .Error}}<div class="flash flash-error">{{.Error}}</div>{{end}}
<div class="card">
<form method="POST" action="/brick-types/save">
    {{if not .IsNew}}<input type="hidden" name="original_id" value="{{.BrickType.BrickTypeID}}">{{end}}
    <div class="form-group">
        <label>Brick Type ID</label>
        <input type="text" name="brick_type_id" value="{{.BrickType.BrickTypeID}}" required>
        <div class="desc">Unique identifier (e.g. dirt, rock, ice)</div>
    </div>
    <div class="form-group">
        <label>Name</label>
        <input type="text" name="name" value="{{.BrickType.Name}}" required>
        <div class="desc">Display name</div>
    </div>
    <div class="form-group">
        <label>
            <input type="checkbox" name="destructible" value="true" {{if .BrickType.Destructible}}checked{{end}}>
            Destructible
        </label>
        <div class="desc">Whether explosions can destroy this brick</div>
    </div>
    <button type="submit" class="btn btn-primary">Save</button>
</form>
</div>
{{end}}
```

- [ ] **Step 3: Create handlers_map_editor.go with brick type handlers**

```go
package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"battle-squad/internal/shared/observability"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Brick Types Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleBrickTypesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items, err := s.repo.GetBrickTypes(ctx)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to list brick types")
	}

	s.render(w, "brick_types", map[string]interface{}{
		"ActivePage": "brick-types",
		"Items":      items,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleBrickTypeEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("id")
	isNew := id == ""

	var bt ConfigBrickType
	if !isNew {
		found, err := s.repo.GetBrickType(ctx, id)
		if err != nil {
			http.Redirect(w, r, "/brick-types?error=Brick+type+not+found", http.StatusSeeOther)
			return
		}
		bt = *found
	}

	s.render(w, "brick_type_edit", map[string]interface{}{
		"ActivePage": "brick-types",
		"BrickType":  bt,
		"IsNew":      isNew,
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleBrickTypeSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/brick-types?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()

	bt := &ConfigBrickType{
		BrickTypeID:  strings.TrimSpace(r.FormValue("brick_type_id")),
		Name:         r.FormValue("name"),
		Destructible: r.FormValue("destructible") == "true",
	}
	if bt.BrickTypeID == "" {
		http.Redirect(w, r, "/brick-types/edit?error=Brick+Type+ID+is+required", http.StatusSeeOther)
		return
	}
	if err := s.repo.UpsertBrickType(ctx, bt); err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert brick type")
		http.Redirect(w, r, "/brick-types?error=Failed+to+save+brick+type", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/brick-types?flash=Saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleBrickTypeDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/brick-types?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	id := r.FormValue("id")
	if id == "" {
		http.Redirect(w, r, "/brick-types?error=Missing+ID", http.StatusSeeOther)
		return
	}

	if err := s.repo.DeleteBrickType(ctx, id); err != nil {
		observability.Log.Error().Err(err).Msg("failed to delete brick type")
		http.Redirect(w, r, "/brick-types?error=Failed+to+delete", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/brick-types?flash=Deleted+successfully", http.StatusSeeOther)
}
```

- [ ] **Step 4: Add nav link in layout.html**

In `internal/admin/templates/layout.html`, add after the Maps nav link (line 68):

```html
    <a href="/brick-types" class="{{if eq .ActivePage "brick-types"}}active{{end}}">Brick Types</a>
```

- [ ] **Step 5: Register routes in server.go**

Add to the `Routes()` function in `server.go`, after the maps routes (after line 77):

```go
	// Brick Types
	r.Get("/brick-types", s.handleBrickTypesList)
	r.Get("/brick-types/edit", s.handleBrickTypeEdit)
	r.Post("/brick-types/save", s.handleBrickTypeSave)
	r.Post("/brick-types/delete", s.handleBrickTypeDelete)
```

- [ ] **Step 6: Verify it compiles**

Run: `go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add internal/admin/handlers_map_editor.go internal/admin/templates/brick_types.html \
  internal/admin/templates/brick_type_edit.html internal/admin/server.go \
  internal/admin/templates/layout.html
git commit -m "feat(map-editor): add brick types CRUD in admin dashboard"
```

---

### Task 4: Map Editor API Handlers

**Files:**
- Modify: `internal/admin/handlers_map_editor.go`
- Modify: `internal/admin/server.go`

- [ ] **Step 1: Add map editor page handler and API handlers**

Append to `handlers_map_editor.go`:

```go
// ---------------------------------------------------------------------------
// Map Editor Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleMapEditor(w http.ResponseWriter, r *http.Request) {
	mapID := r.URL.Query().Get("id")
	if mapID == "" {
		http.Redirect(w, r, "/maps?error=Missing+map+ID", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	m, err := s.repo.GetMap(ctx, mapID)
	if err != nil {
		http.Redirect(w, r, "/maps?error=Map+not+found", http.StatusSeeOther)
		return
	}

	// Load brick types for the palette
	brickTypes, err := s.repo.GetBrickTypes(ctx)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to load brick types for editor")
	}

	brickTypesJSON, _ := json.Marshal(brickTypes)

	s.render(w, "map_editor", map[string]interface{}{
		"ActivePage":     "maps",
		"MapID":          m.MapID,
		"MapName":        m.Name,
		"BrickTypesJSON": string(brickTypesJSON),
	})
}

func (s *Server) handleMapTilesGet(w http.ResponseWriter, r *http.Request) {
	mapID := r.URL.Query().Get("id")
	if mapID == "" {
		http.Error(w, `{"error":"missing map id"}`, http.StatusBadRequest)
		return
	}

	data, err := s.repo.GetMapTiles(r.Context(), mapID)
	if err != nil {
		http.Error(w, `{"error":"map not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleMapTilesSave(w http.ResponseWriter, r *http.Request) {
	mapID := r.URL.Query().Get("id")
	if mapID == "" {
		http.Error(w, `{"error":"missing map id"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Tiles       json.RawMessage `json:"tiles"`
		SpawnPoints json.RawMessage `json:"spawnPoints"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if err := s.repo.SaveMapTiles(r.Context(), mapID, body.Tiles, body.SpawnPoints); err != nil {
		observability.Log.Error().Err(err).Str("mapId", mapID).Msg("failed to save map tiles")
		http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// MapExportYAML is the YAML output structure for a single map.
type MapExportYAML struct {
	MapID                 string           `yaml:"mapId"`
	Name                  string           `yaml:"name"`
	GridWidth             int              `yaml:"gridWidth"`
	GridHeight            int              `yaml:"gridHeight"`
	CellSize              int              `yaml:"cellSize"`
	DefaultWindPowerRange []float64        `yaml:"defaultWindPowerRange"`
	Tiles                 [][]interface{}  `yaml:"tiles"`
	SpawnPoints           []map[string]float64 `yaml:"spawnPoints"`
}

func (s *Server) handleMapExport(w http.ResponseWriter, r *http.Request) {
	mapID := r.URL.Query().Get("id")
	if mapID == "" {
		http.Error(w, "missing map id", http.StatusBadRequest)
		return
	}

	data, err := s.repo.GetMapTiles(r.Context(), mapID)
	if err != nil {
		http.Error(w, "map not found", http.StatusNotFound)
		return
	}

	var export MapExportYAML
	export.MapID = data.MapID
	export.Name = data.Name
	export.GridWidth = data.GridWidth
	export.GridHeight = data.GridHeight
	export.CellSize = data.CellSize

	// Parse wind range
	if err := json.Unmarshal(data.DefaultWindPowerRange, &export.DefaultWindPowerRange); err != nil {
		export.DefaultWindPowerRange = []float64{0, 4}
	}

	// Parse tiles
	if err := json.Unmarshal(data.Tiles, &export.Tiles); err != nil {
		export.Tiles = [][]interface{}{}
	}

	// Parse spawn points
	if err := json.Unmarshal(data.SpawnPoints, &export.SpawnPoints); err != nil {
		export.SpawnPoints = []map[string]float64{}
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", "attachment; filename="+mapID+".yaml")
	yaml.NewEncoder(w).Encode([]MapExportYAML{export})
}

// handleBrickTypesAPI returns brick types as JSON for the editor.
func (s *Server) handleBrickTypesAPI(w http.ResponseWriter, r *http.Request) {
	types, err := s.repo.GetBrickTypes(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to load brick types"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types)
}
```

- [ ] **Step 2: Register map editor routes in server.go**

Add after brick types routes:

```go
	// Map Editor
	r.Get("/maps/editor", s.handleMapEditor)
	r.Get("/api/maps/tiles", s.handleMapTilesGet)
	r.Put("/api/maps/tiles", s.handleMapTilesSave)
	r.Get("/api/maps/export", s.handleMapExport)
	r.Get("/api/brick-types", s.handleBrickTypesAPI)
```

- [ ] **Step 3: Add "Open Editor" button in config_list.html**

In `internal/admin/templates/config_list.html`, in the maps section (around line 80), add an editor link before the Edit button:

Change:
```html
        <a href="/maps/edit?id={{.MapID}}" class="btn btn-primary btn-sm">Edit</a>
```
To:
```html
        <a href="/maps/editor?id={{.MapID}}" class="btn btn-success btn-sm">Editor</a>
        <a href="/maps/edit?id={{.MapID}}" class="btn btn-primary btn-sm">Edit</a>
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/admin/handlers_map_editor.go internal/admin/server.go \
  internal/admin/templates/config_list.html
git commit -m "feat(map-editor): add map editor API handlers and routes"
```

---

### Task 5: Map Editor Canvas UI — Template & Core JS

**Files:**
- Create: `internal/admin/templates/map_editor.html`
- Create: `internal/admin/static/map_editor.js`
- Modify: `internal/admin/server.go` (serve static files)

- [ ] **Step 1: Update server.go to embed and serve static files**

In `server.go`, add a second embed directive and file server route:

```go
//go:embed static/*
var staticFS embed.FS
```

In `Routes()`, add at the top of the function:

```go
	// Serve static JS/CSS files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
```

Note: The `staticFS` embed needs to be adjusted to handle the `static/` prefix. Use:

```go
	staticContent, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))
```

Add `"io/fs"` to the imports.

- [ ] **Step 2: Create map_editor.html template**

```html
{{define "content"}}
<style>
.editor-container { display: flex; gap: 12px; height: calc(100vh - 90px); }
.editor-toolbar { width: 180px; flex-shrink: 0; }
.editor-canvas-wrap { flex: 1; overflow: hidden; position: relative; background: #1a1a2e; border-radius: 8px; }
.editor-canvas-wrap canvas { display: block; cursor: crosshair; }
.editor-properties { width: 220px; flex-shrink: 0; }
.tool-btn { display: block; width: 100%; padding: 8px 12px; margin-bottom: 4px; border: 1px solid #ccc; border-radius: 4px; background: #fff; cursor: pointer; text-align: left; font-size: 13px; }
.tool-btn.active { background: #4a6cf7; color: #fff; border-color: #4a6cf7; }
.brick-btn { display: inline-block; width: 36px; height: 36px; margin: 2px; border: 2px solid #ccc; border-radius: 4px; cursor: pointer; text-align: center; line-height: 32px; font-size: 10px; }
.brick-btn.active { border-color: #4a6cf7; box-shadow: 0 0 0 2px #4a6cf7; }
.editor-top-bar { display: flex; gap: 8px; margin-bottom: 12px; align-items: center; }
.editor-top-bar h1 { font-size: 18px; margin: 0; flex: 1; }
.coords-display { position: absolute; bottom: 8px; left: 8px; background: rgba(0,0,0,0.7); color: #fff; padding: 4px 8px; border-radius: 4px; font-size: 12px; font-family: monospace; }
.spawn-list { max-height: 200px; overflow-y: auto; }
.spawn-item { display: flex; justify-content: space-between; align-items: center; padding: 4px 0; font-size: 12px; border-bottom: 1px solid #eee; }
</style>

<div class="editor-top-bar">
    <h1>Map Editor: {{.MapName}}</h1>
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
        <h3 style="font-size:14px;margin-bottom:8px">Properties</h3>
        <div class="form-group">
            <label>Grid Width</label>
            <input type="number" id="propGridWidth" readonly>
        </div>
        <div class="form-group">
            <label>Grid Height</label>
            <input type="number" id="propGridHeight" readonly>
        </div>
        <div class="form-group">
            <label>Cell Size</label>
            <input type="number" id="propCellSize" readonly>
        </div>
        <h3 style="font-size:14px;margin:12px 0 8px">Spawn Points</h3>
        <div class="spawn-list" id="spawnList"></div>
    </div>
</div>

<script>
const MAP_ID = "{{.MapID}}";
const BRICK_TYPES = JSON.parse('{{.BrickTypesJSON}}');
</script>
<script src="/static/map_editor.js"></script>
{{end}}
```

- [ ] **Step 3: Create map_editor.js — core editor class with state management**

Create `internal/admin/static/map_editor.js`:

```javascript
// Map Editor - Canvas-based tilemap editor
(function() {
    'use strict';

    // Assign a deterministic color to each brick type
    const BRICK_COLORS = {};
    const COLOR_PALETTE = [
        '#8B4513','#808080','#87CEEB','#FF4500','#DEB887',
        '#228B22','#FFD700','#9370DB','#FF6347','#2E8B57'
    ];
    BRICK_TYPES.forEach(function(bt, i) {
        BRICK_COLORS[bt.brickTypeId || bt.BrickTypeID] = COLOR_PALETTE[i % COLOR_PALETTE.length];
    });

    function MapEditor(canvasId, wrapId) {
        this.canvas = document.getElementById(canvasId);
        this.ctx = this.canvas.getContext('2d');
        this.wrap = document.getElementById(wrapId);

        // Map data
        this.gridWidth = 0;
        this.gridHeight = 0;
        this.cellSize = 16;
        this.tiles = [];       // [row][col] = brickTypeId or null
        this.spawnPoints = []; // [{x, y}]

        // Editor state
        this.tool = 'paint';
        this.selectedBrick = BRICK_TYPES.length > 0 ? (BRICK_TYPES[0].brickTypeId || BRICK_TYPES[0].BrickTypeID) : null;
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
        this.selection = null;      // {startCol, startRow, endCol, endRow}
        this.selectionTiles = null; // copied tile data for moving
        this.isSelecting = false;
        this.isDraggingSelection = false;
        this.dragOffsetCol = 0;
        this.dragOffsetRow = 0;

        // Undo/Redo
        this.undoStack = [];
        this.redoStack = [];
        this.currentStroke = null; // track changes during a single paint/erase stroke

        this.init();
    }

    MapEditor.prototype.init = function() {
        var self = this;
        this.buildPalette();
        this.resizeCanvas();
        this.load();

        window.addEventListener('resize', function() { self.resizeCanvas(); self.render(); });

        // Mouse events
        this.canvas.addEventListener('mousedown', function(e) { self.onMouseDown(e); });
        this.canvas.addEventListener('mousemove', function(e) { self.onMouseMove(e); });
        this.canvas.addEventListener('mouseup', function(e) { self.onMouseUp(e); });
        this.canvas.addEventListener('wheel', function(e) { self.onWheel(e); e.preventDefault(); }, {passive: false});
        this.canvas.addEventListener('contextmenu', function(e) { e.preventDefault(); });

        // Keyboard
        document.addEventListener('keydown', function(e) {
            if (e.code === 'Space') { self.spaceHeld = true; e.preventDefault(); }
            if ((e.ctrlKey || e.metaKey) && e.key === 'z') { e.preventDefault(); self.undo(); }
            if ((e.ctrlKey || e.metaKey) && e.key === 'y') { e.preventDefault(); self.redo(); }
        });
        document.addEventListener('keyup', function(e) {
            if (e.code === 'Space') { self.spaceHeld = false; }
        });
    };

    MapEditor.prototype.buildPalette = function() {
        var palette = document.getElementById('brickPalette');
        var self = this;
        BRICK_TYPES.forEach(function(bt) {
            var id = bt.brickTypeId || bt.BrickTypeID;
            var btn = document.createElement('div');
            btn.className = 'brick-btn' + (id === self.selectedBrick ? ' active' : '');
            btn.style.background = BRICK_COLORS[id];
            btn.title = (bt.name || bt.Name) + (bt.destructible || bt.Destructible ? ' (D)' : '');
            btn.setAttribute('data-id', id);
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

    // ----- Data Loading/Saving -----

    MapEditor.prototype.load = function() {
        var self = this;
        fetch('/api/maps/tiles?id=' + MAP_ID)
            .then(function(r) { return r.json(); })
            .then(function(data) {
                self.gridWidth = data.gridWidth;
                self.gridHeight = data.gridHeight;
                self.cellSize = data.cellSize;

                // Initialize tiles
                if (data.tiles && data.tiles.length > 0) {
                    self.tiles = data.tiles;
                } else {
                    self.tiles = [];
                    for (var row = 0; row < self.gridHeight; row++) {
                        self.tiles.push(new Array(self.gridWidth).fill(null));
                    }
                }

                self.spawnPoints = data.spawnPoints || [];

                // Fit map in view
                var scaleX = self.canvas.width / (self.gridWidth * self.cellSize);
                var scaleY = self.canvas.height / (self.gridHeight * self.cellSize);
                self.zoom = Math.min(scaleX, scaleY) * 0.9;
                self.panX = (self.canvas.width - self.gridWidth * self.cellSize * self.zoom) / 2;
                self.panY = (self.canvas.height - self.gridHeight * self.cellSize * self.zoom) / 2;

                self.updateProperties();
                self.renderSpawnList();
                self.render();
            });
    };

    MapEditor.prototype.save = function() {
        fetch('/api/maps/tiles?id=' + MAP_ID, {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({tiles: this.tiles, spawnPoints: this.spawnPoints})
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.ok) alert('Saved!');
            else alert('Save failed: ' + (data.error || 'unknown'));
        });
    };

    MapEditor.prototype.exportYAML = function() {
        window.open('/api/maps/export?id=' + MAP_ID, '_blank');
    };

    // ----- Coordinate Conversion -----

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

    // ----- Undo/Redo -----

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

    // ----- Tools -----

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
        var val = this.tool === 'erase' ? null : this.selectedBrick;
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

    // ----- Mouse Handlers -----

    MapEditor.prototype.onMouseDown = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;

        // Pan: middle button or space+left
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
            // Check if clicking inside existing selection to drag
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
            // Start new selection
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

        // Update coords display
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
            // Copy tiles from selection
            var s = this.selection;
            var minCol = Math.min(s.startCol, s.endCol);
            var maxCol = Math.max(s.startCol, s.endCol);
            var minRow = Math.min(s.startRow, s.endRow);
            var maxRow = Math.max(s.startRow, s.endRow);
            this.selectionTiles = [];
            for (var r = minRow; r <= maxRow; r++) {
                var row = [];
                for (var c = minCol; c <= maxCol; c++) {
                    row.push(this.inBounds(c, r) ? this.tiles[r][c] : null);
                }
                this.selectionTiles.push(row);
            }
            this.selection = {startCol: minCol, startRow: minRow, endCol: maxCol, endRow: maxRow};
        }

        if (this.isDraggingSelection && this.selectionTiles) {
            this.isDraggingSelection = false;
            // Place tiles at new position
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

        // Zoom towards mouse position
        this.panX = mx - (mx - this.panX) * (this.zoom / oldZoom);
        this.panY = my - (my - this.panY) * (this.zoom / oldZoom);

        this.render();
    };

    MapEditor.prototype.toggleGrid = function(show) {
        this.showGrid = show;
        this.render();
    };

    // ----- Properties Panel -----

    MapEditor.prototype.updateProperties = function() {
        document.getElementById('propGridWidth').value = this.gridWidth;
        document.getElementById('propGridHeight').value = this.gridHeight;
        document.getElementById('propCellSize').value = this.cellSize;
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

    // ----- Rendering -----

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

        // Draw tiles
        for (var row = 0; row < this.gridHeight; row++) {
            for (var col = 0; col < this.gridWidth; col++) {
                var tile = this.tiles[row] ? this.tiles[row][col] : null;
                if (tile) {
                    ctx.fillStyle = BRICK_COLORS[tile] || '#888';
                    ctx.fillRect(col * cs, row * cs, cs, cs);
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

        // Selection highlight
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

            // Draw dragged selection tiles preview
            if (this.isDraggingSelection && this.selectionTiles) {
                ctx.globalAlpha = 0.6;
                for (var r = 0; r < this.selectionTiles.length; r++) {
                    for (var c = 0; c < this.selectionTiles[r].length; c++) {
                        var tile = this.selectionTiles[r][c];
                        if (tile) {
                            ctx.fillStyle = BRICK_COLORS[tile] || '#888';
                            ctx.fillRect((minCol + c) * cs, (minRow + r) * cs, cs, cs);
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

        // Border
        ctx.strokeStyle = '#fff';
        ctx.lineWidth = 1 / this.zoom;
        ctx.strokeRect(0, 0, this.gridWidth * cs, this.gridHeight * cs);

        ctx.restore();
    };

    // ----- Init -----
    window.editor = new MapEditor('editorCanvas', 'canvasWrap');
})();
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/admin/templates/map_editor.html internal/admin/static/map_editor.js \
  internal/admin/server.go
git commit -m "feat(map-editor): add canvas-based map editor UI"
```

---

### Task 6: Update GameData MapConfig Struct & Loaders

**Files:**
- Modify: `internal/game/gamedata/loader.go`

- [ ] **Step 1: Update MapConfig struct**

Replace the existing `MapConfig` struct and related types in `loader.go`:

```go
type MapConfig struct {
	MapID                 string      `yaml:"mapId"`
	Name                  string      `yaml:"name"`
	GridWidth             int         `yaml:"gridWidth"`
	GridHeight            int         `yaml:"gridHeight"`
	CellSize              int         `yaml:"cellSize"`
	DefaultWindPowerRange []float64   `yaml:"defaultWindPowerRange"`
	Tiles                 [][]string  `yaml:"tiles"`
	SpawnPoints           []SpawnPoint `yaml:"spawnPoints"`

	// Legacy fields (for backward compatibility during transition)
	Width         int            `yaml:"width,omitempty"`
	Height        int            `yaml:"height,omitempty"`
	TerrainLayers []TerrainLayer `yaml:"terrainLayers,omitempty"`
}
```

Note: Keep `TerrainLayer` struct for backward compatibility. `SpawnPoint` stays the same.

- [ ] **Step 2: Update the DB loader (LoadGameDataFromDB) for maps**

Replace the maps loading section (around lines 281-309):

```go
	// 5. Load maps (with JSONB fields)
	rows, err = db.Pool.Query(ctx, `SELECT map_id, name, grid_width, grid_height, cell_size,
		default_wind_power_range, tiles, spawn_points,
		width, height, terrain_layers
		FROM config_maps`)
	if err != nil {
		return fmt.Errorf("failed to query config_maps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m MapConfig
		var windRangeJSON, tilesJSON, spawnJSON, terrainJSON []byte
		var legacyWidth, legacyHeight int
		if err := rows.Scan(&m.MapID, &m.Name, &m.GridWidth, &m.GridHeight, &m.CellSize,
			&windRangeJSON, &tilesJSON, &spawnJSON,
			&legacyWidth, &legacyHeight, &terrainJSON); err != nil {
			return fmt.Errorf("failed to scan config_maps row: %w", err)
		}
		m.Width = legacyWidth
		m.Height = legacyHeight
		if err := json.Unmarshal(windRangeJSON, &m.DefaultWindPowerRange); err != nil {
			return fmt.Errorf("failed to unmarshal wind_power_range for map %s: %w", m.MapID, err)
		}
		if err := json.Unmarshal(tilesJSON, &m.Tiles); err != nil {
			// Tiles might be empty array, that's OK
			m.Tiles = nil
		}
		if err := json.Unmarshal(spawnJSON, &m.SpawnPoints); err != nil {
			return fmt.Errorf("failed to unmarshal spawn_points for map %s: %w", m.MapID, err)
		}
		if terrainJSON != nil {
			json.Unmarshal(terrainJSON, &m.TerrainLayers)
		}
		gData.Maps[m.MapID] = m
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_maps: %w", err)
	}
	if len(gData.Maps) == 0 {
		return fmt.Errorf("config_maps table is empty")
	}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/game/...`
Expected: No errors (may have errors in terrain.go/match.go which we fix in next tasks)

- [ ] **Step 4: Commit**

```bash
git add internal/game/gamedata/loader.go
git commit -m "feat(map-editor): update MapConfig struct for tilemap support"
```

---

### Task 7: Update Terrain for Tilemap

**Files:**
- Modify: `internal/game/match/terrain.go`
- Create: `internal/game/match/terrain_test.go`

- [ ] **Step 1: Write tests for tilemap terrain**

Create `internal/game/match/terrain_test.go`:

```go
package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

func TestNewTerrainFromTiles(t *testing.T) {
	// 4x3 grid, cellSize=16 → 64x48 pixel map
	cfg := gamedata.MapConfig{
		MapID:      "test_map",
		GridWidth:  4,
		GridHeight: 3,
		CellSize:   16,
		Tiles: [][]string{
			{"", "", "", ""},          // row 0: all air
			{"", "dirt", "dirt", ""},   // row 1: middle two cells solid
			{"rock", "rock", "rock", "rock"}, // row 2: all solid
		},
	}

	// Need brick types loaded
	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_map": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = map[string]gamedata.BrickTypeConfig{
		"dirt": {BrickTypeID: "dirt", Destructible: true},
		"rock": {BrickTypeID: "rock", Destructible: false},
	}

	terrain := NewTerrain(cfg)

	// Row 0 (air) — pixel y=0..15 should be empty
	if terrain.IsSolid(0, 0) {
		t.Error("expected air at (0,0)")
	}
	if terrain.IsSolid(16, 8) {
		t.Error("expected air at (16,8)")
	}

	// Row 1, col 1 (dirt) — pixel y=16..31, x=16..31 should be solid
	if !terrain.IsSolid(20, 20) {
		t.Error("expected solid at (20,20) — dirt cell")
	}

	// Row 1, col 0 (air) — should not be solid
	if terrain.IsSolid(8, 20) {
		t.Error("expected air at (8,20)")
	}

	// Row 2 (all rock) — pixel y=32..47 should be solid
	if !terrain.IsSolid(0, 40) {
		t.Error("expected solid at (0,40) — rock cell")
	}
}

func TestDestroyCircleRespectsDestructible(t *testing.T) {
	cfg := gamedata.MapConfig{
		MapID:      "test_destroy",
		GridWidth:  4,
		GridHeight: 2,
		CellSize:   16,
		Tiles: [][]string{
			{"dirt", "dirt", "rock", "rock"},
			{"dirt", "dirt", "rock", "rock"},
		},
	}

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_destroy": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = map[string]gamedata.BrickTypeConfig{
		"dirt": {BrickTypeID: "dirt", Destructible: true},
		"rock": {BrickTypeID: "rock", Destructible: false},
	}

	terrain := NewTerrain(cfg)

	// Destroy at center of map with large radius — should destroy dirt but not rock
	terrain.DestroyCircle(32, 16, 50)

	// Dirt area (col 0-1) should be destroyed
	if terrain.IsSolid(8, 8) {
		t.Error("expected dirt at (8,8) to be destroyed")
	}

	// Rock area (col 2-3) should remain solid
	if !terrain.IsSolid(40, 8) {
		t.Error("expected rock at (40,8) to remain solid")
	}
}

func TestNewTerrainLegacyFallback(t *testing.T) {
	// No tiles → should use legacy hardcoded generation
	cfg := gamedata.MapConfig{
		MapID:      "grassland_valley",
		GridWidth:  100,
		GridHeight: 56,
		CellSize:   16,
		Tiles:      nil, // empty
		Width:      1600,
		Height:     900,
	}

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"grassland_valley": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = nil

	terrain := NewTerrain(cfg)

	// Bottom of map should be solid (legacy generation)
	if !terrain.IsSolid(800, 850) {
		t.Error("expected solid near bottom of map with legacy generation")
	}

	// Top of map should be air
	if terrain.IsSolid(800, 100) {
		t.Error("expected air near top of map with legacy generation")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/match/... -v -run TestNewTerrainFromTiles`
Expected: FAIL — `NewTerrain` has wrong signature, `BrickTypes` doesn't exist yet

- [ ] **Step 3: Add BrickTypeConfig to gamedata**

In `internal/game/gamedata/loader.go`, add:

```go
type BrickTypeConfig struct {
	BrickTypeID  string `yaml:"brickTypeId"`
	Destructible bool   `yaml:"destructible"`
}

// BrickTypes is loaded from config_brick_types table.
var BrickTypes map[string]BrickTypeConfig
```

Add loading in `LoadGameDataFromDB`, after maps section:

```go
	// 6. Load brick types
	BrickTypes = make(map[string]BrickTypeConfig)
	rows, err = db.Pool.Query(ctx, `SELECT brick_type_id, destructible FROM config_brick_types`)
	if err != nil {
		return fmt.Errorf("failed to query config_brick_types: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bt BrickTypeConfig
		if err := rows.Scan(&bt.BrickTypeID, &bt.Destructible); err != nil {
			return fmt.Errorf("failed to scan config_brick_types row: %w", err)
		}
		BrickTypes[bt.BrickTypeID] = bt
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_brick_types: %w", err)
	}
```

- [ ] **Step 4: Rewrite NewTerrain to accept MapConfig**

Replace `NewTerrain` in `terrain.go`:

```go
func NewTerrain(mapCfg gamedata.MapConfig) *Terrain {
	// Determine pixel dimensions
	var width, height int
	hasTiles := len(mapCfg.Tiles) > 0 && len(mapCfg.Tiles[0]) > 0

	if hasTiles {
		width = mapCfg.GridWidth * mapCfg.CellSize
		height = mapCfg.GridHeight * mapCfg.CellSize
	} else {
		// Legacy fallback
		width = mapCfg.Width
		height = mapCfg.Height
		if width == 0 {
			width = mapCfg.GridWidth * mapCfg.CellSize
		}
		if height == 0 {
			height = mapCfg.GridHeight * mapCfg.CellSize
		}
	}

	t := &Terrain{
		Width:            width,
		Height:           height,
		Mask:             make([]bool, width*height),
		DestructibleMask: make([]bool, width*height),
	}

	if hasTiles {
		// Build terrain from tilemap
		cs := mapCfg.CellSize
		for row := 0; row < len(mapCfg.Tiles); row++ {
			for col := 0; col < len(mapCfg.Tiles[row]); col++ {
				brickID := mapCfg.Tiles[row][col]
				if brickID == "" {
					continue
				}
				// Fill the cell_size x cell_size pixel block
				destructible := true
				if gamedata.BrickTypes != nil {
					if bt, ok := gamedata.BrickTypes[brickID]; ok {
						destructible = bt.Destructible
					}
				}
				for py := row * cs; py < (row+1)*cs && py < height; py++ {
					for px := col * cs; px < (col+1)*cs && px < width; px++ {
						idx := py*width + px
						t.Mask[idx] = true
						t.DestructibleMask[idx] = destructible
					}
				}
			}
		}
	} else {
		// Legacy: hardcoded terrain generation
		t.generateLegacyTerrain(mapCfg.MapID)
	}

	// Build terrain zones from legacy terrain layers (backward compat)
	if !hasTiles {
		specialTypes := map[string]bool{"lava": true, "ice": true, "fragile": true}
		for _, layer := range mapCfg.TerrainLayers {
			if !specialTypes[layer.Type] {
				continue
			}
			if len(layer.YRange) != 2 {
				continue
			}
			zone := TerrainZone{
				Type: layer.Type,
				MinX: 0,
				MaxX: float64(width),
				MinY: float64(layer.YRange[0]),
				MaxY: float64(layer.YRange[1]),
			}
			t.Zones = append(t.Zones, zone)
		}
	}

	return t
}

// generateLegacyTerrain creates terrain using the old hardcoded math functions.
func (t *Terrain) generateLegacyTerrain(mapID string) {
	for x := 0; x < t.Width; x++ {
		var terrainHeight float64
		switch mapID {
		case "frozen_peak":
			terrainHeight = 500 + 120*math.Sin(float64(x)*0.005) + 60*math.Cos(float64(x)*0.015)
		case "steel_base":
			if x > 300 && x < 600 {
				terrainHeight = 400
			} else if x > 1000 && x < 1300 {
				terrainHeight = 400
			} else {
				terrainHeight = 600
			}
		default: // grassland_valley
			terrainHeight = 550 + 100*math.Sin(float64(x)*0.003) + 40*math.Sin(float64(x)*0.01)
		}

		for y := 0; y < t.Height; y++ {
			idx := y*t.Width + x
			if float64(y) >= terrainHeight {
				t.Mask[idx] = true
				t.DestructibleMask[idx] = true // legacy terrain is always destructible
			}
		}
	}
}
```

- [ ] **Step 5: Add DestructibleMask field to Terrain struct**

Update the `Terrain` struct:

```go
type Terrain struct {
	Width            int
	Height           int
	Mask             []bool // true = solid, false = empty/destroyed
	DestructibleMask []bool // true = can be destroyed by explosions
	Zones            []TerrainZone
}
```

- [ ] **Step 6: Update DestroyCircle to check DestructibleMask**

```go
func (t *Terrain) DestroyCircle(cx, cy float64, radius float64) bool {
	icx := int(math.Round(cx))
	icy := int(math.Round(cy))
	ir := int(math.Round(radius))

	destroyedAny := false
	for y := icy - ir; y <= icy+ir; y++ {
		if y < 0 || y >= t.Height {
			continue
		}
		for x := icx - ir; x <= icx+ir; x++ {
			if x < 0 || x >= t.Width {
				continue
			}

			dx := float64(x - icx)
			dy := float64(y - icy)
			if dx*dx+dy*dy <= radius*radius {
				idx := y*t.Width + x
				if t.Mask[idx] && t.DestructibleMask[idx] {
					t.Mask[idx] = false
					destroyedAny = true
				}
			}
		}
	}

	return destroyedAny
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/game/match/... -v -run "TestNewTerrain|TestDestroy"`
Expected: All 3 tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/game/gamedata/loader.go internal/game/match/terrain.go \
  internal/game/match/terrain_test.go
git commit -m "feat(map-editor): rewrite terrain for tilemap with destructible mask"
```

---

### Task 8: Update Match & Model for New Terrain API

**Files:**
- Modify: `internal/game/match/match.go`
- Modify: `internal/game/match/model.go`
- Modify: `internal/game/match/physics_test.go`

- [ ] **Step 1: Update WindState to use float**

In `model.go`, change `WindState`:

```go
type WindState struct {
	Direction int     `json:"direction"` // -1: left, 0: no wind, 1: right
	Power     float64 `json:"power"`     // 0.0 to 4.0
}
```

- [ ] **Step 2: Update NewMatch to use new NewTerrain signature**

In `match.go`, change line 77 from:

```go
	terrain := NewTerrain(1600, 900, mapID)
```

To:

```go
	mapCfg := gamedata.MapConfig{
		MapID:  mapID,
		Width:  1600,
		Height: 900,
	}
	if gamedata.Data != nil {
		if cfg, ok := gamedata.Data.Maps[mapID]; ok {
			mapCfg = cfg
		}
	}
	terrain := NewTerrain(mapCfg)
```

- [ ] **Step 3: Update wind generation to use float**

In `match.go`, update the wind generation (around line 1256):

```go
	var windMin, windMax float64
	windMin = 0
	windMax = 4
	if mapCfg, ok := gamedata.Data.Maps[m.State.MapID]; ok && len(mapCfg.DefaultWindPowerRange) == 2 {
		windMin = mapCfg.DefaultWindPowerRange[0]
		windMax = mapCfg.DefaultWindPowerRange[1]
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m.State.Wind.Power = windMin + r.Float64()*(windMax-windMin)
	if m.State.Wind.Power < 0.01 {
		m.State.Wind.Direction = 0
	} else {
		if r.Float64() < 0.5 {
			m.State.Wind.Direction = -1
		} else {
			m.State.Wind.Direction = 1
		}
	}
```

- [ ] **Step 4: Update physics_test.go**

In `physics_test.go`, change line 32 from:

```go
	terrain := NewTerrain(1600, 900, "grassland_valley")
```

To:

```go
	terrain := NewTerrain(gamedata.MapConfig{
		MapID:  "grassland_valley",
		Width:  1600,
		Height: 900,
	})
```

- [ ] **Step 5: Verify all tests pass**

Run: `go test ./internal/game/match/... -v`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/game/match/match.go internal/game/match/model.go \
  internal/game/match/physics_test.go
git commit -m "feat(map-editor): update match to use new terrain API and float wind"
```

---

### Task 9: Update Admin Seed & YAML Config

**Files:**
- Modify: `internal/admin/seed.go`
- Modify: `configs/maps.yaml`

- [ ] **Step 1: Update seed.go to include brick types seeding**

Add brick types seeding before maps in the `SeedConfigFromYAML` function. Add after the items seeding section:

```go
	// Seed brick types
	brickQuery := `INSERT INTO config_brick_types (brick_type_id, name, destructible)
		VALUES ($1, $2, $3)
		ON CONFLICT (brick_type_id) DO NOTHING`
	defaultBricks := []struct {
		ID           string
		Name         string
		Destructible bool
	}{
		{"dirt", "Dirt", true},
		{"rock", "Rock", false},
		{"ice", "Ice", true},
		{"lava", "Lava", false},
		{"fragile", "Fragile", true},
	}
	for _, b := range defaultBricks {
		if _, err := db.Pool.Exec(ctx, brickQuery, b.ID, b.Name, b.Destructible); err != nil {
			return fmt.Errorf("insert brick type %s: %w", b.ID, err)
		}
	}
	observability.Log.Info().Int("count", len(defaultBricks)).Msg("seeded config_brick_types")
```

Also update the map seeding query to include the new columns:

```go
	mapQuery := `INSERT INTO config_maps
		(map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points,
		 grid_width, grid_height, cell_size, tiles)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (map_id) DO NOTHING`
	for _, m := range data.Maps {
		windRange, err := json.Marshal(m.DefaultWindPowerRange)
		if err != nil {
			return fmt.Errorf("marshal wind range for map %s: %w", m.MapID, err)
		}
		terrainLayers, err := json.Marshal(m.TerrainLayers)
		if err != nil {
			return fmt.Errorf("marshal terrain layers for map %s: %w", m.MapID, err)
		}
		spawnPoints, err := json.Marshal(m.SpawnPoints)
		if err != nil {
			return fmt.Errorf("marshal spawn points for map %s: %w", m.MapID, err)
		}
		tiles, err := json.Marshal(m.Tiles)
		if err != nil {
			tiles = []byte("[]")
		}
		gridWidth := m.GridWidth
		if gridWidth == 0 {
			gridWidth = 100
		}
		gridHeight := m.GridHeight
		if gridHeight == 0 {
			gridHeight = 56
		}
		cellSize := m.CellSize
		if cellSize == 0 {
			cellSize = 16
		}
		if _, err := db.Pool.Exec(ctx, mapQuery,
			m.MapID, m.Name, m.Width, m.Height, windRange, terrainLayers, spawnPoints,
			gridWidth, gridHeight, cellSize, tiles,
		); err != nil {
			return fmt.Errorf("insert map %s: %w", m.MapID, err)
		}
	}
```

- [ ] **Step 2: Update configs/maps.yaml to new format**

Keep backward compatible (include both old and new fields for now):

```yaml
- mapId: grassland_valley
  name: Grassland Valley
  width: 1600
  height: 900
  gridWidth: 100
  gridHeight: 56
  cellSize: 16
  defaultWindPowerRange: [0.0, 4.0]
  terrainLayers:
    - type: dirt
      hardness: 20
      yRange: [550, 750]
    - type: rock
      hardness: 60
      yRange: [750, 900]
  tiles: []
  spawnPoints:
    - x: 200
      y: 400
    - x: 1400
      y: 400
    - x: 400
      y: 500
    - x: 1200
      y: 500

- mapId: frozen_peak
  name: Frozen Peak
  width: 1600
  height: 900
  gridWidth: 100
  gridHeight: 56
  cellSize: 16
  defaultWindPowerRange: [1.0, 4.0]
  terrainLayers:
    - type: ice
      hardness: 10
      yRange: [450, 650]
    - type: rock
      hardness: 70
      yRange: [650, 900]
  tiles: []
  spawnPoints:
    - x: 300
      y: 350
    - x: 1300
      y: 350
    - x: 500
      y: 450
    - x: 1100
      y: 450

- mapId: steel_base
  name: Steel Base
  width: 1600
  height: 900
  gridWidth: 100
  gridHeight: 56
  cellSize: 16
  defaultWindPowerRange: [0.0, 2.0]
  terrainLayers:
    - type: lava
      hardness: 90
      yRange: [700, 900]
    - type: fragile
      hardness: 30
      yRange: [500, 700]
  tiles: []
  spawnPoints:
    - x: 250
      y: 300
    - x: 1350
      y: 300
    - x: 450
      y: 400
    - x: 1150
      y: 400
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/admin/seed.go configs/maps.yaml
git commit -m "feat(map-editor): update seed and YAML config for tilemap"
```

---

### Task 10: Update Admin ConfigMap for New Columns

**Files:**
- Modify: `internal/admin/repository.go`
- Modify: `internal/admin/handlers_config.go`

- [ ] **Step 1: Update ConfigMap struct**

In `repository.go`, update the `ConfigMap` struct:

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
}
```

- [ ] **Step 2: Update GetMaps query**

```go
func (r *Repository) GetMaps(ctx context.Context) ([]ConfigMap, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT map_id, name, width, height, grid_width, grid_height, cell_size,
		        default_wind_power_range, terrain_layers, spawn_points, tiles, description
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
			&m.Tiles, &m.Description); err != nil {
			return nil, fmt.Errorf("scan map: %w", err)
		}
		maps = append(maps, m)
	}
	return maps, rows.Err()
}
```

- [ ] **Step 3: Update GetMap query**

```go
func (r *Repository) GetMap(ctx context.Context, id string) (*ConfigMap, error) {
	var m ConfigMap
	err := r.db.Pool.QueryRow(ctx,
		`SELECT map_id, name, width, height, grid_width, grid_height, cell_size,
		        default_wind_power_range, terrain_layers, spawn_points, tiles, description
		 FROM config_maps WHERE map_id = $1`, id).
		Scan(&m.MapID, &m.Name, &m.Width, &m.Height,
			&m.GridWidth, &m.GridHeight, &m.CellSize,
			&m.DefaultWindPowerRange, &m.TerrainLayers, &m.SpawnPoints,
			&m.Tiles, &m.Description)
	if err != nil {
		return nil, fmt.Errorf("get map %s: %w", id, err)
	}
	return &m, nil
}
```

- [ ] **Step 4: Update UpsertMap query**

```go
func (r *Repository) UpsertMap(ctx context.Context, m *ConfigMap) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_maps
		 (map_id, name, width, height, grid_width, grid_height, cell_size,
		  default_wind_power_range, terrain_layers, spawn_points, tiles, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, CURRENT_TIMESTAMP)
		 ON CONFLICT (map_id) DO UPDATE SET
		   name=EXCLUDED.name, width=EXCLUDED.width, height=EXCLUDED.height,
		   grid_width=EXCLUDED.grid_width, grid_height=EXCLUDED.grid_height,
		   cell_size=EXCLUDED.cell_size,
		   default_wind_power_range=EXCLUDED.default_wind_power_range,
		   terrain_layers=EXCLUDED.terrain_layers, spawn_points=EXCLUDED.spawn_points,
		   tiles=EXCLUDED.tiles,
		   description=EXCLUDED.description, updated_at=CURRENT_TIMESTAMP`,
		m.MapID, m.Name, m.Width, m.Height, m.GridWidth, m.GridHeight, m.CellSize,
		m.DefaultWindPowerRange, m.TerrainLayers, m.SpawnPoints, m.Tiles, m.Description)
	if err != nil {
		return fmt.Errorf("upsert map %s: %w", m.MapID, err)
	}
	return nil
}
```

- [ ] **Step 5: Update handleConfigEdit for maps**

In `handlers_config.go`, update the maps case in `handleConfigEdit` to include new fields:

```go
		case "maps":
			title = "Map"
			var m ConfigMap
			if !isNew {
				found, err := s.repo.GetMap(ctx, id)
				if err != nil {
					http.Redirect(w, r, "/maps?error=Map+not+found", http.StatusSeeOther)
					return
				}
				m = *found
				originalID = m.MapID
			}
			fields = []FieldDef{
				{Name: "map_id", Label: "Map ID", Type: "text", Description: "Unique identifier (e.g. map_valley)", Value: m.MapID},
				{Name: "name", Label: "Name", Type: "text", Description: "Display name", Value: m.Name},
				{Name: "grid_width", Label: "Grid Width", Type: "number", Step: "1", Description: "Number of horizontal cells", Value: m.GridWidth},
				{Name: "grid_height", Label: "Grid Height", Type: "number", Step: "1", Description: "Number of vertical cells", Value: m.GridHeight},
				{Name: "cell_size", Label: "Cell Size", Type: "number", Step: "1", Description: "Pixel size of each cell", Value: m.CellSize},
				{Name: "default_wind_power_range", Label: "Wind Power Range (JSON)", Type: "textarea", Description: "JSON array [min, max] for wind power range (float)", Value: jsonString(m.DefaultWindPowerRange)},
				{Name: "spawn_points", Label: "Spawn Points (JSON)", Type: "textarea", Description: "JSON array of spawn point coordinates", Value: jsonString(m.SpawnPoints)},
				{Name: "description", Label: "Description", Type: "textarea", Description: "Map description", Value: m.Description},
			}
```

- [ ] **Step 6: Update handleConfigSave for maps**

In `handlers_config.go`, update the maps case in `handleConfigSave`:

```go
		case "maps":
			gridWidth := formInt(r, "grid_width")
			if gridWidth == 0 {
				gridWidth = 100
			}
			gridHeight := formInt(r, "grid_height")
			if gridHeight == 0 {
				gridHeight = 56
			}
			cellSize := formInt(r, "cell_size")
			if cellSize == 0 {
				cellSize = 16
			}
			m := &ConfigMap{
				MapID:                 strings.TrimSpace(r.FormValue("map_id")),
				Name:                  r.FormValue("name"),
				Width:                 gridWidth * cellSize,
				Height:                gridHeight * cellSize,
				GridWidth:             gridWidth,
				GridHeight:            gridHeight,
				CellSize:              cellSize,
				DefaultWindPowerRange: json.RawMessage(r.FormValue("default_wind_power_range")),
				TerrainLayers:         json.RawMessage("[]"),
				SpawnPoints:           json.RawMessage(r.FormValue("spawn_points")),
				Tiles:                 json.RawMessage("[]"),
				Description:           r.FormValue("description"),
			}
			if m.MapID == "" {
				http.Redirect(w, r, "/maps/edit?error=Map+ID+is+required", http.StatusSeeOther)
				return
			}
			if err := s.repo.UpsertMap(ctx, m); err != nil {
				observability.Log.Error().Err(err).Msg("failed to upsert map")
				http.Redirect(w, r, "/maps?error=Failed+to+save+map", http.StatusSeeOther)
				return
			}
```

- [ ] **Step 7: Update config_list.html for maps columns**

Replace the maps section in `config_list.html`:

```html
{{else if eq .ConfigType "maps"}}
<thead><tr><th>ID</th><th>Name</th><th>Grid</th><th>Cell Size</th><th>Description</th><th>Actions</th></tr></thead>
<tbody>
{{range .Items}}
<tr>
    <td>{{.MapID}}</td><td>{{.Name}}</td><td>{{.GridWidth}}x{{.GridHeight}}</td><td>{{.CellSize}}px</td><td>{{.Description}}</td>
    <td>
        <a href="/maps/editor?id={{.MapID}}" class="btn btn-success btn-sm">Editor</a>
        <a href="/maps/edit?id={{.MapID}}" class="btn btn-primary btn-sm">Edit</a>
        <form method="POST" action="/maps/delete" style="display:inline" onsubmit="return confirm('Delete this map?')">
            <input type="hidden" name="id" value="{{.MapID}}">
            <button type="submit" class="btn btn-danger btn-sm">Delete</button>
        </form>
    </td>
</tr>
{{end}}
</tbody>
```

- [ ] **Step 8: Verify it compiles**

Run: `go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 9: Run all tests**

Run: `go test ./...`
Expected: All tests PASS

- [ ] **Step 10: Commit**

```bash
git add internal/admin/repository.go internal/admin/handlers_config.go \
  internal/admin/templates/config_list.html
git commit -m "feat(map-editor): update admin map CRUD for tilemap columns"
```

---

### Task 11: End-to-End Smoke Test

**Files:** None (manual verification)

- [ ] **Step 1: Start infrastructure**

Run: `docker-compose up -d`

- [ ] **Step 2: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration 007 applied

- [ ] **Step 3: Start admin server**

Run: `go run cmd/admin/main.go`

- [ ] **Step 4: Verify brick types page**

Open `http://localhost:9000/brick-types` in browser.
Expected: List of 5 default brick types (dirt, rock, ice, lava, fragile)

- [ ] **Step 5: Verify map editor**

Open `http://localhost:9000/maps`, click "Editor" on a map.
Expected: Canvas editor loads with grid, brick palette, tools

- [ ] **Step 6: Test paint and save**

Paint some bricks on the canvas, click Save.
Expected: "Saved!" alert. Reload page, tiles persist.

- [ ] **Step 7: Test export**

Click "Export YAML".
Expected: YAML file downloads with tilemap data.

- [ ] **Step 8: Run all tests one final time**

Run: `go test ./... -v`
Expected: All tests PASS

- [ ] **Step 9: Final commit**

```bash
git add -A
git commit -m "feat(map-editor): complete map editor implementation"
```
