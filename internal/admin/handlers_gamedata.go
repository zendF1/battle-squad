package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	apigamedata "battle-squad/internal/api/gamedata"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/gamedatasync"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// ── Version bump and notification ────────────────────────────────────────────────────────

// notifyOnChange runs a mutating request and, when it changed the game data revision (the
// trigger on every config table bumps it), notifies cmd/game, cmd/api and this dashboard to
// reload (Redis gamedata:changed; they also poll the revision).
func (s *Server) notifyOnChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		before, err := gamedatasync.Revision(r.Context(), s.db)
		next.ServeHTTP(w, r)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if after, err := gamedatasync.Revision(ctx, s.db); err == nil && after != before {
			s.notify(ctx, after)
		}
	})
}

// notify publishes the reload notification.
func (s *Server) notify(ctx context.Context, revision int64) {
	if s.redis == nil {
		return
	}
	if err := gamedatasync.NotifyOn(ctx, s.redis, s.notifyChannel, revision); err != nil {
		observability.Log.Warn().Err(err).Msg("failed to notify the game data reload")
	}
}

// gameDataHeader is the header of every page: the game data this dashboard serves, the stored
// revision and what every server reported.
type gameDataHeader struct {
	Version  string
	Revision int64
	Services []serviceStatus
	Stale    bool // a server serves another version
	Failing  bool // a server's last reload failed
}

type serviceStatus struct {
	Key string
	gamedatasync.LoadedStatus
	Same bool
	Age  string
}

func (s *Server) gameDataHeader(ctx context.Context) gameDataHeader {
	h := gameDataHeader{Version: gamedata.Current().Version}
	if s.db != nil {
		h.Revision, _ = gamedatasync.Revision(ctx, s.db)
	}
	if s.redis != nil {
		st, _ := gamedatasync.Statuses(ctx, s.redis)
		keys := make([]string, 0, len(st))
		for k := range st {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := st[k]
			if time.Since(v.CheckedAt) > 10*time.Minute {
				continue // a process gone for a while
			}
			ss := serviceStatus{Key: k, LoadedStatus: v, Same: v.Version == h.Version,
				Age: time.Since(v.CheckedAt).Round(time.Second).String()}
			h.Stale = h.Stale || !ss.Same
			h.Failing = h.Failing || v.Error != ""
			h.Services = append(h.Services, ss)
		}
	}
	return h
}

// ── The game data page ───────────────────────────────────────────────────────────────────

