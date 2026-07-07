package admin

import (
	"encoding/json"
	"io"
	"net/http"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handleMatchmakingPage(w http.ResponseWriter, r *http.Request) {
	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")
	s.render(w, "matchmaking", map[string]interface{}{
		"ActivePage": "matchmaking",
		"Flash":      flash,
		"Error":      errMsg,
	})
}

func (s *Server) handleMatchmakingConfigGet(settingKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := s.repo.GetJSONSetting(r.Context(), settingKey)
		if err != nil {
			observability.Log.Error().Err(err).Str("key", settingKey).Msg("failed to get config")
			http.Error(w, "Config not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(value))
	}
}

func (s *Server) handleMatchmakingConfigSave(settingKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Validate JSON
		var js json.RawMessage
		if err := json.Unmarshal(body, &js); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if err := s.repo.UpsertJSONSetting(r.Context(), settingKey, string(body)); err != nil {
			observability.Log.Error().Err(err).Str("key", settingKey).Msg("failed to save config")
			http.Error(w, "Failed to save", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}
}
