package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/observability"
)

// handlePhysics lists the game_settings rows (the rules.* rows are edited on Game Rules).
func (s *Server) handlePhysics(w http.ResponseWriter, r *http.Request) {
	settings, err := s.repo.GetAllSettings(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get settings")
	}
	shown := settings[:0:0]
	for _, st := range settings {
		if !strings.HasPrefix(st.Key, gamedata.RuleGroupPrefix) {
			shown = append(shown, st)
		}
	}

	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")

	s.render(w, "physics", map[string]interface{}{
		"ActivePage": "physics",
		"Settings":   shown,
		"Flash":      flash,
		"Error":      errMsg,
	})
}

// handlePhysicsSave saves the posted rows after checking all of them: a known number row
// (gamedata.ScalarSettings) within its range, a JSON row valid JSON; rules.* rows only through
// Game Rules.
func (s *Server) handlePhysicsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/physics?error=Invalid+form+data", http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	settings, err := s.repo.GetAllSettings(ctx)
	if err != nil {
		redirectMsg(w, r, "/physics", "error", "Không đọc được game_settings")
		return
	}
	types := map[string]string{}
	for _, st := range settings {
		types[st.Key] = st.ValueType
	}

	updates := map[string]string{}
	for key, values := range r.Form {
		if !strings.HasPrefix(key, "setting_") || len(values) == 0 {
			continue
		}
		settingKey := strings.TrimPrefix(key, "setting_")
		value := strings.TrimSpace(values[0])
		if strings.HasPrefix(settingKey, gamedata.RuleGroupPrefix) {
			redirectMsg(w, r, "/physics", "error", settingKey+": sửa ở trang Game Rules")
			return
		}
		if sc, ok := gamedata.ScalarSettingOf(settingKey); ok {
			if _, err := sc.Check(value); err != nil {
				redirectMsg(w, r, "/physics", "error", settingKey+": "+err.Error())
				return
			}
		} else if types[settingKey] == "json" && !json.Valid([]byte(value)) {
			redirectMsg(w, r, "/physics", "error", settingKey+": không phải JSON hợp lệ")
			return
		}
		updates[settingKey] = value
	}
	for k, v := range updates {
		if err := s.repo.UpdateSetting(ctx, k, v); err != nil {
			observability.Log.Error().Err(err).Str("key", k).Msg("failed to update setting")
			http.Redirect(w, r, "/physics?error=Failed+to+update+settings", http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/physics?flash=Settings+saved+successfully", http.StatusSeeOther)
}
