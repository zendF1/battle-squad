package admin

import (
	"embed"
	"html/template"
	"net/http"

	"battle-squad/internal/shared/database"

	"github.com/go-chi/chi/v5"
)

//go:embed templates/*.html
var templateFS embed.FS

// Server holds all dependencies for the admin dashboard.
type Server struct {
	repo      *Repository
	db        *database.PostgresDB
	redis     *database.RedisClient
	templates *template.Template
	configDir string
}

// NewServer creates a new admin Server with parsed templates and repository.
func NewServer(db *database.PostgresDB, redis *database.RedisClient, configDir string) *Server {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"deref": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
	}

	tmpl := template.Must(
		template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"),
	)

	return &Server{
		repo:      NewRepository(db, redis),
		db:        db,
		redis:     redis,
		templates: tmpl,
		configDir: configDir,
	}
}

// Routes returns the chi router with all admin dashboard routes.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/", s.handleDashboard)

	// Config CRUD routes
	r.Get("/characters", s.handleConfigList("characters"))
	r.Get("/characters/edit", s.handleConfigEdit("characters"))
	r.Post("/characters/save", s.handleConfigSave("characters"))
	r.Post("/characters/delete", s.handleConfigDelete("characters"))

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
	r.Get("/maps/edit", s.handleConfigEdit("maps"))
	r.Post("/maps/save", s.handleConfigSave("maps"))
	r.Post("/maps/delete", s.handleConfigDelete("maps"))

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

	return r
}

// render executes the named template with the given data.
func (s *Server) render(w http.ResponseWriter, tmplName string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, tmplName, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
