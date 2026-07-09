# Map Editor v2 — Polygon Brick Borders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade brick types from simple squares to polygon-bordered shapes with image support. Change tiles from string IDs to integer IDs. Add a 16x16 pixel editor for designing brick borders.

**Architecture:** Recreate `config_brick_types` table with SERIAL PK + border JSONB + image_id. Update tiles format to `[][]int`. Add polygon scanline fill for terrain mask generation. Add brick editor UI with 16x16 canvas.

**Tech Stack:** Go (chi router, pgx), HTML5 Canvas + Vanilla JS, PostgreSQL (JSONB)

---

### File Structure

**New files:**
- `migrations/008_brick_border_v2.up.sql` — DB migration
- `migrations/008_brick_border_v2.down.sql` — Rollback
- `internal/admin/templates/brick_editor.html` — 16x16 pixel brick editor page
- `internal/admin/static/brick_editor.js` — Brick editor JavaScript
- `internal/game/match/polygon.go` — Scanline polygon fill for mask generation

**Modified files:**
- `internal/admin/repository.go` — Update ConfigBrickType struct, queries
- `internal/admin/handlers_map_editor.go` — Update brick handlers, add editor page
- `internal/admin/templates/brick_types.html` — Update list columns
- `internal/admin/templates/brick_type_edit.html` — Remove (replaced by brick_editor)
- `internal/admin/server.go` — Update routes
- `internal/admin/static/map_editor.js` — Tiles as int, color from brick type
- `internal/admin/seed.go` — Update brick seeding for new schema
- `internal/game/gamedata/loader.go` — Update BrickTypeConfig, MapConfig (int tiles)
- `internal/game/match/terrain.go` — Polygon-based mask generation
- `internal/game/match/terrain_test.go` — Update tests for polygon borders

---

### Task 1: Database Migration — Brick Types v2

**Files:**
- Create: `migrations/008_brick_border_v2.up.sql`
- Create: `migrations/008_brick_border_v2.down.sql`
- Modify: `cmd/migrate/main.go`

- [ ] **Step 1: Write the up migration**

```sql
-- migrations/008_brick_border_v2.up.sql

-- Recreate brick types with SERIAL PK, border, image_id, color
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

-- Reset tiles to empty since format changes from string to int
UPDATE config_maps SET tiles = '[]'::jsonb;
```

- [ ] **Step 2: Write the down migration**

```sql
-- migrations/008_brick_border_v2.down.sql

DROP TABLE IF EXISTS config_brick_types;
CREATE TABLE config_brick_types (
    brick_type_id TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    destructible  BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

UPDATE config_maps SET tiles = '[]'::jsonb;
```

- [ ] **Step 3: Add migration 008 to migrate tool**

In `cmd/migrate/main.go`, add to the migrations list:
```go
		filepath.Join("migrations", "008_brick_border_v2.up.sql"),
```

- [ ] **Step 4: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration 008 applied successfully

- [ ] **Step 5: Commit**

```bash
git add migrations/008_brick_border_v2.up.sql migrations/008_brick_border_v2.down.sql \
  cmd/migrate/main.go
git commit -m "feat(map-editor-v2): migrate brick types to serial PK with border and image"
```

---

### Task 2: Update Repository — Brick Types v2

**Files:**
- Modify: `internal/admin/repository.go`

- [ ] **Step 1: Update ConfigBrickType struct**

Replace the existing `ConfigBrickType` struct:

```go
// ConfigBrickType represents a row in config_brick_types (v2 with polygon border).
type ConfigBrickType struct {
	BrickTypeID  int
	Name         string
	ImageID      string
	Destructible bool
	Border       json.RawMessage // BrickBorder JSON
	Color        string
}
```

- [ ] **Step 2: Update GetBrickTypes**

