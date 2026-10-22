package admin

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// numRule is a form number's range; whole: it must be a whole number.
type numRule struct {
	Name     string
	Min, Max float64
	Whole    bool
	Optional bool // may be missing from the form (a field only one of the forms has)
}

// configRules are the form checks of each config type: required text fields and number ranges.
var configRules = map[string]struct {
	Required []string
	Numbers  []numRule
}{
	"characters": {Required: []string{"character_id", "name"}, Numbers: []numRule{
		{"hp", 1, 1000000, true, false}, {"damage", 0, 1000000, true, false}, {"mobility", 0, 100000, true, false},
		{"defense", 0, 1000000, true, false}, {"skill_power", 0, 100000, true, false}, {"terrain_damage", 0, 100000, true, false},
		{"difficulty", 1, 5, true, false}, {"sort_order", 0, 10000, true, true},
		{"projectile_count", 0, 1000, true, true}, {"damage_multiplier", 0, 100, false, true},
	}},
	"weapons": {Required: []string{"weapon_id", "name"}, Numbers: []numRule{
		{"damage", 0, 1000000, true, false}, {"explosion_radius", 0, 2000, true, false}, {"terrain_damage", 0, 2000, true, false},
		{"projectile_weight", 0.01, 100, false, false}, {"wind_influence", 0, 100, false, false}, {"multi_hit", 1, 20, true, false},
	}},
	"skills": {Required: []string{"skill_id", "character_id", "name", "effect_type"}, Numbers: []numRule{
		{"cooldown_turn", 0, 100, true, false}, {"projectile_count", 0, 1000, true, false}, {"damage_multiplier", 0, 100, false, false},
	}},
	"items": {Required: []string{"item_id", "name", "type"}, Numbers: []numRule{
		{"value", 0, 1000000, false, false}, {"max_use_per_match", 0, 100, true, false}, {"cooldown", 0, 100, true, false},
		{"sort_order", 0, 10000, true, true},
	}},
}

// validateConfigForm checks a config form: required fields present, numbers parse and lie in
// their range.
func validateConfigForm(configType string, r *http.Request) error {
	rules, ok := configRules[configType]
	if !ok {
		return nil
	}
	for _, name := range rules.Required {
		if strings.TrimSpace(r.FormValue(name)) == "" {
			return fmt.Errorf("%s: bắt buộc", name)
		}
	}
	for _, n := range rules.Numbers {
		raw, present := r.Form[n.Name]
		if !present && n.Optional {
			continue
		}
		text := ""
		if len(raw) > 0 {
			text = strings.TrimSpace(raw[0])
		}
		if text == "" {
			if n.Optional {
				continue
			}
			return fmt.Errorf("%s: bắt buộc", n.Name)
		}
		x, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("%s: %q không phải số", n.Name, text)
		}
		if n.Whole && x != math.Trunc(x) {
			return fmt.Errorf("%s: %v không phải số nguyên", n.Name, x)
		}
		if x < n.Min || x > n.Max {
			return fmt.Errorf("%s: %v ngoài khoảng %v..%v", n.Name, x, n.Min, n.Max)
		}
	}
	return nil
}

// itemValueHelp says what an item's value drives in the match.
func itemValueHelp(itemID string) string {
	switch itemID {
	case "medkit":
		return "Máu hồi (mặc định 450)"
	case "wind_stopper":
		return "Số lượt không có gió (mặc định 2)"
	case "power_shot":
		return "Hệ số sát thương của phát bắn / kỹ năng (mặc định 1.5)"
	case "drill_bomb":
		return "Số px địa hình đạn khoan xuyên qua (mặc định 50)"
	case "spider_net":
		return "Năng lượng di chuyển tối đa của địch bị lưới ở lượt sau (mặc định 50)"
	case "freeze_bomb":
		return "Số lượt địch bị đóng băng (mặc định 1)"
	case "air_strike":
		return "Không dùng: góc và lực không kích ở Game Rules → Vật phẩm"
	case "teleport":
		return "Không dùng"
	}
	return "Giá trị hiệu ứng (≤ 0 = mặc định của vật phẩm)"
}

// validateJSONSetting checks the typed JSON rows the game reads: numbers in range, the level
// curve increasing.
func validateJSONSetting(key string, body []byte) error {
	inRange := func(name string, x, lo, hi float64) error {
		if x < lo || x > hi {
			return fmt.Errorf("%s: %v ngoài khoảng %v..%v", name, x, lo, hi)
		}
		return nil
	}
	switch key {
	case "matchmaking":
		var c struct {
			TickInterval, BaseRatingRange, ExpandInterval, ExpandStep, MaxRatingRange, MaxWaitTime, BotFillAfter, MatchStartDelay, InviteTimeoutSec *float64
			BotRatingModifier, WeightedRatio                                                                                                        *float64
		}
		if err := json.Unmarshal(body, &c); err != nil {
			return fmt.Errorf("matchmaking: %w", err)
		}
		checks := []struct {
			n      string
			v      *float64
			lo, hi float64
		}{{"tickInterval", c.TickInterval, 1, 3600}, {"botFillAfter", c.BotFillAfter, 0, 3600}, {"matchStartDelay", c.MatchStartDelay, 0, 60},
			{"inviteTimeoutSec", c.InviteTimeoutSec, 5, 600},
			{"weightedRatio", c.WeightedRatio, 0, 1}, {"botRatingModifier", c.BotRatingModifier, 0, 10}, {"maxWaitTime", c.MaxWaitTime, 0, 36000},
			{"baseRatingRange", c.BaseRatingRange, 0, 100000}, {"expandStep", c.ExpandStep, 0, 100000}, {"maxRatingRange", c.MaxRatingRange, 0, 100000},
			{"expandInterval", c.ExpandInterval, 0, 3600}}
		for _, ch := range checks {
			if ch.v != nil {
				if err := inRange(ch.n, *ch.v, ch.lo, ch.hi); err != nil {
					return err
				}
			}
		}
	case "elo":
		var c struct{ KFactor, RatingFloor, DefaultRating *float64 }
		if err := json.Unmarshal(body, &c); err != nil {
			return fmt.Errorf("elo: %w", err)
		}
		for _, ch := range []struct {
			n      string
			v      *float64
			lo, hi float64
		}{{"kFactor", c.KFactor, 1, 1000}, {"ratingFloor", c.RatingFloor, 0, 100000}, {"defaultRating", c.DefaultRating, 0, 100000}} {
			if ch.v != nil {
				if err := inRange(ch.n, *ch.v, ch.lo, ch.hi); err != nil {
					return err
				}
			}
		}
	case "character_levels":
		var c struct {
			Levels []struct {
				Level       int `json:"level"`
				ExpRequired int `json:"expRequired"`
			} `json:"levels"`
		}
		if err := json.Unmarshal(body, &c); err != nil {
			return fmt.Errorf("character_levels: %w", err)
		}
		prevLevel, prevExp := 1, 0
		for _, l := range c.Levels {
			if l.Level <= prevLevel || l.ExpRequired <= prevExp {
				return fmt.Errorf("character_levels: level %d (%d EXP) must come after level %d (%d EXP) and need more EXP", l.Level, l.ExpRequired, prevLevel, prevExp)
			}
			prevLevel, prevExp = l.Level, l.ExpRequired
		}
	}
	return nil
}
