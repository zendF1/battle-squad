package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/observability"
)

// The Boss page: config_bosses rows, every field editable (the JSON columns as textareas,
// validated by gamedata.ValidateBoss with the characters, items and maps of the DB). Saving
// bumps the game data revision (trigger), so the servers reload.

func (s *Server) handleBossList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.repo.Bosses(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to read bosses")
	}
	s.render(w, "bosses", map[string]interface{}{
		"ActivePage": "bosses",
		"Bosses":     rows,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleBossEdit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	row := BossRow{Config: gamedata.BossConfig{Enabled: true, RecommendedLevel: 1, HP: 10000, Defense: 50, Damage: 400, BodySize: 3,
		TurnLimit: 15, ShieldedDamageFactor: 0.1, OutOfMap: gamedata.BossOutOfMapClamp, DailyBonusRuns: 3},
		SkillsJSON: "[]", MinionsJSON: "[]", WavesJSON: `{"initial": [], "maxAlive": 4}`, RewardsJSON: "{}"}
	if id != "" {
		found, err := s.repo.Boss(r.Context(), id)
		if err != nil {
			redirectMsg(w, r, "/bosses", "error", "Không tìm thấy boss "+id)
			return
		}
		row = found
	}
	s.renderBossEdit(w, row, id == "", r.URL.Query().Get("error"))
}

func (s *Server) renderBossEdit(w http.ResponseWriter, row BossRow, isNew bool, errMsg string) {
	s.render(w, "boss_edit", map[string]interface{}{
		"ActivePage": "bosses",
		"B":          row,
		"IsNew":      isNew,
		"Error":      errMsg,
		"Kinds":      strings.Join(gamedata.BossSkillKinds, ", "),
		"OutOfMaps":  []string{gamedata.BossOutOfMapClamp, gamedata.BossOutOfMapKill},
	})
}

func (s *Server) handleBossSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "/bosses", "error", "Dữ liệu form không hợp lệ")
		return
	}
	row, err := parseBossForm(r)
	isNew := r.FormValue("new") != ""
	if err == nil && isNew {
		if _, err2 := s.repo.Boss(r.Context(), row.Config.BossID); err2 == nil {
			err = fmt.Errorf("boss %s đã có", row.Config.BossID)
		}
	}
	if err == nil {
		err = s.repo.SaveBoss(r.Context(), row.Config)
	}
	if err != nil {
		s.renderBossEdit(w, row, isNew, "Lưu thất bại: "+err.Error())
		return
	}
	redirectMsg(w, r, "/bosses", "flash", "Đã lưu boss "+row.Config.BossID)
}

func (s *Server) handleBossDelete(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	if err := s.repo.DeleteBoss(r.Context(), id); err != nil {
		redirectMsg(w, r, "/bosses", "error", err.Error())
		return
	}
	http.Redirect(w, r, "/bosses?flash="+url.QueryEscape("Đã xóa boss "+id), http.StatusSeeOther)
}

// parseBossForm reads the edit form; the row keeps the submitted texts for a re-render.
func parseBossForm(r *http.Request) (BossRow, error) {
	row := BossRow{SkillsJSON: r.FormValue("skills"), MinionsJSON: r.FormValue("minions"), WavesJSON: r.FormValue("waves"),
		RewardsJSON: r.FormValue("rewards"), HoverJSON: strings.TrimSpace(r.FormValue("hover")),
		SummonJSON: strings.TrimSpace(r.FormValue("summon"))}
	c := &row.Config
	c.BossID = strings.TrimSpace(r.FormValue("boss_id"))
	c.Name = strings.TrimSpace(r.FormValue("name"))
	c.Description = strings.TrimSpace(r.FormValue("description"))
	c.MapID = strings.TrimSpace(r.FormValue("map_id"))
	c.OutOfMap = r.FormValue("out_of_map")
	c.Enabled = r.FormValue("enabled") == "on" || r.FormValue("enabled") == "true"
	c.Flying = r.FormValue("flying") == "on" || r.FormValue("flying") == "true"
	var errs []string
	ints := map[string]*int{"recommended_level": &c.RecommendedLevel, "sort_order": &c.SortOrder, "hp": &c.HP, "defense": &c.Defense,
		"damage": &c.Damage, "turn_limit": &c.TurnLimit, "daily_bonus_runs": &c.DailyBonusRuns}
	for name, dst := range ints {
		n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(name)))
		if err != nil {
			errs = append(errs, name+": không phải số nguyên")
		}
		*dst = n
	}
	floats := map[string]*float64{"body_size": &c.BodySize, "shielded_damage_factor": &c.ShieldedDamageFactor}
	for name, dst := range floats {
		f, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue(name)), 64)
		if err != nil {
			errs = append(errs, name+": không phải số")
		}
		*dst = f
	}
	if len(errs) > 0 {
		return row, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	if err := gamedata.ParseBossJSON(c, []byte(row.SkillsJSON), []byte(row.MinionsJSON), []byte(row.WavesJSON), []byte(row.RewardsJSON)); err != nil {
		return row, fmt.Errorf("JSON: %w", err)
	}
	if err := gamedata.ParseBossHover(c, []byte(row.HoverJSON)); err != nil {
		return row, fmt.Errorf("JSON: %w", err)
	}
	if err := gamedata.ParseBossSummon(c, []byte(row.SummonJSON)); err != nil {
		return row, fmt.Errorf("JSON: %w", err)
	}
	return row, nil
}
