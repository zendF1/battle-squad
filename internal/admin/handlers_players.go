package admin

import (
	"net/http"
	"strconv"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	const limit = 20

	players, total, err := s.repo.GetPlayers(r.Context(), search, page, limit)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get players")
	}

	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}

	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")

	s.render(w, "players", map[string]interface{}{
		"ActivePage": "players",
		"Players":    players,
		"Search":     search,
		"Page":       page,
		"TotalPages": totalPages,
		"Total":      total,
		"Flash":      flash,
		"Error":      errMsg,
	})
}

func (s *Server) handlePlayerBan(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/players?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	accountID := r.FormValue("account_id")
	reason := r.FormValue("reason")
	if accountID == "" {
		http.Redirect(w, r, "/players?error=Missing+account+ID", http.StatusSeeOther)
		return
	}
	if reason == "" {
		reason = "Admin ban"
	}

	if err := s.repo.BanPlayer(r.Context(), accountID, reason); err != nil {
		observability.Log.Error().Err(err).Str("account_id", accountID).Msg("failed to ban player")
		http.Redirect(w, r, "/players?error=Failed+to+ban+player", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/players?flash=Player+banned+successfully", http.StatusSeeOther)
}

func (s *Server) handlePlayerUnban(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/players?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	accountID := r.FormValue("account_id")
	if accountID == "" {
		http.Redirect(w, r, "/players?error=Missing+account+ID", http.StatusSeeOther)
		return
	}

	if err := s.repo.UnbanPlayer(r.Context(), accountID); err != nil {
		observability.Log.Error().Err(err).Str("account_id", accountID).Msg("failed to unban player")
		http.Redirect(w, r, "/players?error=Failed+to+unban+player", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/players?flash=Player+unbanned+successfully", http.StatusSeeOther)
}