```go
func (r *Repository) GetBrickTypes(ctx context.Context) ([]ConfigBrickType, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT brick_type_id, name, image_id, destructible, border, color
		 FROM config_brick_types ORDER BY brick_type_id`)
	if err != nil {
		return nil, fmt.Errorf("query brick types: %w", err)
	}
	defer rows.Close()

	var types []ConfigBrickType
	for rows.Next() {
		var bt ConfigBrickType
		if err := rows.Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible,
			&bt.Border, &bt.Color); err != nil {
			return nil, fmt.Errorf("scan brick type: %w", err)
		}
		types = append(types, bt)
	}
	return types, rows.Err()
}
```

- [ ] **Step 3: Update GetBrickType**

```go
func (r *Repository) GetBrickType(ctx context.Context, id int) (*ConfigBrickType, error) {
	var bt ConfigBrickType
	err := r.db.Pool.QueryRow(ctx,
		`SELECT brick_type_id, name, image_id, destructible, border, color
		 FROM config_brick_types WHERE brick_type_id = $1`, id).
		Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible,
			&bt.Border, &bt.Color)
	if err != nil {
		return nil, fmt.Errorf("get brick type %d: %w", id, err)
	}
	return &bt, nil
}
```

- [ ] **Step 4: Replace UpsertBrickType with InsertBrickType and UpdateBrickType**

Since brick_type_id is SERIAL (auto-generated), insert doesn't provide an ID:

```go
// InsertBrickType creates a new brick type and returns the generated ID.
func (r *Repository) InsertBrickType(ctx context.Context, bt *ConfigBrickType) (int, error) {
	var id int
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO config_brick_types (name, image_id, destructible, border, color)
		 VALUES ($1, $2, $3, $4, $5) RETURNING brick_type_id`,
		bt.Name, bt.ImageID, bt.Destructible, bt.Border, bt.Color).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert brick type: %w", err)
	}
	return id, nil
}

// UpdateBrickType updates an existing brick type.
func (r *Repository) UpdateBrickType(ctx context.Context, bt *ConfigBrickType) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE config_brick_types SET name=$1, image_id=$2, destructible=$3,
		 border=$4, color=$5, updated_at=CURRENT_TIMESTAMP
		 WHERE brick_type_id=$6`,
		bt.Name, bt.ImageID, bt.Destructible, bt.Border, bt.Color, bt.BrickTypeID)
	if err != nil {
		return fmt.Errorf("update brick type %d: %w", bt.BrickTypeID, err)
	}
	return nil
}
```

- [ ] **Step 5: Update DeleteBrickType to accept int**

```go
func (r *Repository) DeleteBrickType(ctx context.Context, id int) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_brick_types WHERE brick_type_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete brick type %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 6: Verify it compiles**

Run: `go build ./internal/admin/...`
Note: Handlers will break temporarily due to type changes — fixed in Task 3.

- [ ] **Step 7: Commit**

```bash
git add internal/admin/repository.go
git commit -m "feat(map-editor-v2): update brick types repository for serial PK + border"
```

---

### Task 3: Update Brick Types Handlers

**Files:**
- Modify: `internal/admin/handlers_map_editor.go`
- Modify: `internal/admin/templates/brick_types.html`
- Modify: `internal/admin/server.go`

- [ ] **Step 1: Rewrite brick type handlers for new schema**

Replace all brick type handlers in `handlers_map_editor.go`. Key changes: BrickTypeID is now `int`, save uses insert vs update, add editor page handler.

```go
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

func (s *Server) handleBrickTypeEditor(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.URL.Query().Get("id")
	isNew := idStr == ""

	var bt ConfigBrickType
	if !isNew {
		id, _ := strconv.Atoi(idStr)
		found, err := s.repo.GetBrickType(ctx, id)
		if err != nil {
			http.Redirect(w, r, "/brick-types?error=Brick+type+not+found", http.StatusSeeOther)
			return
		}
		bt = *found
	}

	borderJSON := string(bt.Border)
	if borderJSON == "" || borderJSON == "null" {
		borderJSON = `{"top":[{"x":0,"y":16},{"x":16,"y":16}],"right":[{"x":16,"y":16},{"x":16,"y":0}],"bottom":[{"x":16,"y":0},{"x":0,"y":0}],"left":[{"x":0,"y":0},{"x":0,"y":16}]}`
	}

	s.render(w, "brick_editor", map[string]interface{}{
		"ActivePage": "brick-types",
		"BrickType":  bt,
		"BorderJSON": borderJSON,
		"IsNew":      isNew,
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleBrickTypeSave(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	// Support JSON API (from editor JS)
	if strings.Contains(contentType, "application/json") {
		var body struct {
			BrickTypeID  int             `json:"brickTypeId"`
			Name         string          `json:"name"`
			ImageID      string          `json:"imageId"`
			Destructible bool            `json:"destructible"`
			Border       json.RawMessage `json:"border"`
			Color        string          `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		bt := &ConfigBrickType{
			BrickTypeID:  body.BrickTypeID,
			Name:         body.Name,
			ImageID:      body.ImageID,
			Destructible: body.Destructible,
			Border:       body.Border,
			Color:        body.Color,
		}

		ctx := r.Context()
		if bt.BrickTypeID == 0 {
			id, err := s.repo.InsertBrickType(ctx, bt)
			if err != nil {
				observability.Log.Error().Err(err).Msg("failed to insert brick type")
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": id})
		} else {
			if err := s.repo.UpdateBrickType(ctx, bt); err != nil {
				observability.Log.Error().Err(err).Msg("failed to update brick type")
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": bt.BrickTypeID})
		}
		return
	}

	// Fallback: form submit (shouldn't normally be used)
	http.Redirect(w, r, "/brick-types", http.StatusSeeOther)
}

