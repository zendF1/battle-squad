package gamedata

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Skill effect types: config_skills.effect_type picks the match's resolver and the tuning its
// params (config_skills.params, a JSON object name → number) fill. A missing or invalid param
// keeps the effect type's default (DefaultSkillTuning). Times are in s, speeds in px/s, damage
// factors × the skill's base damage, blast and crater × the weapon's.
const (
	EffectMissileRain = "missile_rain" // Rookie, triple_shot
	EffectBombChain   = "bomb_chain"   // Tanko, heavy_bomb
	EffectLaser       = "laser"        // Spark, shock_field
	EffectBloom       = "bloom"        // Flora, healing_bloom
)

// RainTuning: a flare fired like a normal shot; Delay s after it lands, BaseCount +
// CountPerRank × rank missiles fall straight down Interval s apart over the flare point ±
// (SpreadBase + SpreadPerRank × rank) px, packed toward it (column offset = spread × |v|^Focus),
// from StartY at DropSpeed. Each missile: Damage × base, blast Blast ×, crater Crater ×; the
// rain deals one target at most Cap × base (before defense). Every enemy damaged burns for
// BurnTurns.
type RainTuning struct {
	BaseCount     int     `json:"baseCount"`
	CountPerRank  int     `json:"countPerRank"`
	SpreadBase    float64 `json:"spreadBase"`
	SpreadPerRank float64 `json:"spreadPerRank"`
	Focus         float64 `json:"focus"`
	Delay         float64 `json:"delay"`
	Interval      float64 `json:"interval"`
	StartY        float64 `json:"startY"`
	DropSpeed     float64 `json:"dropSpeed"`
	Damage        float64 `json:"damage"`
	Blast         float64 `json:"blast"`
	Crater        float64 `json:"crater"`
	Cap           float64 `json:"cap"`
	BurnTurns     int     `json:"burnTurns"`
}

// ChainTuning: BaseCount + CountPerRank × rank bombs on the same trajectory, at least
// LaunchGap s apart; each deals (TotalBase + TotalPerRank × rank) / (count + 1) × base; one
// target takes at most (CapBase + CapPerRank × rank) × base; blast Blast ×; the shot's falls
// play FallSpeed × faster; a bomb explodes at least HitGap s after the last fall landed;
// every enemy damaged is rooted for RootTurns.
type ChainTuning struct {
	BaseCount    int     `json:"baseCount"`
	CountPerRank int     `json:"countPerRank"`
	LaunchGap    float64 `json:"launchGap"`
	TotalBase    float64 `json:"totalBase"`
	TotalPerRank float64 `json:"totalPerRank"`
	CapBase      float64 `json:"capBase"`
	CapPerRank   float64 `json:"capPerRank"`
	Blast        float64 `json:"blast"`
	FallSpeed    float64 `json:"fallSpeed"`
	HitGap       float64 `json:"hitGap"`
	RootTurns    int     `json:"rootTurns"`
}

// LaserTuning: a straight beam to the map edge hitting every enemy within HalfWidth + the hit
// radius of it, nearest first: (FirstBase + FirstPerRank × rank) × base, each next × Decay;
// the beam shows at ChargeTime for BeamTime s, its front sweeping at SweepSpeed; every enemy
// hit is stunned for StunTurns.
type LaserTuning struct {
	HalfWidth    float64 `json:"halfWidth"`
	FirstBase    float64 `json:"firstBase"`
	FirstPerRank float64 `json:"firstPerRank"`
	Decay        float64 `json:"decay"`
	ChargeTime   float64 `json:"chargeTime"`
	BeamTime     float64 `json:"beamTime"`
	SweepSpeed   float64 `json:"sweepSpeed"`
	StunTurns    int     `json:"stunTurns"`
}

