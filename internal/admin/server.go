package admin

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"battle-squad/internal/shared/database"

	"github.com/go-chi/chi/v5"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server holds all dependencies for the admin dashboard.
type Server struct {
	repo      *Repository
	db        *database.PostgresDB
	redis     *database.RedisClient
	configDir string
}

// NewServer creates a new admin Server with parsed templates and repository.
func NewServer(db *database.PostgresDB, redis *database.RedisClient, configDir string) *Server {
	return &Server{
		repo:      NewRepository(db, redis),
		db:        db,
		redis:     redis,
		configDir: configDir,
	}
}

var tmplFuncMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"safeJS": func(s string) template.JS { return template.JS(s) },
	"deref": func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	},
}

// Routes returns the chi router with all admin dashboard routes.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	// Serve static JS/CSS files
	staticContent, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	r.Get("/", s.handleDashboard)

	// Config CRUD routes
	r.Get("/characters", s.handleConfigList("characters"))
	r.Get("/characters/edit", s.handleConfigEdit("characters"))
	r.Post("/characters/save", s.handleConfigSave("characters"))
	r.Post("/characters/delete", s.handleConfigDelete("characters"))
	r.Get("/characters/detail", s.handleCharacterDetail())
	r.Post("/characters/detail/save", s.handleCharacterDetailSave())

	r.Get("/weapons", s.handleConfigList("weapons"))
	r.Get("/weapons/edit", s.handleConfigEdit("weapons"))
	r.Post("/weapons/save", s.handleConfigSave("weapons"))
	r.Post("/weapons/delete", s.handleConfigDelete("weapons"))

	r.Get("/skills", s.handleConfigList("skills"))
	r.Get("/skills/edit", s.handleConfigEdit("skills"))
	r.Post("/skills/save", s.handleConfigSave("skills"))
	r.Post("/skills/delete", s.handleConfigDelete("skills"))

	r.Get("/items", s.handleConfigList("items"))
	r.Get("/items/edit", s.handleConfigEdit("items"))
	r.Post("/items/save", s.handleConfigSave("items"))
	r.Post("/items/delete", s.handleConfigDelete("items"))

	r.Get("/maps", s.handleConfigList("maps"))
	r.Post("/maps/delete", s.handleConfigDelete("maps"))

	// Brick Types
	r.Get("/brick-types", s.handleBrickTypesList)
	r.Get("/brick-types/editor", s.handleBrickTypeEditor)
	r.Post("/brick-types/save", s.handleBrickTypeSave)
	r.Post("/brick-types/delete", s.handleBrickTypeDelete)

	// Map Editor
	r.Get("/maps/editor", s.handleMapEditor)
	r.Get("/api/maps/tiles", s.handleMapTilesGet)
	r.Put("/api/maps/save", s.handleMapSave)
	r.Get("/api/maps/export", s.handleMapExport)
	r.Get("/api/brick-types", s.handleBrickTypesAPI)

	// Physics settings
	r.Get("/physics", s.handlePhysics)
	r.Post("/physics/save", s.handlePhysicsSave)

	// Shop offers
	r.Get("/shop", s.handleShopList)
	r.Get("/shop/edit", s.handleShopEdit)
	r.Post("/shop/save", s.handleShopSave)
	r.Post("/shop/delete", s.handleShopDelete)

	// Players
	r.Get("/players", s.handlePlayers)
	r.Post("/players/ban", s.handlePlayerBan)
	r.Post("/players/unban", s.handlePlayerUnban)

	// Dev tools
	r.Get("/devtools", s.handleDevTools)
	r.Post("/devtools/clear-rooms", s.handleClearRooms)
	r.Post("/devtools/reset-data", s.handleResetData)
	r.Post("/devtools/seed-config", s.handleSeedConfig)

	// Matchmaking config page + API
	r.Get("/matchmaking", s.handleMatchmakingPage)
	r.Get("/api/config/matchmaking", s.handleMatchmakingConfigGet("matchmaking"))
	r.Post("/api/config/matchmaking", s.handleMatchmakingConfigSave("matchmaking"))
	r.Get("/api/config/elo", s.handleMatchmakingConfigGet("elo"))
	r.Post("/api/config/elo", s.handleMatchmakingConfigSave("elo"))
	r.Get("/api/config/bot-difficulty", s.handleMatchmakingConfigGet("bot_difficulty"))
	r.Post("/api/config/bot-difficulty", s.handleMatchmakingConfigSave("bot_difficulty"))

	// Character progression config
	r.Get("/character-progression", s.handleCharacterProgressionPage)
	r.Get("/api/config/character-progression", s.handleMatchmakingConfigGet("character_progression"))
	r.Post("/api/config/character-progression", s.handleMatchmakingConfigSave("character_progression"))
	r.Get("/api/config/character-levels", s.handleMatchmakingConfigGet("character_levels"))
	r.Post("/api/config/character-levels", s.handleMatchmakingConfigSave("character_levels"))

	// Equipment config
	r.Get("/equipment-items", s.handleEquipmentItemsList)
	r.Get("/equipment-items/edit", s.handleEquipmentItemEdit)
	r.Post("/equipment-items/save", s.handleEquipmentItemSave)
	r.Post("/equipment-items/delete", s.handleEquipmentItemDelete)
	r.Get("/upgrade-rates", s.handleUpgradeRates)
	r.Post("/upgrade-rates/save", s.handleUpgradeRateSave)
	r.Get("/equipment-stones", s.handleStoneConfigs)
	r.Post("/equipment-stones/save", s.handleStoneConfigSave)
	r.Get("/equipment-gems", s.handleGemConfigs)
	r.Post("/equipment-gems/save", s.handleGemConfigSave)
	r.Get("/set-bonuses", s.handleSetBonuses)
	r.Post("/set-bonuses/save", s.handleSetBonusSave)

	// Materials config
	r.Get("/materials", s.handleMaterialsList)
	r.Get("/materials/edit", s.handleMaterialEdit)
	r.Post("/materials/save", s.handleMaterialSave)
	r.Post("/materials/delete", s.handleMaterialDelete)

	// Crafting recipes config
	r.Get("/crafting-recipes", s.handleCraftingRecipesList)
	r.Get("/crafting-recipes/edit", s.handleCraftingRecipeEdit)
	r.Post("/crafting-recipes/save", s.handleCraftingRecipeSave)
	r.Post("/crafting-recipes/delete", s.handleCraftingRecipeDelete)

	return r
}

// render parses layout.html + the specific page template together to avoid
// {{define "content"}} collisions between page templates, then executes "layout".
func (s *Server) render(w http.ResponseWriter, tmplName string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, err := template.New("").Funcs(tmplFuncMap).ParseFS(templateFS, "templates/layout.html", "templates/"+tmplName+".html")
	if err != nil {
		http.Error(w, "template parse error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "template exec error: "+err.Error(), http.StatusInternalServerError)
	}
}