func (s *Server) handleBrickTypeDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/brick-types?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	id, _ := strconv.Atoi(r.FormValue("id"))
	if id == 0 {
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

- [ ] **Step 2: Update brick_types.html list template**

```html
{{define "content"}}
<div class="top-bar">
    <h1>Brick Types</h1>
    <a href="/brick-types/editor" class="btn btn-primary">+ Add Brick</a>
</div>
{{if .Flash}}<div class="flash flash-success">{{.Flash}}</div>{{end}}
{{if .Error}}<div class="flash flash-error">{{.Error}}</div>{{end}}
<div class="card">
<table>
<thead><tr><th>ID</th><th>Name</th><th>Color</th><th>Image ID</th><th>Destructible</th><th>Actions</th></tr></thead>
<tbody>
{{range .Items}}
<tr>
    <td>{{.BrickTypeID}}</td>
    <td>{{.Name}}</td>
    <td><span style="display:inline-block;width:20px;height:20px;background:{{.Color}};border:1px solid #ccc;border-radius:3px;vertical-align:middle"></span> {{.Color}}</td>
    <td>{{.ImageID}}</td>
    <td>{{if .Destructible}}<span class="badge badge-success">Yes</span>{{else}}<span class="badge badge-danger">No</span>{{end}}</td>
    <td>
        <a href="/brick-types/editor?id={{.BrickTypeID}}" class="btn btn-primary btn-sm">Edit</a>
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

- [ ] **Step 3: Update routes in server.go**

Replace `r.Get("/brick-types/edit", ...)` with `r.Get("/brick-types/editor", ...)`:

```go
	r.Get("/brick-types", s.handleBrickTypesList)
	r.Get("/brick-types/editor", s.handleBrickTypeEditor)
	r.Post("/brick-types/save", s.handleBrickTypeSave)
	r.Post("/brick-types/delete", s.handleBrickTypeDelete)
```

- [ ] **Step 4: Delete brick_type_edit.html (replaced by brick_editor)**

Remove `internal/admin/templates/brick_type_edit.html` — no longer needed.

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/admin/...`

- [ ] **Step 6: Commit**

```bash
git add internal/admin/handlers_map_editor.go internal/admin/templates/brick_types.html \
  internal/admin/server.go
git rm internal/admin/templates/brick_type_edit.html
git commit -m "feat(map-editor-v2): update brick type handlers for serial PK + border"
```

---

### Task 4: Brick Editor UI — 16x16 Polygon Canvas

**Files:**
- Create: `internal/admin/templates/brick_editor.html`
- Create: `internal/admin/static/brick_editor.js`

- [ ] **Step 1: Create brick_editor.html template**

```html
{{define "content"}}
<style>
.brick-editor-container { display: flex; gap: 20px; }
.brick-editor-canvas-wrap { position: relative; }
.brick-editor-canvas-wrap canvas { border: 1px solid #ccc; cursor: crosshair; image-rendering: pixelated; }
.brick-editor-props { width: 280px; }
.brick-preview { margin-top: 16px; }
.brick-preview canvas { border: 1px solid #ccc; }
.edge-selector { display: flex; gap: 4px; margin-bottom: 8px; }
.edge-btn { padding: 4px 10px; border: 1px solid #ccc; border-radius: 4px; background: #fff; cursor: pointer; font-size: 12px; }
.edge-btn.active { background: #4a6cf7; color: #fff; border-color: #4a6cf7; }
.point-list { max-height: 200px; overflow-y: auto; font-size: 12px; font-family: monospace; }
.point-item { padding: 2px 4px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #eee; }
</style>

<div class="top-bar">
    <h1>{{if .IsNew}}New{{else}}Edit{{end}} Brick Type</h1>
    <div>
        <a href="/brick-types" class="btn btn-primary">Back to List</a>
        <button class="btn btn-success" onclick="brickEditor.save()">Save</button>
    </div>
</div>
{{if .Error}}<div class="flash flash-error">{{.Error}}</div>{{end}}

<div class="brick-editor-container">
    <div>
        <div class="card brick-editor-props">
            <div class="form-group">
                <label>Name</label>
                <input type="text" id="propName" value="{{.BrickType.Name}}">
            </div>
            <div class="form-group">
                <label>Image ID</label>
                <input type="text" id="propImageId" value="{{.BrickType.ImageID}}">
                <div class="desc">Sprite reference (loaded by client)</div>
            </div>
            <div class="form-group">
                <label>Color</label>
                <input type="color" id="propColor" value="{{if .BrickType.Color}}{{.BrickType.Color}}{{else}}#8B4513{{end}}">
            </div>
            <div class="form-group">
                <label>
                    <input type="checkbox" id="propDestructible" {{if .BrickType.Destructible}}checked{{end}}>
                    Destructible
                </label>
            </div>
        </div>

        <div class="card" style="margin-top:12px">
            <h3 style="font-size:14px;margin-bottom:8px">Edge</h3>
            <div class="edge-selector">
                <button class="edge-btn active" onclick="brickEditor.setEdge('top',this)">Top</button>
                <button class="edge-btn" onclick="brickEditor.setEdge('right',this)">Right</button>
                <button class="edge-btn" onclick="brickEditor.setEdge('bottom',this)">Bottom</button>
                <button class="edge-btn" onclick="brickEditor.setEdge('left',this)">Left</button>
            </div>
            <div class="point-list" id="pointList"></div>
            <button class="btn btn-danger btn-sm" style="margin-top:8px" onclick="brickEditor.resetEdge()">Reset Edge</button>
        </div>
    </div>

    <div>
        <div class="brick-editor-canvas-wrap">
            <canvas id="brickCanvas" width="400" height="400"></canvas>
        </div>
        <div class="brick-preview card" style="margin-top:12px">
            <h3 style="font-size:14px;margin-bottom:8px">Preview (actual size)</h3>
            <canvas id="previewCanvas" width="48" height="48"></canvas>
        </div>
    </div>
</div>

<script>
const BRICK_ID = {{.BrickType.BrickTypeID}};
const BORDER_DATA = {{.BorderJSON}};
</script>
<script src="/static/brick_editor.js"></script>
{{end}}
```

- [ ] **Step 2: Create brick_editor.js**

```javascript
(function() {
    'use strict';

    var CELL = 16;
    var SCALE = 25; // 400px / 16 = 25px per pixel
    var DEFAULT_BORDER = {
        top:    [{x:0,y:16},{x:16,y:16}],
        right:  [{x:16,y:16},{x:16,y:0}],
        bottom: [{x:16,y:0},{x:0,y:0}],
        left:   [{x:0,y:0},{x:0,y:16}]
    };

    function BrickEditor() {
        this.canvas = document.getElementById('brickCanvas');
        this.ctx = this.canvas.getContext('2d');
        this.previewCanvas = document.getElementById('previewCanvas');
        this.previewCtx = this.previewCanvas.getContext('2d');

        this.border = JSON.parse(JSON.stringify(BORDER_DATA || DEFAULT_BORDER));
        this.activeEdge = 'top';
        this.dragIndex = -1;

        this.init();
    }

    BrickEditor.prototype.init = function() {
        var self = this;
        this.canvas.addEventListener('mousedown', function(e) { self.onMouseDown(e); });
        this.canvas.addEventListener('mousemove', function(e) { self.onMouseMove(e); });
        this.canvas.addEventListener('mouseup', function(e) { self.onMouseUp(e); });
        this.canvas.addEventListener('dblclick', function(e) { self.onDblClick(e); });
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.screenToGrid = function(e) {
        var rect = this.canvas.getBoundingClientRect();
        var sx = e.clientX - rect.left;
        var sy = e.clientY - rect.top;
        // Convert to brick coords (origin bottom-left, y-up)
        return {
            x: Math.round(sx / SCALE * 2) / 2, // snap to 0.5
            y: Math.round((CELL - sy / SCALE) * 2) / 2
        };
    };

    BrickEditor.prototype.gridToScreen = function(pt) {
        return {
            x: pt.x * SCALE,
            y: (CELL - pt.y) * SCALE
        };
    };

    BrickEditor.prototype.findNearestPoint = function(gx, gy) {
        var points = this.border[this.activeEdge];
        var best = -1, bestDist = Infinity;
        for (var i = 0; i < points.length; i++) {
            var dx = points[i].x - gx;
            var dy = points[i].y - gy;
            var d = dx * dx + dy * dy;
            if (d < bestDist && d < 4) { // within ~2 grid units
                bestDist = d;
                best = i;
            }
        }
        return best;
    };

    BrickEditor.prototype.onMouseDown = function(e) {
        var g = this.screenToGrid(e);
        var idx = this.findNearestPoint(g.x, g.y);
        if (idx >= 0) {
            this.dragIndex = idx;
        }
    };

    BrickEditor.prototype.onMouseMove = function(e) {
        if (this.dragIndex >= 0) {
            var g = this.screenToGrid(e);
            g.x = Math.max(0, Math.min(CELL, g.x));
            g.y = Math.max(0, Math.min(CELL, g.y));
            this.border[this.activeEdge][this.dragIndex] = g;
            this.renderPointList();
            this.render();
        }
    };

    BrickEditor.prototype.onMouseUp = function(e) {
        this.dragIndex = -1;
    };

    BrickEditor.prototype.onDblClick = function(e) {
        var g = this.screenToGrid(e);
        g.x = Math.max(0, Math.min(CELL, g.x));
        g.y = Math.max(0, Math.min(CELL, g.y));

        // Check if near existing point — if so, delete it (unless it's an endpoint)
        var idx = this.findNearestPoint(g.x, g.y);
        var points = this.border[this.activeEdge];
        if (idx >= 0 && points.length > 2) {
            points.splice(idx, 1);
        } else if (idx < 0) {
            // Add point — insert between two nearest consecutive points
            var bestInsert = points.length - 1;
            var bestDist = Infinity;
            for (var i = 0; i < points.length - 1; i++) {
                var mx = (points[i].x + points[i+1].x) / 2;
                var my = (points[i].y + points[i+1].y) / 2;
                var d = (g.x - mx) * (g.x - mx) + (g.y - my) * (g.y - my);
                if (d < bestDist) {
                    bestDist = d;
                    bestInsert = i + 1;
                }
            }
            points.splice(bestInsert, 0, g);
        }
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.setEdge = function(edge, btn) {
        this.activeEdge = edge;
        this.dragIndex = -1;
        document.querySelectorAll('.edge-btn').forEach(function(b) { b.classList.remove('active'); });
        if (btn) btn.classList.add('active');
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.resetEdge = function() {
        this.border[this.activeEdge] = JSON.parse(JSON.stringify(DEFAULT_BORDER[this.activeEdge]));
        this.renderPointList();
        this.render();
    };

    BrickEditor.prototype.renderPointList = function() {
        var list = document.getElementById('pointList');
        var points = this.border[this.activeEdge];
        var self = this;
        list.innerHTML = '';
        points.forEach(function(p, i) {
            var div = document.createElement('div');
            div.className = 'point-item';
            div.innerHTML = '<span>(' + p.x + ', ' + p.y + ')</span>';
            if (points.length > 2) {
                var btn = document.createElement('button');
                btn.className = 'btn btn-danger btn-sm';
                btn.style.cssText = 'padding:1px 5px;font-size:10px';
                btn.textContent = 'X';
                btn.onclick = function() {
                    points.splice(i, 1);
                    self.renderPointList();
                    self.render();
                };
                div.appendChild(btn);
            }
            list.appendChild(div);
        });
    };

    BrickEditor.prototype.render = function() {
        var ctx = this.ctx;
        var w = this.canvas.width;
        var h = this.canvas.height;

        ctx.clearRect(0, 0, w, h);

        // Background grid
        ctx.fillStyle = '#f0f0f0';
        ctx.fillRect(0, 0, w, h);
        ctx.strokeStyle = '#ddd';
        ctx.lineWidth = 1;
        for (var i = 0; i <= CELL; i++) {
            ctx.beginPath();
            ctx.moveTo(i * SCALE, 0);
            ctx.lineTo(i * SCALE, h);
            ctx.stroke();
            ctx.beginPath();
            ctx.moveTo(0, i * SCALE);
            ctx.lineTo(w, i * SCALE);
            ctx.stroke();
        }

        // Draw filled polygon
        var color = document.getElementById('propColor').value || '#8B4513';
        ctx.fillStyle = color + '44';
        ctx.beginPath();
        var edges = ['bottom', 'right', 'top', 'left'];
        var first = true;
        for (var e = 0; e < edges.length; e++) {
            var pts = this.border[edges[e]];
            for (var p = 0; p < pts.length; p++) {
                var s = this.gridToScreen(pts[p]);
                if (first) { ctx.moveTo(s.x, s.y); first = false; }
                else ctx.lineTo(s.x, s.y);
            }
        }
        ctx.closePath();
        ctx.fill();

        // Draw all edges
        var edgeColors = {top: '#e74c3c', right: '#27ae60', bottom: '#3498db', left: '#f39c12'};
        for (var e = 0; e < edges.length; e++) {
            var edge = edges[e];
            var pts = this.border[edge];
            ctx.strokeStyle = edge === this.activeEdge ? edgeColors[edge] : '#999';
            ctx.lineWidth = edge === this.activeEdge ? 3 : 1;
            ctx.beginPath();
            for (var p = 0; p < pts.length; p++) {
                var s = this.gridToScreen(pts[p]);
                if (p === 0) ctx.moveTo(s.x, s.y);
                else ctx.lineTo(s.x, s.y);
            }
            ctx.stroke();

            // Draw points for active edge
            if (edge === this.activeEdge) {
                for (var p = 0; p < pts.length; p++) {
                    var s = this.gridToScreen(pts[p]);
                    ctx.fillStyle = edgeColors[edge];
                    ctx.beginPath();
                    ctx.arc(s.x, s.y, 5, 0, Math.PI * 2);
                    ctx.fill();
                    ctx.fillStyle = '#fff';
                    ctx.font = '10px monospace';
                    ctx.fillText(p, s.x + 7, s.y - 5);
                }
            }
        }

        // Axis labels
        ctx.fillStyle = '#999';
        ctx.font = '10px monospace';
        ctx.fillText('(0,0)', 2, h - 4);
        ctx.fillText('x→', w / 2, h - 4);
        ctx.fillText('y↑', 2, h / 2);

        this.renderPreview();
    };

    BrickEditor.prototype.renderPreview = function() {
        var ctx = this.previewCtx;
        var size = 48;
        ctx.clearRect(0, 0, size, size);

        var color = document.getElementById('propColor').value || '#8B4513';
        var offset = (size - CELL) / 2;

        ctx.fillStyle = color;
        ctx.beginPath();
        var edges = ['bottom', 'right', 'top', 'left'];
        var first = true;
        for (var e = 0; e < edges.length; e++) {
            var pts = this.border[edges[e]];
            for (var p = 0; p < pts.length; p++) {
                var px = offset + pts[p].x;
                var py = offset + (CELL - pts[p].y);
                if (first) { ctx.moveTo(px, py); first = false; }
                else ctx.lineTo(px, py);
            }
        }
        ctx.closePath();
        ctx.fill();
        ctx.strokeStyle = '#333';
        ctx.lineWidth = 0.5;
        ctx.stroke();
    };

    BrickEditor.prototype.save = function() {
        var data = {
            brickTypeId: BRICK_ID || 0,
            name: document.getElementById('propName').value,
            imageId: document.getElementById('propImageId').value,
            destructible: document.getElementById('propDestructible').checked,
            border: this.border,
            color: document.getElementById('propColor').value
        };

        fetch('/brick-types/save', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(data)
        })
        .then(function(r) { return r.json(); })
        .then(function(res) {
            if (res.ok) {
                window.location.href = '/brick-types?flash=Saved+successfully';
            } else {
                alert('Save failed: ' + (res.error || 'unknown'));
            }
        });
    };

    window.brickEditor = new BrickEditor();
})();
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/admin/...`

- [ ] **Step 4: Commit**

```bash
git add internal/admin/templates/brick_editor.html internal/admin/static/brick_editor.js
git commit -m "feat(map-editor-v2): add 16x16 polygon brick editor UI"
```

---

### Task 5: Update Map Editor for Integer Tiles

**Files:**
- Modify: `internal/admin/static/map_editor.js`
- Modify: `internal/admin/handlers_map_editor.go` — update `handleMapEditor` to pass brick types with int IDs

- [ ] **Step 1: Update map_editor.js**

Key changes:
- `BRICK_TYPES` now has `BrickTypeID` as integer
- Tiles are `0` (air) instead of `null`, brick IDs are integers
- Use brick's `Color` field for rendering instead of hardcoded palette
- `selectedBrick` is an integer

Replace the color assignment at the top:
```javascript
var BRICK_COLORS = {};
BRICK_TYPES.forEach(function(bt) {
    BRICK_COLORS[bt.BrickTypeID] = bt.Color || '#888';
});
```

Replace null checks: tiles use `0` instead of `null` for air:
- `paintCell`: `var val = this.tool === 'erase' ? 0 : this.selectedBrick;`
- `floodFill`: compare with `=== target` (works for both 0 and int)
- Initialize empty tiles with `new Array(self.gridWidth).fill(0)`
- Check for tile: `if (tile && tile > 0)` instead of `if (tile)`

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/admin/...`

- [ ] **Step 3: Commit**

```bash
git add internal/admin/static/map_editor.js internal/admin/handlers_map_editor.go
git commit -m "feat(map-editor-v2): update map editor for integer tile IDs"
```

---

### Task 6: Update GameData — BrickTypeConfig + MapConfig

**Files:**
- Modify: `internal/game/gamedata/loader.go`

- [ ] **Step 1: Update BrickTypeConfig struct**

```go
type BorderPoint struct {
	X float64 `json:"x" yaml:"x"`
	Y float64 `json:"y" yaml:"y"`
}

type BrickBorder struct {
	Top    []BorderPoint `json:"top" yaml:"top"`
	Right  []BorderPoint `json:"right" yaml:"right"`
	Bottom []BorderPoint `json:"bottom" yaml:"bottom"`
	Left   []BorderPoint `json:"left" yaml:"left"`
}

type BrickTypeConfig struct {
	BrickTypeID  int         `yaml:"brickTypeId"`
	Name         string      `yaml:"name"`
	ImageID      string      `yaml:"imageId"`
	Destructible bool        `yaml:"destructible"`
	Border       BrickBorder `yaml:"border"`
	Color        string      `yaml:"color"`
}
```

- [ ] **Step 2: Update MapConfig tiles to `[][]int`**

```go
type MapConfig struct {
	MapID                 string       `yaml:"mapId"`
	Name                  string       `yaml:"name"`
	GridWidth             int          `yaml:"gridWidth"`
	GridHeight            int          `yaml:"gridHeight"`
	CellSize              int          `yaml:"cellSize"`
	DefaultWindPowerRange []float64    `yaml:"defaultWindPowerRange"`
	Tiles                 [][]int      `yaml:"tiles"` // 0 = air, >0 = brick_type_id
	SpawnPoints           []SpawnPoint `yaml:"spawnPoints"`

	// Legacy fields
	Width         int            `yaml:"width,omitempty"`
	Height        int            `yaml:"height,omitempty"`
	TerrainLayers []TerrainLayer `yaml:"terrainLayers,omitempty"`
}
```

- [ ] **Step 3: Update BrickTypes loading in LoadGameDataFromDB**

Change `BrickTypes` type to `map[int]BrickTypeConfig` and update the loading:

```go
var BrickTypes map[int]BrickTypeConfig

// In LoadGameDataFromDB:
	BrickTypes = make(map[int]BrickTypeConfig)
	rows, err = db.Pool.Query(ctx, `SELECT brick_type_id, name, image_id, destructible, border, color FROM config_brick_types`)
	if err != nil {
		return fmt.Errorf("failed to query config_brick_types: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bt BrickTypeConfig
		var borderJSON []byte
		if err := rows.Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible, &borderJSON, &bt.Color); err != nil {
			return fmt.Errorf("failed to scan config_brick_types row: %w", err)
		}
		if borderJSON != nil {
			json.Unmarshal(borderJSON, &bt.Border)
		}
		BrickTypes[bt.BrickTypeID] = bt
	}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/game/...`

- [ ] **Step 5: Commit**

```bash
git add internal/game/gamedata/loader.go
git commit -m "feat(map-editor-v2): update BrickTypeConfig with border and int tiles"
```

---

### Task 7: Polygon Scanline Fill + Terrain Update

**Files:**
- Create: `internal/game/match/polygon.go`
- Modify: `internal/game/match/terrain.go`
- Modify: `internal/game/match/terrain_test.go`

- [ ] **Step 1: Create polygon.go with scanline fill**

```go
package match

import (
	"battle-squad/internal/game/gamedata"
	"sort"
)

// polygonFromBorder builds a closed polygon from a BrickBorder's 4 edges.
// Points are in brick-local coords (origin bottom-left, y-up).
// Returns points converted to pixel coords relative to cell top-left (y-down).
func polygonFromBorder(border gamedata.BrickBorder, cellSize int) []gamedata.BorderPoint {
	var poly []gamedata.BorderPoint
	edges := [][]gamedata.BorderPoint{border.Bottom, border.Right, border.Top, border.Left}
	for _, edge := range edges {
		for _, p := range edge {
			// Convert from bottom-left origin (y-up) to top-left origin (y-down)
			poly = append(poly, gamedata.BorderPoint{
				X: p.X,
				Y: float64(cellSize) - p.Y,
			})
		}
	}
	return poly
}

// scanlineFillPolygon fills a polygon into a boolean mask at the given cell offset.
// poly points are in cell-local pixel coords (top-left origin).
// offsetX, offsetY are the cell's top-left corner in the full terrain mask.
func scanlineFillPolygon(
	mask []bool,
	destructibleMask []bool,
	width int,
	poly []gamedata.BorderPoint,
	offsetX, offsetY int,
	cellSize int,
	destructible bool,
) {
	if len(poly) < 3 {
		return
	}

	for y := 0; y < cellSize; y++ {
		scanY := float64(y) + 0.5 // scan through center of pixel row

		// Find intersections of scanline with polygon edges
		var intersections []float64
		n := len(poly)
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			y1, y2 := poly[i].Y, poly[j].Y
			if y1 == y2 {
				continue // horizontal edge
			}
			if scanY < min64(y1, y2) || scanY >= max64(y1, y2) {
				continue
			}
			// Linear interpolation for x at scanY
			t := (scanY - y1) / (y2 - y1)
			xIntersect := poly[i].X + t*(poly[j].X-poly[i].X)
			intersections = append(intersections, xIntersect)
		}

		sort.Float64s(intersections)

		// Fill between pairs of intersections
		for k := 0; k+1 < len(intersections); k += 2 {
			xStart := int(intersections[k])
			xEnd := int(intersections[k+1])
			if xStart < 0 {
				xStart = 0
			}
			if xEnd > cellSize {
				xEnd = cellSize
			}
			for x := xStart; x < xEnd; x++ {
				px := offsetX + x
				py := offsetY + y
				if px >= 0 && px < width && py >= 0 {
					idx := py*width + px
					if idx < len(mask) {
						mask[idx] = true
						destructibleMask[idx] = destructible
					}
				}
			}
		}
	}
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Update NewTerrain in terrain.go**

Update the tilemap branch to use polygon fill instead of full-square fill:

```go
	if hasTiles {
		cs := mapCfg.CellSize
		for row := 0; row < len(mapCfg.Tiles); row++ {
			for col := 0; col < len(mapCfg.Tiles[row]); col++ {
				brickID := mapCfg.Tiles[row][col]
				if brickID == 0 {
					continue
				}
				destructible := true
				var border gamedata.BrickBorder
				hasBorder := false
				if gamedata.BrickTypes != nil {
					if bt, ok := gamedata.BrickTypes[brickID]; ok {
						destructible = bt.Destructible
						border = bt.Border
						hasBorder = len(border.Top) > 0
					}
				}

				offsetX := col * cs
				offsetY := row * cs

				if hasBorder {
					poly := polygonFromBorder(border, cs)
					scanlineFillPolygon(t.Mask, t.DestructibleMask, width, poly, offsetX, offsetY, cs, destructible)
				} else {
					// Fallback: fill full square
					for py := offsetY; py < offsetY+cs && py < height; py++ {
						for px := offsetX; px < offsetX+cs && px < width; px++ {
							idx := py*width + px
							t.Mask[idx] = true
							t.DestructibleMask[idx] = destructible
						}
					}
				}
			}
		}
	}
```

- [ ] **Step 3: Update terrain_test.go for int tiles and polygon**

Update existing tests to use `[][]int` instead of `[][]string`, and `map[int]BrickTypeConfig` instead of `map[string]BrickTypeConfig`. Add a test for polygon border:

```go
func TestNewTerrainPolygonBorder(t *testing.T) {
	// Triangle brick: bottom-left half of the square
	cfg := gamedata.MapConfig{
		MapID:      "test_poly",
		GridWidth:  1,
		GridHeight: 1,
		CellSize:   16,
		Tiles:      [][]int{{1}},
	}

	gamedata.BrickTypes = map[int]gamedata.BrickTypeConfig{
		1: {
			BrickTypeID:  1,
			Destructible: true,
			Border: gamedata.BrickBorder{
				Bottom: []gamedata.BorderPoint{{X: 0, Y: 0}, {X: 16, Y: 0}},
				Right:  []gamedata.BorderPoint{{X: 16, Y: 0}, {X: 16, Y: 16}},
				Top:    []gamedata.BorderPoint{{X: 16, Y: 16}, {X: 0, Y: 16}},
				Left:   []gamedata.BorderPoint{{X: 0, Y: 16}, {X: 0, Y: 0}},
			},
		},
	}

	terrain := NewTerrain(cfg)

	// Full square should be mostly solid
	if !terrain.IsSolid(8, 8) {
		t.Error("expected solid at center (8,8)")
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/game/match/... -v -count=1`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/game/match/polygon.go internal/game/match/terrain.go \
  internal/game/match/terrain_test.go
git commit -m "feat(map-editor-v2): polygon scanline fill for terrain mask"
```

---

### Task 8: Update Seed + Final Cleanup

**Files:**
- Modify: `internal/admin/seed.go`
- Modify: `configs/maps.yaml`

- [ ] **Step 1: Update seed.go brick types seeding**

Remove the old text-based brick seeding. The v2 table uses SERIAL PK so seeding works differently:

```go
	// Seed brick types (v2 — SERIAL PK, auto-increment)
	brickQuery := `INSERT INTO config_brick_types (name, image_id, destructible, color)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`
	defaultBricks := []struct {
		Name         string
		Destructible bool
		Color        string
	}{
		{"Dirt", true, "#8B4513"},
		{"Rock", false, "#808080"},
		{"Ice", true, "#87CEEB"},
		{"Lava", false, "#FF4500"},
		{"Fragile", true, "#DEB887"},
	}
	// Only seed if table is empty
	var brickCount int
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_brick_types`).Scan(&brickCount)
	if brickCount == 0 {
		for _, b := range defaultBricks {
			if _, err := db.Pool.Exec(ctx, brickQuery, b.Name, "", b.Destructible, b.Color); err != nil {
				return fmt.Errorf("insert brick type %s: %w", b.Name, err)
			}
		}
		observability.Log.Info().Int("count", len(defaultBricks)).Msg("seeded config_brick_types")
	}
```

- [ ] **Step 2: Update maps.yaml tiles to int format**

```yaml
  tiles: []
```
(Keep as empty — maps will be designed in the editor)

- [ ] **Step 3: Verify all tests pass**

Run: `go test ./... -count=1`
Run: `go build ./...`

- [ ] **Step 4: Commit**

```bash
git add internal/admin/seed.go configs/maps.yaml
git commit -m "feat(map-editor-v2): update seed for brick types v2"
```

---

### Task 9: End-to-End Verification

- [ ] **Step 1: Run migration**
Run: `go run cmd/migrate/main.go`

- [ ] **Step 2: Start admin server**
Run: `go run cmd/admin/main.go`

- [ ] **Step 3: Test brick types page**
Open `http://localhost:9000/brick-types` — should show list

- [ ] **Step 4: Test brick editor**
Click "+ Add Brick" — should open 16x16 polygon editor
Draw polygon points, set name/color, save

- [ ] **Step 5: Test map editor**
Open a map editor — brick palette should show bricks from DB

- [ ] **Step 6: Run all tests**
Run: `go test ./... -v -count=1`

- [ ] **Step 7: Final commit**
```bash
git add -A
git commit -m "feat(map-editor-v2): complete polygon brick border system"
```
