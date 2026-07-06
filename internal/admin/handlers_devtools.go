package admin

import (
	"context"
	"fmt"
	"net/http"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handleDevTools(w http.ResponseWriter, r *http.Request) {
	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")

	s.render(w, "devtools", map[string]interface{}{
		"ActivePage": "devtools",
		"Flash":      flash,
		"Error":      errMsg,
	})
}

func (s *Server) handleClearRooms(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.repo.ClearRooms(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to clear rooms")
		http.Redirect(w, r, "/devtools?error=Failed+to+clear+rooms", http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Cleared+rooms+(%d+keys+deleted)", deleted)
	http.Redirect(w, r, "/devtools?flash="+msg, http.StatusSeeOther)
}

func (s *Server) handleResetData(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.ResetAllData(r.Context()); err != nil {
		observability.Log.Error().Err(err).Msg("failed to reset data")
		http.Redirect(w, r, "/devtools?error=Failed+to+reset+data", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/devtools?flash=All+player+data+has+been+reset", http.StatusSeeOther)
}

func (s *Server) handleSeedConfig(w http.ResponseWriter, r *http.Request) {
	if err := SeedAll(context.Background(), s.db, s.configDir); err != nil {
		observability.Log.Error().Err(err).Msg("failed to seed config")
		http.Redirect(w, r, "/devtools?error=Failed+to+seed+config:+"+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/devtools?flash=Config+data+seeded+successfully", http.StatusSeeOther)
}