// BloomTuning: heals Flora SelfBase + PerRank × rank % and every teammate AllyBase + PerRank ×
// rank % of max HP; the seeds rise for RiseTime to SkyY, cross for CrossTime and fall for
// FallTime (at up to FallSpeed when they leave the sky); the heal lands at the sum, the replay
// lingers Linger s more.
type BloomTuning struct {
	SelfBase  int     `json:"selfBase"`
	AllyBase  int     `json:"allyBase"`
	PerRank   int     `json:"perRank"`
	RiseTime  float64 `json:"riseTime"`
	CrossTime float64 `json:"crossTime"`
	FallTime  float64 `json:"fallTime"`
	SkyY      float64 `json:"skyY"`
	FallSpeed float64 `json:"fallSpeed"`
	Linger    float64 `json:"linger"`
}

// SeedTime is when a bloom seed lands (s since the shot).
func (b BloomTuning) SeedTime() float64 { return b.RiseTime + b.CrossTime + b.FallTime }

// DefaultRainTuning … DefaultBloomTuning are the tunings the skills played with before they
// moved into config_skills.params (playtest round 6; what 041_gamedata_rules seeds).
func DefaultRainTuning() RainTuning {
	return RainTuning{BaseCount: 12, CountPerRank: 4, SpreadBase: 90, SpreadPerRank: 15, Focus: 2, Delay: 0.5, Interval: 0.05,
		StartY: -60, DropSpeed: 1500, Damage: 0.25, Blast: 0.7, Crater: 1, Cap: 2, BurnTurns: 1}
}

func DefaultChainTuning() ChainTuning {
	return ChainTuning{BaseCount: 2, CountPerRank: 1, LaunchGap: 0.1, TotalBase: 1.8, TotalPerRank: 0.15, CapBase: 1.2,
		CapPerRank: 0.15, Blast: 1.3, FallSpeed: 3, HitGap: 0.08, RootTurns: 1}
}

func DefaultLaserTuning() LaserTuning {
	return LaserTuning{HalfWidth: 10, FirstBase: 1.5, FirstPerRank: 0.1, Decay: 0.8, ChargeTime: 0.3, BeamTime: 0.6,
		SweepSpeed: 12000, StunTurns: 1}
}

func DefaultBloomTuning() BloomTuning {
	return BloomTuning{SelfBase: 40, AllyBase: 30, PerRank: 5, RiseTime: 0.55, CrossTime: 0.2, FallTime: 0.65, SkyY: -140,
		FallSpeed: 400, Linger: 0.9}
}

// ParamSpec is one tuning number of an effect type: its key in config_skills.params, the
// admin label and help, and its valid range (inclusive).
type ParamSpec struct {
	Key   string
	Label string
	Desc  string
	Min   float64
	Max   float64
}

