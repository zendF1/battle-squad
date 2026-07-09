package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
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

	// Build border map for JS preview rendering
	borderMap := make(map[int]json.RawMessage)
	for _, item := range items {
		borderMap[item.BrickTypeID] = item.Border
	}
	borderMapJSON, _ := json.Marshal(borderMap)

	s.render(w, "brick_types", map[string]interface{}{
		"ActivePage":    "brick-types",
		"Items":         items,
		"BorderMapJSON": string(borderMapJSON),
		"Flash":         r.URL.Query().Get("flash"),
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
		borderJSON = `{"bottom":[{"x":0,"y":0},{"x":16,"y":0}],"right":[{"x":16,"y":0},{"x":16,"y":16}],"top":[{"x":16,"y":16},{"x":0,"y":16}],"left":[{"x":0,"y":16},{"x":0,"y":0}]}`
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

	m, err := s.repo.GetMap(r.Context(), mapID)
	if err != nil {
		observability.Log.Error().Err(err).Str("mapId", mapID).Msg("failed to load map for save")
		http.Error(w, `{"error":"map not found"}`, http.StatusNotFound)
		return
	}
	m.Tiles = body.Tiles
	m.SpawnPoints = body.SpawnPoints

	if err := s.repo.SaveMapFull(r.Context(), m); err != nil {
		observability.Log.Error().Err(err).Str("mapId", mapID).Msg("failed to save map tiles")
		http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// MapExportYAML is the YAML output structure for a single map.
type MapExportYAML struct {
	MapID                 string               `yaml:"mapId"`
	Name                  string               `yaml:"name"`
	GridWidth             int                  `yaml:"gridWidth"`
	GridHeight            int                  `yaml:"gridHeight"`
	CellSize              int                  `yaml:"cellSize"`
	DefaultWindPowerRange []float64            `yaml:"defaultWindPowerRange"`
	Tiles                 [][]interface{}      `yaml:"tiles"`
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

	if err := json.Unmarshal(data.DefaultWindPowerRange, &export.DefaultWindPowerRange); err != nil {
		export.DefaultWindPowerRange = []float64{0, 4}
	}

	if err := json.Unmarshal(data.Tiles, &export.Tiles); err != nil {
		export.Tiles = [][]interface{}{}
	}

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
