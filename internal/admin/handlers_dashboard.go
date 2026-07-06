package admin

import (
	"net/http"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.repo.GetDashboardStats(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get dashboard stats")
		stats = &DashboardStats{}
	}

	s.render(w, "dashboard", map[string]interface{}{
		"ActivePage": "dashboard",
		"Stats":      stats,
	})
}