// SkillParamSpecs are the tuning numbers of each effect type, in admin order.
var SkillParamSpecs = map[string][]ParamSpec{
	EffectMissileRain: {
		{"baseCount", "Số tên lửa (cấp 0)", "", 1, 200},
		{"countPerRank", "Thêm mỗi cấp súng", "", 0, 100},
		{"spreadBase", "Bán kính rơi (px)", "Cấp 0: tên lửa rơi trong ± chừng này px quanh pháo hiệu", 0, 2000},
		{"spreadPerRank", "Bán kính thêm mỗi cấp (px)", "", 0, 1000},
		{"focus", "Độ dồn về tâm", "Vị trí = bán kính × |v|^focus: lớn hơn = dồn về pháo hiệu", 0.1, 10},
		{"delay", "Chờ sau khi pháo hiệu đáp (giây)", "", 0, 10},
		{"interval", "Cách nhau (giây)", "", 0, 2},
		{"startY", "Độ cao bắt đầu (px)", "Âm = trên mép bản đồ", -2000, 0},
		{"dropSpeed", "Tốc độ rơi (px/s)", "", 1, 20000},
		{"damage", "Sát thương mỗi quả (× sát thương gốc)", "", 0, 10},
		{"blast", "Vùng nổ (× của vũ khí)", "", 0, 10},
		{"crater", "Hố (× của vũ khí)", "", 0, 10},
		{"cap", "Tối đa mỗi mục tiêu (× sát thương gốc)", "", 0, 100},
		{"burnTurns", "Số lượt bỏng", "", 0, 20},
	},
	EffectBombChain: {
		{"baseCount", "Số bom (cấp 0)", "", 1, 50},
		{"countPerRank", "Thêm mỗi cấp súng", "", 0, 20},
		{"launchGap", "Cách nhau tối thiểu (giây)", "", 0, 5},
		{"totalBase", "Tổng sát thương cấp 0 (× gốc)", "Mỗi quả = tổng / (số bom + 1)", 0, 100},
		{"totalPerRank", "Tổng thêm mỗi cấp (× gốc)", "", 0, 10},
		{"capBase", "Tối đa mỗi mục tiêu cấp 0 (× gốc)", "", 0, 100},
		{"capPerRank", "Tối đa thêm mỗi cấp (× gốc)", "", 0, 10},
		{"blast", "Vùng nổ (× của vũ khí)", "", 0, 10},
		{"fallSpeed", "Tốc độ rơi khi phát lại (× rơi tự do)", "", 1, 20},
		{"hitGap", "Nổ sau khi rơi xong (giây)", "", 0, 2},
		{"rootTurns", "Số lượt cầm chân", "", 0, 20},
	},
	EffectLaser: {
		{"halfWidth", "Nửa bề rộng tia (px)", "Trúng địch trong halfWidth + bán kính va chạm của tia", 0, 500},
		{"firstBase", "Sát thương địch đầu tiên cấp 0 (× gốc)", "", 0, 100},
		{"firstPerRank", "Thêm mỗi cấp (× gốc)", "", 0, 10},
		{"decay", "Mỗi địch sau (× địch trước)", "0..1", 0, 1},
		{"chargeTime", "Nạp tia (giây)", "", 0, 5},
		{"beamTime", "Tia hiện (giây)", "", 0, 10},
		{"sweepSpeed", "Tốc độ quét (px/s)", "", 1, 1000000},
		{"stunTurns", "Số lượt choáng", "", 0, 20},
	},
	EffectBloom: {
		{"selfBase", "Hồi Flora cấp 0 (% máu tối đa)", "", 0, 1000},
		{"allyBase", "Hồi đồng đội cấp 0 (% máu tối đa)", "", 0, 1000},
		{"perRank", "Thêm mỗi cấp súng (%)", "", 0, 100},
		{"riseTime", "Hạt bay lên (giây)", "", 0.01, 10},
		{"crossTime", "Hạt bay ngang (giây)", "", 0.01, 10},
		{"fallTime", "Hạt rơi xuống (giây)", "", 0.01, 10},
		{"skyY", "Độ cao trên trời (px)", "Âm = trên mép bản đồ", -5000, 0},
		{"fallSpeed", "Tốc độ rơi tối đa (px/s)", "", 0, 20000},
		{"linger", "Hoa nở thêm (giây)", "", 0, 10},
	},
}

// DefaultSkillParams is the default tuning of an effect type as a params object (nil for an
// effect type without tuning).
func DefaultSkillParams(effectType string) map[string]float64 {
	switch effectType {
	case EffectMissileRain:
		return TuningParams(DefaultRainTuning())
	case EffectBombChain:
		return TuningParams(DefaultChainTuning())
	case EffectLaser:
		return TuningParams(DefaultLaserTuning())
	case EffectBloom:
		return TuningParams(DefaultBloomTuning())
	}
	return nil
}

