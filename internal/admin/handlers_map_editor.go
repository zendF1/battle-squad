package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
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
		"Error":         r.URL.Query().Get("error"),
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
		"MapThemes":      gamedata.MapThemes,
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
		Theme                 string          `json:"theme"`
		Tiles                 json.RawMessage `json:"tiles"`
		SpawnPoints           json.RawMessage `json:"spawnPoints"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

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
		gridWidth = 150
	}
	gridHeight := body.GridHeight
	if gridHeight == 0 {
		gridHeight = 70
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
		Theme:                 body.Theme, // "" keeps the stored theme (grassland for a new map)
	}
	if m.Theme != "" && !gamedata.KnownMapTheme(m.Theme) {
		writeMapSaveError(w, fmt.Sprintf("unknown theme %q", m.Theme))
		return
	}

	cfg := gamedata.MapConfig{MapID: mapID, GridWidth: gridWidth, GridHeight: gridHeight, CellSize: cellSize}
	if err := json.Unmarshal(tiles, &cfg.Tiles); err != nil {
		writeMapSaveError(w, "tiles must be a matrix of brick ids")
		return
	}
	if err := json.Unmarshal(spawnPoints, &cfg.SpawnPoints); err != nil {
		writeMapSaveError(w, "invalid spawn points")
		return
	}
	if err := json.Unmarshal(windRange, &cfg.DefaultWindPowerRange); err != nil {
		writeMapSaveError(w, "invalid wind power range")
		return
	}

	// The editor turns a legacy map's empty tiles into an all-air grid. Saving that grid would
	// switch the map to tile mode with no terrain, so keep the legacy terrain instead.
	if existing, err := s.repo.GetMap(r.Context(), mapID); err == nil && isLegacyTiles(existing.Tiles) && allAir(cfg.Tiles) {
		m.Tiles = json.RawMessage(`[]`)
		m.TerrainLayers = existing.TerrainLayers
		m.Width, m.Height = existing.Width, existing.Height
		cfg.Tiles = nil
	}
	cfg.Width, cfg.Height = m.Width, m.Height

	brickTypes := make(map[int]gamedata.BrickTypeConfig)
	if items, err := s.repo.GetBrickTypes(r.Context()); err == nil {
		for _, item := range items {
			bt := gamedata.BrickTypeConfig{BrickTypeID: item.BrickTypeID, Destructible: item.Destructible}
			if len(item.Border) > 0 {
				json.Unmarshal(item.Border, &bt.Border)
			}
			brickTypes[item.BrickTypeID] = bt
		}
	} else {
		observability.Log.Error().Err(err).Msg("failed to load brick types for map validation")
		brickTypes = nil
	}
	if err := gamedata.ValidateMap(cfg, brickTypes); err != nil {
		writeMapSaveError(w, err.Error())
		return
	}
	if len(cfg.SpawnPoints) < gamedata.MinSpawnPoints {
		writeMapSaveError(w, fmt.Sprintf("map needs at least %d spawn points (one per team)", gamedata.MinSpawnPoints))
		return
	}

	// Every spawn must land on ground inside the map with room for the player.
	terrain := match.NewTerrainWithBricks(cfg, brickTypes)
	for i, sp := range cfg.SpawnPoints {
		if !terrain.CanStandAt(sp.X, sp.Y) {
			writeMapSaveError(w, fmt.Sprintf("spawn point %d (%v,%v) has no ground below it or less than %dpx of headroom", i, sp.X, sp.Y, gamedata.Current().Rules.Terrain.PlayerHeight))
			return
		}
	}

	if err := s.repo.UpsertMap(r.Context(), m); err != nil {
		observability.Log.Error().Err(err).Str("mapId", mapID).Msg("failed to save map")
		http.Error(w, `{"error":"save failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "mapId": mapID})
}

func writeMapSaveError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// isLegacyTiles reports whether stored tiles are empty, meaning the map uses legacy terrain
// (same rule as gamedata.MapConfig.HasTiles).
func isLegacyTiles(raw json.RawMessage) bool {
	var tiles [][]int
	if err := json.Unmarshal(raw, &tiles); err != nil {
		return len(raw) == 0 || string(raw) == "null"
	}
	return len(tiles) == 0
}

func allAir(tiles [][]int) bool {
	for _, row := range tiles {
		for _, id := range row {
			if id != 0 {
				return false
			}
		}
	}
	return true
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
	SpawnPoints           []map[string]any `yaml:"spawnPoints"`
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
		export.SpawnPoints = []map[string]any{}
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
