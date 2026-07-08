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
