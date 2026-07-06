package admin

import (
	"net/http"
	"strings"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handlePhysics(w http.ResponseWriter, r *http.Request) {
	settings, err := s.repo.GetAllSettings(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get settings")
	}

	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")

	s.render(w, "physics", map[string]interface{}{
		"ActivePage": "physics",
		"Settings":   settings,
		"Flash":      flash,
		"Error":      errMsg,
	})
}

func (s *Server) handlePhysicsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/physics?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()

	// Iterate over all form values that start with "setting_"
	for key, values := range r.Form {
		if !strings.HasPrefix(key, "setting_") {
			continue
		}
		settingKey := strings.TrimPrefix(key, "setting_")
		if len(values) == 0 {
			continue
		}
		value := values[0]

		if err := s.repo.UpdateSetting(ctx, settingKey, value); err != nil {
			observability.Log.Error().Err(err).Str("key", settingKey).Msg("failed to update setting")
			http.Redirect(w, r, "/physics?error=Failed+to+update+settings", http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/physics?flash=Settings+saved+successfully", http.StatusSeeOther)
}