// TuningParams is a tuning struct as a params object (json key → number).
func TuningParams(v any) map[string]float64 {
	raw, _ := json.Marshal(v)
	out := map[string]float64{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// RainTuningOf … BloomTuningOf are a skill's tuning: the effect type's defaults overlaid with
// every valid value of its params.
func RainTuningOf(s SkillConfig) RainTuning {
	t := DefaultRainTuning()
	overlay(EffectMissileRain, s.Params, &t)
	return t
}

func ChainTuningOf(s SkillConfig) ChainTuning {
	t := DefaultChainTuning()
	overlay(EffectBombChain, s.Params, &t)
	return t
}

func LaserTuningOf(s SkillConfig) LaserTuning {
	t := DefaultLaserTuning()
	overlay(EffectLaser, s.Params, &t)
	return t
}

func BloomTuningOf(s SkillConfig) BloomTuning {
	t := DefaultBloomTuning()
	overlay(EffectBloom, s.Params, &t)
	return t
}

// overlay sets every field of *t (by json tag) that params holds a valid value for.
func overlay(effectType string, params map[string]float64, t any) {
	if len(params) == 0 {
		return
	}
	v := reflect.ValueOf(t).Elem()
	for _, spec := range SkillParamSpecs[effectType] {
		x, ok := params[spec.Key]
		if !ok || spec.check(x, intField(v, spec.Key)) != nil {
			continue
		}
		f := fieldByTag(v, spec.Key)
		if f.Kind() == reflect.Int {
			f.SetInt(int64(x))
		} else {
			f.SetFloat(x)
		}
	}
}

func fieldByTag(v reflect.Value, key string) reflect.Value {
	for i := 0; i < v.NumField(); i++ {
		if tagName(v.Type().Field(i)) == key {
			return v.Field(i)
		}
	}
	panic("gamedata: no tuning field " + key)
}

func intField(v reflect.Value, key string) bool { return fieldByTag(v, key).Kind() == reflect.Int }

// check validates one param value.
func (p ParamSpec) check(x float64, whole bool) error {
	if math.IsNaN(x) || math.IsInf(x, 0) || x < p.Min || x > p.Max {
		return fmt.Errorf("%s: %v is outside %v..%v", p.Key, x, p.Min, p.Max)
	}
	if whole && x != math.Trunc(x) {
		return fmt.Errorf("%s: %v is not a whole number", p.Key, x)
	}
	return nil
}

// tuningValue is the empty tuning struct of an effect type (nil when it has none).
func tuningValue(effectType string) reflect.Value {
	switch effectType {
	case EffectMissileRain:
		return reflect.ValueOf(&RainTuning{}).Elem()
	case EffectBombChain:
		return reflect.ValueOf(&ChainTuning{}).Elem()
	case EffectLaser:
		return reflect.ValueOf(&LaserTuning{}).Elem()
	case EffectBloom:
		return reflect.ValueOf(&BloomTuning{}).Elem()
	}
	return reflect.Value{}
}

// ParamIsWhole reports whether an effect type's param is a whole number.
func ParamIsWhole(effectType, key string) bool {
	v := tuningValue(effectType)
	return v.IsValid() && intField(v, key)
}

// ValidateSkillParams checks a params object for an effect type: every key known, every value
// in range (a whole number where the tuning has one). An effect type without tuning takes
// only an empty object.
func ValidateSkillParams(effectType string, params map[string]float64) error {
	specs := SkillParamSpecs[effectType]
	known := map[string]ParamSpec{}
	for _, s := range specs {
		known[s.Key] = s
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s, ok := known[k]
		if !ok {
			return fmt.Errorf("unknown param %q for effect type %q", k, effectType)
		}
		if err := s.check(params[k], ParamIsWhole(effectType, k)); err != nil {
			return err
		}
	}
	return nil
}

// FormatParam prints a param value in its shortest form.
func FormatParam(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }

// ParseParamsForm reads an effect type's params from admin form values ("param_<key>"): every
// param of the type is required.
func ParseParamsForm(effectType string, get func(name string) string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, s := range SkillParamSpecs[effectType] {
		text := strings.TrimSpace(get("param_" + s.Key))
		if text == "" {
			return nil, fmt.Errorf("%s: required", s.Label)
		}
		x, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a number", s.Label, text)
		}
		if err := s.check(x, ParamIsWhole(effectType, s.Key)); err != nil {
			return nil, fmt.Errorf("%s (%v)", s.Label, err)
		}
		out[s.Key] = x
	}
	return out, nil
}