func (s *Server) handleGameDataPage(w http.ResponseWriter, r *http.Request) {
	snap := gamedata.Current()
	payload, _ := json.MarshalIndent(apigamedata.Stamped(snap), "", "  ")
	s.render(w, "gamedata", map[string]interface{}{
		"ActivePage": "gamedata",
		"Snapshot":   snap,
		"Payload":    string(payload),
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

// handleGameDataReload forces every server to reload: bumps the revision and notifies.
func (s *Server) handleGameDataReload(w http.ResponseWriter, r *http.Request) {
	rev, err := gamedatasync.Bump(r.Context(), s.db)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to bump the game data revision")
		redirectMsg(w, r, back(r, "/gamedata"), "error", "Không tăng được revision: "+err.Error())
		return
	}
	// the middleware notifies on the revision change
	redirectMsg(w, r, back(r, "/gamedata"), "flash", fmt.Sprintf("Đã yêu cầu tải lại cấu hình (revision %d)", rev))
}

// back is the page the request came from (same site), else def.
func back(r *http.Request, def string) string {
	if ref := r.Referer(); ref != "" {
		if u, err := url.Parse(ref); err == nil && strings.HasPrefix(u.Path, "/") {
			q := u.Query()
			q.Del("flash")
			q.Del("error")
			u.RawQuery = q.Encode()
			return u.RequestURI()
		}
	}
	return def
}

// redirectMsg redirects to path with a flash or error message.
func redirectMsg(w http.ResponseWriter, r *http.Request, path, kind, msg string) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	http.Redirect(w, r, path+sep+kind+"="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ── Game rules ───────────────────────────────────────────────────────────────────────────

type ruleFieldView struct {
	gamedata.Field
	Value  string
	IsMap  bool
	IsText bool
	IsList bool
}

type ruleGroupView struct {
	gamedata.Group
	Fields []ruleFieldView
}

type scalarView struct {
	gamedata.ScalarSetting
	Value string
}

func (s *Server) handleRulesPage(w http.ResponseWriter, r *http.Request) {
	settings, err := s.repo.SettingsMap(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to read game_settings")
	}
	rules, _ := gamedata.RulesFromSettings(settings)
	var groups []ruleGroupView
	for _, g := range gamedata.RuleGroups {
		vals := g.FormValues(rules)
		gv := ruleGroupView{Group: g}
		for _, f := range g.Fields {
			gv.Fields = append(gv.Fields, ruleFieldView{Field: f, Value: vals[f.Key], IsMap: g.IsMap(f.Key), IsText: g.IsText(f.Key),
				IsList: g.IsList(f.Key)})
		}
		groups = append(groups, gv)
	}
	phys := gamedata.PhysicsFromSettings(settings)
	defaults := map[string]float64{
		"physics.gravity": phys.Gravity, "physics.projectile_speed_multiplier": phys.ProjectileSpeedMultiplier,
		"physics.wind_scale": phys.WindScale, "physics.player_hit_radius": phys.PlayerHitRadius, "physics.time_step": phys.TimeStep,
		"physics.path_record_step": phys.PathRecordStep, "physics.max_flight_seconds": phys.MaxFlightSeconds,
		gamedata.SettingDefenseK: phys.DefenseKOrDefault(), "match.turn_time_seconds": float64(phys.TurnTimeSeconds),
		"match.idle_timeout_minutes": float64(phys.IdleTimeoutMinutes), "move.step_pixels": phys.MoveStepPixels,
		"move.energy_cost_per_2px": phys.MoveEnergyCostPer2px, "fall.damage_threshold": phys.FallDamageThreshold,
		"fall.damage_per_pixel": phys.FallDamagePerPixel,
	}
	var scalars []scalarView
	for _, sc := range gamedata.ScalarSettings {
		v, ok := settings[sc.Key]
		if !ok {
			if d, has := defaults[sc.Key]; has {
				v = strconv.FormatFloat(d, 'f', -1, 64)
			}
		}
		scalars = append(scalars, scalarView{ScalarSetting: sc, Value: v})
	}
	s.render(w, "rules", map[string]interface{}{
		"ActivePage": "rules",
		"Groups":     groups,
		"Scalars":    scalars,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

// handleRulesSave saves one rules group (group=<key>) or the scalar rows (scalars=1), after
// validating every field (types, ranges, required).
func (s *Server) handleRulesSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "/rules", "error", "Dữ liệu form không hợp lệ")
		return
	}
	ctx := r.Context()
	if r.FormValue("scalars") != "" {
		for _, sc := range gamedata.ScalarSettings {
			text := strings.TrimSpace(r.FormValue("s_" + sc.Key))
			if text == "" {
				redirectMsg(w, r, "/rules#scalars", "error", sc.Label+": bắt buộc")
				return
			}
			if _, err := sc.Check(text); err != nil {
				redirectMsg(w, r, "/rules#scalars", "error", err.Error())
				return
			}
		}
		for _, sc := range gamedata.ScalarSettings {
			if err := s.repo.SetSetting(ctx, sc.Key, strings.TrimSpace(r.FormValue("s_"+sc.Key)), "number", sc.Desc, strings.Split(sc.Key, ".")[0]); err != nil {
				redirectMsg(w, r, "/rules#scalars", "error", "Lưu thất bại: "+err.Error())
				return
			}
		}
		redirectMsg(w, r, "/rules#scalars", "flash", "Đã lưu các dòng số")
		return
	}
	g, ok := gamedata.RuleGroup(r.FormValue("group"))
	if !ok {
		redirectMsg(w, r, "/rules", "error", "Nhóm không tồn tại")
		return
	}
	anchor := "/rules#" + g.Key
	settings, err := s.repo.SettingsMap(ctx)
	if err != nil {
		redirectMsg(w, r, anchor, "error", "Không đọc được game_settings")
		return
	}
	base, _ := gamedata.RulesFromSettings(settings)
	form := map[string]string{}
	for _, f := range g.Fields {
		if v, ok := r.Form["f_"+f.Key]; ok && len(v) > 0 {
			form[f.Key] = v[0]
		}
	}
	value, err := g.ParseForm(base, form)
	if err == nil && g.Key == "rules.player" {
		var p gamedata.PlayerRules
		_ = json.Unmarshal([]byte(value), &p)
		if exists, cerr := s.characterExists(ctx, p.DefaultCharacterID); cerr != nil || !exists {
			err = fmt.Errorf("nhân vật mặc định %q không có trong Characters", p.DefaultCharacterID)
		}
	}
	if err != nil {
		redirectMsg(w, r, anchor, "error", err.Error())
		return
	}
	if err := s.repo.SetSetting(ctx, g.Key, value, "json", g.Title, "rules"); err != nil {
		redirectMsg(w, r, anchor, "error", "Lưu thất bại: "+err.Error())
		return
	}
	redirectMsg(w, r, anchor, "flash", "Đã lưu: "+g.Title)
}

func (s *Server) characterExists(ctx context.Context, id string) (bool, error) {
	var ok bool
	err := s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM config_characters WHERE character_id = $1)`, id).Scan(&ok)
	return ok, err
}

// ── Skill tuning ─────────────────────────────────────────────────────────────────────────

type paramView struct {
	gamedata.ParamSpec
	Value   string
	Default string
	Whole   bool
}

type rankView struct {
	Rank        int
	Values      string
	Description string
}

func (s *Server) handleSkillTuning(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	t, err := s.repo.GetSkillTuning(r.Context(), id)
	if err != nil {
		redirectMsg(w, r, "/skills", "error", "Không tìm thấy kỹ năng "+id)
		return
	}
	cfg := gamedata.SkillConfig{SkillID: t.SkillID, EffectType: t.EffectType, Params: t.Params}
	defaults := gamedata.DefaultSkillParams(t.EffectType)
	current := gamedata.TuningParams(tuningOf(cfg))
	var params []paramView
	for _, spec := range gamedata.SkillParamSpecs[t.EffectType] {
		params = append(params, paramView{ParamSpec: spec, Value: gamedata.FormatParam(current[spec.Key]),
			Default: gamedata.FormatParam(defaults[spec.Key]), Whole: gamedata.ParamIsWhole(t.EffectType, spec.Key)})
	}
	tpl := t.Template
	if tpl == "" {
		tpl = t.Description
	}
	rules := match.SkillRulesOf(gamedata.Current(), cfg)
	var ranks []rankView
	for rank, values := range rules.Ranks {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+gamedata.FormatParam(values[k]))
		}
		ranks = append(ranks, rankView{Rank: rank, Values: strings.Join(parts, ", "), Description: fillTemplate(tpl, values)})
	}
	s.render(w, "skill_tuning", map[string]interface{}{
		"ActivePage": "skills",
		"Skill":      t,
		"Params":     params,
		"Ranks":      ranks,
		"AimKind":    rules.AimKind,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

// tuningOf is a skill's tuning struct (nil for an effect type without one).
func tuningOf(s gamedata.SkillConfig) any {
	switch s.EffectType {
	case gamedata.EffectMissileRain:
		return gamedata.RainTuningOf(s)
	case gamedata.EffectBombChain:
		return gamedata.ChainTuningOf(s)
	case gamedata.EffectLaser:
		return gamedata.LaserTuningOf(s)
	case gamedata.EffectBloom:
		return gamedata.BloomTuningOf(s)
	}
	return struct{}{}
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// fillTemplate fills {key} placeholders with the rank's values, as the client does.
func fillTemplate(tpl string, values map[string]float64) string {
	return placeholder.ReplaceAllStringFunc(tpl, func(m string) string {
		if v, ok := values[m[1:len(m)-1]]; ok {
			return gamedata.FormatParam(v)
		}
		return m
	})
}

func (s *Server) handleSkillTuningSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, "/skills", "error", "Dữ liệu form không hợp lệ")
		return
	}
	id := r.FormValue("skill_id")
	page := "/skills/tuning?id=" + url.QueryEscape(id)
	t, err := s.repo.GetSkillTuning(r.Context(), id)
	if err != nil {
		redirectMsg(w, r, "/skills", "error", "Không tìm thấy kỹ năng "+id)
		return
	}
	params, err := gamedata.ParseParamsForm(t.EffectType, r.FormValue)
	if err != nil {
		redirectMsg(w, r, page, "error", err.Error())
		return
	}
	tpl := strings.TrimSpace(r.FormValue("description_template"))
	for _, m := range placeholder.FindAllStringSubmatch(tpl, -1) {
		known := false
		rules := match.SkillRulesOf(gamedata.Current(), gamedata.SkillConfig{EffectType: t.EffectType, Params: params})
		if len(rules.Ranks) > 0 {
			_, known = rules.Ranks[0][m[1]]
		}
		if !known {
			redirectMsg(w, r, page, "error", fmt.Sprintf("Mẫu mô tả: {%s} không phải số của loại %s", m[1], t.EffectType))
			return
		}
	}
	if err := s.repo.UpdateSkillTuning(r.Context(), id, tpl, params); err != nil {
		redirectMsg(w, r, page, "error", "Lưu thất bại: "+err.Error())
		return
	}
	redirectMsg(w, r, page, "flash", "Đã lưu kỹ năng")
}

// ── Text tables ──────────────────────────────────────────────────────────────────────────

// textCol is an editable column of a text table: text, textarea, int (Min..Max), bool or
// select (Options).
type textCol struct {
	Name, Label, Kind string
	Options           []string
	Min, Max          int
	Required          bool
}

// textTable is a config table of rows edited in place (texts and small rules).
type textTable struct {
	Path, Title, Desc string
	Table, Key, Order string
	KeyLabel          string
	Cols              []textCol
	CanAdd, CanDelete bool
	KeySpaces         bool // the key may hold spaces (type words)
	Check             func(ctx context.Context, tx pgx.Tx) error
}

func (t *textTable) colNames() []string {
	out := make([]string, len(t.Cols))
	for i, c := range t.Cols {
		out[i] = c.Name
	}
	return out
}

var textTables = []*textTable{
	{Path: "status-effects", Title: "Status Effects", Table: "config_status_effects", Key: "effect_id", KeyLabel: "Effect ID",
		Order: "sort_order, effect_id", CanAdd: true, CanDelete: true,
		Desc: "Hiệu ứng trạng thái: tên, chữ trên đầu (marker), thông báo (banner), và cách trận đấu xử lý. Xấu = Hoa hồi xuân giải được; Mất lượt = bỏ cả lượt; Chặn di chuyển = không đi / dịch chuyển được.",
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "marker", Label: "Marker", Kind: "text"},
			{Name: "banner", Label: "Banner", Kind: "text"}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000},
			{Name: "bad", Label: "Xấu", Kind: "bool"}, {Name: "skips_turn", Label: "Mất lượt", Kind: "bool"},
			{Name: "blocks_move", Label: "Chặn di chuyển", Kind: "bool"}}},
	{Path: "rank-tiers", Title: "Rank Tiers", Table: "config_rank_tiers", Key: "tier", KeyLabel: "Tier", Order: "sort_order, tier",
		Desc:  "Hạng: tên và khoảng điểm. Hạng bắt đầu từ Min rating tới hạng sau; chia Divisions bậc, mỗi bậc Division size điểm (0 khi chỉ một bậc). Hạng đầu bắt đầu từ 0, mỗi hạng sau cao hơn hạng trước.",
		Check: checkRankBands,
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 100},
			{Name: "min_rating", Label: "Min rating", Kind: "int", Min: 0, Max: 1000000}, {Name: "divisions", Label: "Divisions", Kind: "int", Min: 1, Max: 20},
			{Name: "division_size", Label: "Division size", Kind: "int", Min: 0, Max: 1000000}}},
	{Path: "equipment-tiers", Title: "Equipment Tiers", Table: "config_equipment_tiers", Key: "tier", KeyLabel: "Tier",
		Order: "sort_order, tier", CanAdd: true, CanDelete: true,
		Desc: "Bậc trang bị: normal (mã kết thúc bằng id suffix, vd lv10) hoặc crafted (mã kết thúc bằng tên bậc). Cấp kỹ năng của súng mỗi bậc sửa ở Game Rules → Cấp kỹ năng theo súng.",
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "kind", Label: "Loại", Kind: "select", Options: []string{"normal", "crafted"}},
			{Name: "id_suffix", Label: "ID suffix", Kind: "text"}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
	{Path: "equipment-slots", Title: "Equipment Slots", Table: "config_equipment_slots", Key: "slot", KeyLabel: "Slot",
		Order: "sort_order, slot", CanAdd: true, CanDelete: true, Desc: "Ô trang bị: tên hiển thị, theo thứ tự hiển thị.",
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
	{Path: "gun-kinds", Title: "Gun Kinds", Table: "config_gun_kinds", Key: "gun_kind", KeyLabel: "Gun kind",
		Order: "sort_order, gun_kind", CanAdd: true, CanDelete: true, Desc: "Loại súng (Characters → gun kind) và tên hiển thị.",
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
	{Path: "type-words", Title: "Item Type Words", Table: "config_item_type_words", Key: "word", KeyLabel: "Từ (chữ thường)",
		Order: "sort_order, word", CanAdd: true, CanDelete: true, KeySpaces: true, Check: checkTypeWordGunKinds,
		Desc: "Phần loại của tên trang bị \"<Tier> <Type>\" (vd \"shock rifle\"), chữ tiếng Việt và loại súng (để trống với giáp).",
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "gun_kind", Label: "Gun kind", Kind: "text"},
			{Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
	{Path: "game-modes", Title: "Game Modes", Table: "config_game_modes", Key: "mode_id", KeyLabel: "Mode ID",
		Order: "sort_order, mode_id", CanAdd: true, CanDelete: true, Check: checkGameModes,
		Desc: "Chế độ: lobby = một lựa chọn ở màn Chọn chế độ, trong mục Category (Mode Categories), theo Thứ tự (queue: hàng chờ ghép trận, " +
			strings.Join(matchmaker.Queues, " / ") + " hoặc boss:<boss id> của một boss đang bật (trang Bosses); bot: QuickPlay với bot; cả hai trống = luyện tập; Icon: mã icon của client, trống = icon chung; " +
			"Số người: tối đa trong đội (2v2: 1..2, boss: 1..4), 1 khi không có queue; Xếp hạng: bật đúng khi queue là " + matchmaker.RankedQueue + "; tắt Bật = ẩn khỏi GET /gamedata; " +
			"Số vật phẩm: số vật phẩm mỗi người mang vào trận của chế độ (0..20), trống = luật chung Rules › maxLoadout; boss: 6). " +
			"match = chế độ của trận (lịch sử, kết quả; mode id = queue của trận ghép, vd boss:steel_robot).",
		Cols: []textCol{{Name: "kind", Label: "Loại", Kind: "select", Options: []string{"lobby", "match"}}, {Name: "name", Label: "Tên", Kind: "text", Required: true},
			{Name: "description", Label: "Mô tả", Kind: "text"}, {Name: "category", Label: "Category", Kind: "text"}, {Name: "icon", Label: "Icon", Kind: "text"},
			{Name: "queue", Label: "Queue", Kind: "text"}, {Name: "bot", Label: "Bot", Kind: "text"},
			{Name: "party_size", Label: "Số người", Kind: "int", Min: 1, Max: gamedata.BossPartySize}, {Name: "ranked", Label: "Xếp hạng", Kind: "bool"},
			{Name: "max_items", Label: "Số vật phẩm", Kind: "optint", Min: 0, Max: 20},
			{Name: "enabled", Label: "Bật", Kind: "bool"}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
	{Path: "mode-categories", Title: "Mode Categories", Table: "config_mode_categories", Key: "category_id", KeyLabel: "Category ID",
		Order: "sort_order, category_id", CanAdd: true, CanDelete: true, Check: checkGameModes,
		Desc: "Các mục (tab) của màn Chọn chế độ, theo thứ tự; mỗi chế độ lobby ở Game Modes thuộc một mục. Mục chưa có chế độ nào: client hiện \"Sắp ra mắt\".",
		Cols: []textCol{{Name: "name", Label: "Tên", Kind: "text", Required: true}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
	{Path: "tips", Title: "Tips", Table: "config_tips", Key: "tip_id", KeyLabel: "Tip ID", Order: "sort_order, tip_id", CanAdd: true, CanDelete: true,
		Desc: "Mẹo hiển thị khi tìm trận, theo thứ tự.",
		Cols: []textCol{{Name: "text", Label: "Nội dung", Kind: "textarea", Required: true}, {Name: "sort_order", Label: "Thứ tự", Kind: "int", Min: 0, Max: 10000}}},
}

func textTableOf(path string) *textTable {
	for _, t := range textTables {
		if t.Path == path {
			return t
		}
	}
	return nil
}

// checkRankBands rejects rank tier numbers that do not make valid bands (ranked.ValidateBands).
func checkRankBands(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT tier, min_rating, divisions, division_size FROM config_rank_tiers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	got := map[string]ranked.Band{}
	for rows.Next() {
		var b ranked.Band
		if err := rows.Scan(&b.Tier, &b.MinRating, &b.Divisions, &b.DivisionSize); err != nil {
			return err
		}
		got[b.Tier] = b
	}
	if err := rows.Err(); err != nil {
		return err
	}
	bands := make([]ranked.Band, 0, len(ranked.Tiers))
	for _, t := range ranked.Tiers {
		if b, ok := got[t]; ok {
			bands = append(bands, b)
		} else {
			bands = append(bands, gamedata.DefaultRankBand(t))
		}
	}
	return ranked.ValidateBands(bands)
}

// checkGameModes rejects a lobby mode the server cannot run: a queue the matchmaker does not
// run (a 2v2 queue, or boss:<id> of an enabled config_bosses row), ranked other than the
// queue's (only ranked_2v2 changes rating), a party bigger than 1 without a queue (or than the
// queue allows: 2 for 2v2, 4 for a boss), or a category that is not a config_mode_categories row.
func checkGameModes(ctx context.Context, tx pgx.Tx) error {
	bosses := map[string]bool{}
	if err := collectIDs(ctx, tx, `SELECT boss_id FROM config_bosses WHERE enabled`, func(id string) { bosses[id] = true }); err != nil {
		return err
	}
	cats := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT category_id FROM config_mode_categories`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		cats[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT mode_id, queue, category, party_size, ranked FROM config_game_modes
		WHERE kind = 'lobby' ORDER BY sort_order, mode_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, queue, category string
		var party int
		var isRanked bool
		if err := rows.Scan(&id, &queue, &category, &party, &isRanked); err != nil {
			return err
		}
		bad := func(format string, args ...any) error {
			return fmt.Errorf("%s: "+format, append([]any{id}, args...)...)
		}
		bossID, isBoss := gamedata.BossIDOfQueue(queue)
		switch {
		case isBoss && !bosses[bossID]:
			return bad("queue %q: boss %q không có hoặc đang tắt (trang Bosses)", queue, bossID)
		case queue != "" && !isBoss && !slices.Contains(matchmaker.Queues, queue):
			return bad("queue %q không có, server chỉ chạy %s và boss:<boss id>", queue, strings.Join(matchmaker.Queues, ", "))
		case isRanked != matchmaker.IsRankedQueue(queue):
			return bad("Xếp hạng phải %s với queue %q", map[bool]string{true: "bật", false: "tắt"}[matchmaker.IsRankedQueue(queue)], queue)
		case party < 1 || party > matchmaker.MaxPartySize(queue):
			return bad("Số người %d ngoài khoảng 1..%d", party, matchmaker.MaxPartySize(queue))
		case queue == "" && party != 1:
			return bad("Số người phải là 1 khi không có queue")
		case !cats[category]:
			return bad("category %q không có trong Mode Categories", category)
		}
	}
	return rows.Err()
}

// checkTypeWordGunKinds rejects a type word whose gun kind is not a config_gun_kinds row.
func checkTypeWordGunKinds(ctx context.Context, tx pgx.Tx) error {
	var bad string
	err := tx.QueryRow(ctx, `SELECT w.gun_kind FROM config_item_type_words w
		WHERE w.gun_kind <> '' AND NOT EXISTS (SELECT 1 FROM config_gun_kinds g WHERE g.gun_kind = w.gun_kind) LIMIT 1`).Scan(&bad)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("gun kind %q không có trong Gun Kinds", bad)
}

var textKeyPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
var textKeySpacesPattern = regexp.MustCompile(`^[a-z0-9_]+( [a-z0-9_]+)*$`)

func (s *Server) handleTextTable(w http.ResponseWriter, r *http.Request) {
	t := textTableOf(chi.URLParam(r, "table"))
	if t == nil {
		http.NotFound(w, r)
		return
	}
	rows, err := s.repo.TextRows(r.Context(), t)
	if err != nil {
		observability.Log.Error().Err(err).Str("table", t.Table).Msg("failed to read text table")
	}
	s.render(w, "text_table", map[string]interface{}{
		"ActivePage": "texts-" + t.Path,
		"T":          t,
		"Rows":       rows,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleTextTableSave(w http.ResponseWriter, r *http.Request) {
	t := textTableOf(chi.URLParam(r, "table"))
	if t == nil {
		http.NotFound(w, r)
		return
	}
	page := "/texts/" + t.Path
	if err := r.ParseForm(); err != nil {
		redirectMsg(w, r, page, "error", "Dữ liệu form không hợp lệ")
		return
	}
	key := strings.TrimSpace(r.FormValue("key"))
	isNew := r.FormValue("new") != ""
	pattern := textKeyPattern
	if t.KeySpaces {
		pattern = textKeySpacesPattern
	}
	if !pattern.MatchString(key) {
		redirectMsg(w, r, page, "error", t.KeyLabel+": bắt buộc, chỉ chữ thường a-z, số và _")
		return
	}
	if isNew && !t.CanAdd {
		redirectMsg(w, r, page, "error", "Bảng này không thêm dòng được")
		return
	}
	values, err := parseTextRow(t, r)
	if err != nil {
		redirectMsg(w, r, page, "error", key+": "+err.Error())
		return
	}
	if isNew {
		if rows, err := s.repo.TextRows(r.Context(), t); err == nil {
			for _, row := range rows {
				if row[t.Key] == key {
					redirectMsg(w, r, page, "error", key+" đã có")
					return
				}
			}
		}
	}
	if err := s.repo.UpsertTextRow(r.Context(), t, key, values); err != nil {
		redirectMsg(w, r, page, "error", key+": "+err.Error())
		return
	}
	redirectMsg(w, r, page, "flash", "Đã lưu "+key)
}

// parseTextRow validates the row's form values ("c_<column>") into column values.
func parseTextRow(t *textTable, r *http.Request) (map[string]any, error) {
	out := map[string]any{}
	for _, c := range t.Cols {
		raw := r.FormValue("c_" + c.Name)
		switch c.Kind {
		case "bool":
			out[c.Name] = raw == "on" || raw == "true" || raw == "1"
		case "int":
			n, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				return nil, fmt.Errorf("%s: %q không phải số nguyên", c.Label, raw)
			}
			if n < c.Min || n > c.Max {
				return nil, fmt.Errorf("%s: %d ngoài khoảng %d..%d", c.Label, n, c.Min, c.Max)
			}
			out[c.Name] = n
		case "optint": // empty: NULL
			v := strings.TrimSpace(raw)
			if v == "" {
				out[c.Name] = nil
				continue
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %q không phải số nguyên", c.Label, raw)
			}
			if n < c.Min || n > c.Max {
				return nil, fmt.Errorf("%s: %d ngoài khoảng %d..%d (trống = mặc định)", c.Label, n, c.Min, c.Max)
			}
			out[c.Name] = n
		case "select":
			ok := false
			for _, o := range c.Options {
				ok = ok || o == raw
			}
			if !ok {
				return nil, fmt.Errorf("%s: %q không hợp lệ", c.Label, raw)
			}
			out[c.Name] = raw
		default:
			v := strings.TrimSpace(raw)
			if c.Required && v == "" {
				return nil, fmt.Errorf("%s: bắt buộc", c.Label)
			}
			out[c.Name] = v
		}
	}
	return out, nil
}

func (s *Server) handleTextTableDelete(w http.ResponseWriter, r *http.Request) {
	t := textTableOf(chi.URLParam(r, "table"))
	if t == nil {
		http.NotFound(w, r)
		return
	}
	page := "/texts/" + t.Path
	if !t.CanDelete {
		redirectMsg(w, r, page, "error", "Bảng này không xóa dòng được")
		return
	}
	key := r.FormValue("key")
	if err := s.repo.DeleteTextRow(r.Context(), t, key); err != nil {
		redirectMsg(w, r, page, "error", err.Error())
		return
	}
	redirectMsg(w, r, page, "flash", "Đã xóa "+key)
}
