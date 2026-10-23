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

// Rules are the gameplay rules and balance numbers that are not a config table column: one
// game_settings JSON row per group (RuleGroups, keys "rules.<group>"). A row or a field that
// is missing, or whose value is not valid (wrong type or out of its range), keeps its
// DefaultRules value, so the Go defaults are only fallbacks: the migration
// 041_gamedata_rules seeds every row with them and admins edit them in the dashboard.
//
// The scalar game_settings rows that predate these groups stay where they are and are parsed
// by PhysicsFromSettings: physics.* (the shell flight), combat.defense_k,
// match.turn_time_seconds, match.idle_timeout_minutes, move.step_pixels,
// move.energy_cost_per_2px, fall.damage_threshold, fall.damage_per_pixel (ScalarSettings).
// Some rules are item values (config_items.value: power_shot's damage factor, spider_net's
// move energy cap, drill_bomb's pierce, ...), skill tuning is config_skills.params, status
// effect rules are config_status_effects columns and the rank bands config_rank_tiers columns.
type Rules struct {
	Physics     ShotRules        `json:"physics"`
	Aim         AimRules         `json:"aim"`
	AimAid      AimAidRules      `json:"aim_aid"`
	Combat      CombatRules      `json:"combat"`
	Terrain     TerrainRules     `json:"terrain"`
	Movement    MovementRules    `json:"movement"`
	Timings     TimingRules      `json:"timings"`
	SkillEnergy SkillEnergyRules `json:"skill_energy"`
	SkillRanks  SkillRankRules   `json:"skill_ranks"`
	Items       ItemRules        `json:"items"`
	Player      PlayerRules      `json:"player"`
	Equipment   EquipmentRules   `json:"equipment"`
	Economy     EconomyRules     `json:"economy"`
	Bots        BotRules         `json:"bots"`
	Elements    ElementRules     `json:"elements"`
}

// ShotRules (rules.physics) complete the physics.* rows: how a shell's flight is checked.
type ShotRules struct {
	CollisionStepPx  float64 `json:"collisionStepPx"`  // each flight step is checked for hits every this many px
	ShotOriginLift   int     `json:"shotOriginLift"`   // px above the feet a shot starts: the body centre hits are measured to
	OutOfBoundsLeft  float64 `json:"outOfBoundsLeft"`  // a flight ends at x < −this,
	OutOfBoundsRight float64 `json:"outOfBoundsRight"` // x > map width + this
	OutOfBoundsBelow float64 `json:"outOfBoundsBelow"` // or y > map height + this
	// A bouncing shell (weapon bounces > 0, 054: Tide's water orb) leaves its terrain contact with
	// bounceRestitution × its speed along the surface normal (reversed) and bounceFriction × its
	// speed along the surface.
	BounceRestitution float64 `json:"bounceRestitution"`
	BounceFriction    float64 `json:"bounceFriction"`
}

// AimRules (rules.aim): what a Shoot may carry.
type AimRules struct {
	PowerMax   float64 `json:"powerMax"`   // the server rejects a Shoot with more power
	AngleMin   float64 `json:"angleMin"`   // the client's slingshot range (client only)
	AngleMax   float64 `json:"angleMax"`   //
	NoAimAngle float64 `json:"noAimAngle"` // what a skill that ignores the aim is sent with
	NoAimPower float64 `json:"noAimPower"` //
}

// AimAidRules (rules.aim_aid): the client's aiming guide (client only).
type AimAidRules struct {
	GuideArcLength      map[string]float64 `json:"guideArcLength"` // rank tier → px of the flight the guide draws
	HitToleranceMax     float64            `json:"hitToleranceMax"`
	HitToleranceMinPart float64            `json:"hitToleranceMinPart"`
	HitToleranceFarPx   float64            `json:"hitToleranceFarPx"`
	HitToleranceHighPx  float64            `json:"hitToleranceHighPx"`
}

// CombatRules (rules.combat).
type CombatRules struct {
	LavaDamage       int     `json:"lavaDamage"`       // HP lost at the start of a turn on lava (no defense)
	CritMultiplier   float64 `json:"critMultiplier"`   // a critical hit deals this × the damage
	BurnMaxHPPercent int     `json:"burnMaxHpPercent"` // burn: % of max HP lost at the start of the target's turn
	ShieldCapPercent int     `json:"shieldCapPercent"` // a shield never exceeds this % of max HP
}

// TerrainRules (rules.terrain): walking and craters.
type TerrainRules struct {
	PlayerHeight                 int     `json:"playerHeight"`                 // free px a player needs above the ground
	MaxClimbPerStep              int     `json:"maxClimbPerStep"`              // px a player climbs per 1 px step
	CraterPxPerTerrainDamage     float64 `json:"craterPxPerTerrainDamage"`     // crater radius per weapon terrain damage point
	BowlDepth                    float64 `json:"bowlDepth"`                    // a crater's floor lies this × r under the impact
	BowlCeiling                  float64 `json:"bowlCeiling"`                  // a crater carves up to this × r above it
	MaxTerrainDamageBonusPercent int     `json:"maxTerrainDamageBonusPercent"` // the terrain damage bonus grows craters by at most this %
}

// MovementRules (rules.movement); how far a walk reaches and costs is move.step_pixels /
// move.energy_cost_per_2px (PhysicsConfig), the net's cap the spider_net item value.
type MovementRules struct {
	DefaultMoveEnergy int `json:"defaultMoveEnergy"` // base move energy of a character with mobility <= 0
}

// TimingRules (rules.timings); the turn length is match.turn_time_seconds.
type TimingRules struct {
	ActionIntervalSec   float64 `json:"actionIntervalSec"`   // the server drops actions closer together
	BotFollowUpDelaySec float64 `json:"botFollowUpDelaySec"` // a bot waits this long after a heal or move
	SkipTurnDelaySec    float64 `json:"skipTurnDelaySec"`    // a stunned / frozen turn ends after this
}

// SkillEnergyRules (rules.skill_energy).
type SkillEnergyRules struct {
	Max     int `json:"max"`     // a skill can be used at this energy
	PerTurn int `json:"perTurn"` // + at the end of each own turn
	OnHit   int `json:"onHit"`   // + once per enemy action that takes HP or shield
}

// SkillRankRules (rules.skill_ranks): a player's skill rank is ByTierWord[the part after the
// last "_" of its weapon item id], Default for no weapon or an unknown word.
type SkillRankRules struct {
	ByTierWord map[string]int `json:"byTierWord"`
	Default    int            `json:"default"`
}

// ItemRules (rules.items).
type ItemRules struct {
	MaxLoadout     int     `json:"maxLoadout"`     // items a player brings into a match
	AirStrikeAngle float64 `json:"airStrikeAngle"` // an air strike falls from (targetX, 0) at this angle
	AirStrikePower float64 `json:"airStrikePower"` // and power, whatever the Shoot carries
}

// PlayerRules (rules.player).
type PlayerRules struct {
	ExpPerLevel        int    `json:"expPerLevel"`        // a player at level N needs N × this EXP for N + 1
	DefaultCharacterID string `json:"defaultCharacterId"` // owned by everyone, played when none is picked
}

// EquipmentRules (rules.equipment); the milestone bonuses, merge rate, sell rate and weapon
// cost multiplier are the equipment.* rows.
type EquipmentRules struct {
	UpgradeStepPerLevel float64 `json:"upgradeStepPerLevel"` // each +level adds this to a piece's stat multiplier
	MaxUpgradeLevel     int     `json:"maxUpgradeLevel"`
	MaxStoneLevel       int     `json:"maxStoneLevel"`
	MergeMinCount       int     `json:"mergeMinCount"`
	MergeMaxCount       int     `json:"mergeMaxCount"`
}

// EconomyRules (rules.economy).
type EconomyRules struct {
	CoinsPerGem int `json:"coinsPerGem"` // the base gem → coin rate the shop compares coin packs with (client only)
}

// BotRules (rules.bots): how the bots of matchmaking and "Đấu với máy" are equipped and use
// their items. The aim (hit chance), item use chance, movement and medkit forget chance per
// difficulty tier are the bot_difficulty row.
type BotRules struct {
	LevelSpread           int      `json:"levelSpread"`           // a bot's level is the humans' average ± up to this
	WeaponUpgradeMin      int      `json:"weaponUpgradeMin"`      // a bot's gun is upgraded at least this
	WeaponUpgradePerLevel float64  `json:"weaponUpgradePerLevel"` // and at most the gun's required level × this
	DefaultItems          []string `json:"defaultItems"`          // every bot brings these
	RandomItems           []string `json:"randomItems"`           // plus RandomItemCount of these at random
	RandomItemCount       int      `json:"randomItemCount"`
	MedkitHPPercent       int      `json:"medkitHpPercent"`       // a bot heals at or below this % of its max HP
	TeleportLavaMarginPx  int      `json:"teleportLavaMarginPx"`  // a teleport never lands this close to lava
	TeleportEnemyMarginPx int      `json:"teleportEnemyMarginPx"` // nor this close (horizontally) to an enemy
}

// ElementRules (rules.elements, 054): the element damage multipliers (gamedata.ElementBeats; the
// match's elementFactor). PvP (every match but boss mode): an attacker whose element beats the
// target's deals + PvpStrongPct %, one whose element the target's beats − PvpWeakPct %. Boss mode:
// a party member whose element beats the boss's (its minions carry it) deals the boss side +
// BossStrongPct %; the boss side deals a party member whose element the boss's beats + BossTakenPct %.
type ElementRules struct {
	PvpStrongPct  int `json:"pvpStrongPct"`
	PvpWeakPct    int `json:"pvpWeakPct"`
	BossStrongPct int `json:"bossStrongPct"`
	BossTakenPct  int `json:"bossTakenPct"`
}

// DefaultRules are the rules the game played with before they moved into the DB (and what
// 041_gamedata_rules seeds; rules.elements 054_elements).
func DefaultRules() *Rules {
	return &Rules{
		Physics: ShotRules{CollisionStepPx: 4, ShotOriginLift: 20, OutOfBoundsLeft: 100, OutOfBoundsRight: 100, OutOfBoundsBelow: 50,
			BounceRestitution: 0.55, BounceFriction: 0.85},
		Aim: AimRules{PowerMax: 100, AngleMin: 0, AngleMax: 180, NoAimAngle: 90, NoAimPower: 50},
		AimAid: AimAidRules{
			GuideArcLength:  map[string]float64{"bronze": 700, "silver": 650, "gold": 600, "platinum": 550, "diamond": 500, "master": 450},
			HitToleranceMax: 20, HitToleranceMinPart: 0.2, HitToleranceFarPx: 1200, HitToleranceHighPx: 700,
		},
		Combat: CombatRules{LavaDamage: 75, CritMultiplier: 1.5, BurnMaxHPPercent: 10, ShieldCapPercent: 50},
		Terrain: TerrainRules{PlayerHeight: 40, MaxClimbPerStep: 3, CraterPxPerTerrainDamage: 0.6, BowlDepth: 0.72, BowlCeiling: 1.5,
			MaxTerrainDamageBonusPercent: 50},
		Movement:    MovementRules{DefaultMoveEnergy: DefaultMoveEnergy},
		Timings:     TimingRules{ActionIntervalSec: 0.15, BotFollowUpDelaySec: 1.5, SkipTurnDelaySec: 2},
		SkillEnergy: SkillEnergyRules{Max: 100, PerTurn: 20, OnHit: 10},
		SkillRanks: SkillRankRules{ByTierWord: map[string]int{
			"lv10": 1, "lv20": 2, "lv30": 3, "lv40": 4,
			"iron": 1, "steel": 2, "mithril": 3, "dragon": 4,
			"silver": 2, "gold": 3, "titan": 4, "diamond": 4,
		}},
		Items:     ItemRules{MaxLoadout: 3, AirStrikeAngle: 270, AirStrikePower: 80},
		Player:    PlayerRules{ExpPerLevel: 100, DefaultCharacterID: "rookie"},
		Equipment: EquipmentRules{UpgradeStepPerLevel: 0.02, MaxUpgradeLevel: 16, MaxStoneLevel: 12, MergeMinCount: 2, MergeMaxCount: 4},
		Economy:   EconomyRules{CoinsPerGem: 100},
		Bots: BotRules{LevelSpread: 3, WeaponUpgradeMin: 0, WeaponUpgradePerLevel: 0.2,
			DefaultItems:    []string{"medkit", "teleport"},
			RandomItems:     []string{"power_shot", "drill_bomb", "spider_net", "freeze_bomb", "air_strike", "wind_stopper"},
			RandomItemCount: 1, MedkitHPPercent: 35, TeleportLavaMarginPx: 40, TeleportEnemyMarginPx: 120},
		Elements: ElementRules{PvpStrongPct: 10, PvpWeakPct: 10, BossStrongPct: 20, BossTakenPct: 10},
	}
}

// MaxSkillRank is the highest rank SkillRanks gives (at least Default).
func (r *Rules) MaxSkillRank() int {
	m := r.SkillRanks.Default
	for _, v := range r.SkillRanks.ByTierWord {
		m = max(m, v)
	}
	return m
}

// Field is one value of a rules group: its key in the row's JSON object, the admin label and
// help, and the valid range (inclusive; each value of a map). A string field must not be
// empty; a list of strings may be, but none of its entries.
type Field struct {
	Key   string
	Label string
	Desc  string
	Min   float64
	Max   float64
}

// Group is one game_settings JSON row of Rules.
type Group struct {
	Key    string // game_settings key: "rules." + the Rules field's json tag
	Title  string
	Desc   string
	Fields []Field
}

// RuleGroupPrefix starts every Rules row key.
const RuleGroupPrefix = "rules."

// RuleGroups are the Rules rows in admin order.
var RuleGroups = []Group{
	{Key: "rules.physics", Title: "Đường bay của đạn", Desc: "Bổ sung các dòng physics.*: kiểm tra va chạm và giới hạn bản đồ.", Fields: []Field{
		{"collisionStepPx", "Bước kiểm tra va chạm (px)", "Mỗi bước bay được kiểm tra va chạm sau mỗi chừng này px", 0.5, 50},
		{"shotOriginLift", "Điểm bắn trên chân (px)", "Đạn bắt đầu cao hơn chân chừng này px: tâm thân, cũng là tâm va chạm", 0, 200},
		{"outOfBoundsLeft", "Ra ngoài bên trái (px)", "Đường bay kết thúc khi x < −giá trị này", 0, 5000},
		{"outOfBoundsRight", "Ra ngoài bên phải (px)", "Đường bay kết thúc khi x > chiều rộng bản đồ + giá trị này", 0, 5000},
		{"outOfBoundsBelow", "Ra ngoài phía dưới (px)", "Đường bay kết thúc khi y > chiều cao bản đồ + giá trị này", 0, 5000},
		{"bounceRestitution", "Nảy: hệ số bật lại", "Đạn nảy (vũ khí bounces > 0) bật khỏi mặt đất với chừng này × tốc độ vuông góc mặt đất", 0, 1},
		{"bounceFriction", "Nảy: hệ số trượt", "… và chừng này × tốc độ dọc mặt đất", 0, 1},
	}},
	{Key: "rules.aim", Title: "Ngắm bắn", Fields: []Field{
		{"powerMax", "Lực bắn tối đa", "Server từ chối phát bắn mạnh hơn", 1, 1000},
		{"angleMin", "Góc nhỏ nhất (độ)", "Giới hạn ná của client (server nhận mọi góc)", -360, 360},
		{"angleMax", "Góc lớn nhất (độ)", "Giới hạn ná của client", -360, 360},
		{"noAimAngle", "Góc khi không ngắm", "Góc gửi kèm kỹ năng không cần ngắm", -360, 360},
		{"noAimPower", "Lực khi không ngắm", "Lực gửi kèm kỹ năng không cần ngắm (và tia laze)", 0, 1000},
	}},
	{Key: "rules.aim_aid", Title: "Hỗ trợ ngắm (client)", Desc: "Đường ngắm của client: độ dài theo hạng, sai số báo \"Trúng!\".", Fields: []Field{
		{"guideArcLength", "Độ dài đường ngắm theo hạng (px)", "Mỗi hạng: số px đường bay thật mà đường ngắm vẽ", 0, 20000},
		{"hitToleranceMax", "Sai số trúng tối đa (px)", "r = max × min(1, minPart + (1 − minPart) × max(D / farPx, H / highPx))", 0, 500},
		{"hitToleranceMinPart", "Phần sai số tối thiểu", "0..1", 0, 1},
		{"hitToleranceFarPx", "Khoảng cách xa (px)", "D: khoảng cách ngang tới điểm rơi", 1, 20000},
		{"hitToleranceHighPx", "Độ cao (px)", "H: độ cao đỉnh đường bay so với điểm bắn", 1, 20000},
	}},
	{Key: "rules.combat", Title: "Chiến đấu", Desc: "Hằng số phòng thủ K là dòng combat.defense_k; hệ số Tăng lực là giá trị item power_shot.", Fields: []Field{
		{"lavaDamage", "Sát thương dung nham", "Máu mất đầu lượt khi đứng trên dung nham (bỏ qua phòng thủ)", 0, 100000},
		{"critMultiplier", "Hệ số chí mạng", "Đòn chí mạng gây chừng này × sát thương", 1, 10},
		{"burnMaxHpPercent", "Bỏng (% máu tối đa)", "Mục tiêu bị bỏng mất chừng này % máu tối đa đầu lượt của nó", 0, 100},
		{"shieldCapPercent", "Khiên tối đa (% máu)", "Khiên không bao giờ vượt quá chừng này % máu tối đa", 0, 1000},
	}},
	{Key: "rules.terrain", Title: "Địa hình", Desc: "Ngưỡng và sát thương rơi là các dòng fall.*.", Fields: []Field{
		{"playerHeight", "Chiều cao nhân vật (px)", "Khoảng trống cần trên mặt đất", 1, 400},
		{"maxClimbPerStep", "Leo tối đa mỗi bước (px)", "Số px leo lên được mỗi 1 px đi ngang", 0, 100},
		{"craterPxPerTerrainDamage", "Bán kính hố / điểm phá địa hình", "Bán kính hố = terrain damage của vũ khí × giá trị này", 0, 10},
		{"bowlDepth", "Độ sâu hố (× bán kính)", "Đáy hố nằm dưới điểm nổ chừng này × r", 0.05, 5},
		{"bowlCeiling", "Trần hố (× bán kính)", "Hố khoét lên trên điểm nổ tối đa chừng này × r", 0, 10},
		{"maxTerrainDamageBonusPercent", "Bonus phá địa hình tối đa (%)", "Bonus phá địa hình của người bắn làm hố to thêm tối đa chừng này %", 0, 1000},
	}},
	{Key: "rules.movement", Title: "Di chuyển", Desc: "Tầm đi và năng lượng tốn là các dòng move.*; giới hạn khi bị lưới là giá trị item spider_net.", Fields: []Field{
		{"defaultMoveEnergy", "Năng lượng di chuyển mặc định", "Của nhân vật có mobility <= 0", 1, 100000},
	}},
	{Key: "rules.timings", Title: "Thời gian", Desc: "Thời gian mỗi lượt là dòng match.turn_time_seconds.", Fields: []Field{
		{"actionIntervalSec", "Khoảng cách hành động (giây)", "Server bỏ hành động của người chơi gửi gần nhau hơn", 0, 5},
		{"botFollowUpDelaySec", "Bot chờ sau hồi máu / di chuyển (giây)", "", 0, 30},
		{"skipTurnDelaySec", "Mất lượt kết thúc sau (giây)", "Lượt bị choáng / đóng băng kết thúc sau chừng này giây", 0, 30},
	}},
	{Key: "rules.skill_energy", Title: "Năng lượng kỹ năng", Fields: []Field{
		{"max", "Năng lượng để dùng kỹ năng", "", 1, 100000},
		{"perTurn", "Cộng mỗi cuối lượt", "", 0, 100000},
		{"onHit", "Cộng khi bị địch đánh trúng", "Một lần mỗi hành động của địch làm mất máu hoặc khiên", 0, 100000},
	}},
	{Key: "rules.skill_ranks", Title: "Cấp kỹ năng theo súng", Desc: "Cấp kỹ năng = giá trị của từ cuối mã súng (sau dấu _ cuối).", Fields: []Field{
		{"byTierWord", "Cấp theo từ cuối mã súng", "lv10 … lv40, iron … dragon, silver … diamond", 0, 20},
		{"default", "Cấp mặc định", "Không có súng hoặc từ không có trong bảng", 0, 20},
	}},
	{Key: "rules.items", Title: "Vật phẩm", Desc: "Giá trị của từng vật phẩm sửa ở trang Items.", Fields: []Field{
		{"maxLoadout", "Số vật phẩm mang vào trận", "", 0, 20},
		{"airStrikeAngle", "Không kích: góc", "Bom rơi từ (targetX, 0) với góc này (270 = thẳng xuống)", -360, 360},
		{"airStrikePower", "Không kích: lực", "", 0, 1000},
	}},
	{Key: "rules.player", Title: "Người chơi", Desc: "Đường cấp nhân vật là dòng character_levels.", Fields: []Field{
		{"expPerLevel", "EXP mỗi cấp người chơi", "Cấp N cần N × giá trị này EXP để lên N + 1", 1, 1000000},
		{"defaultCharacterId", "Nhân vật mặc định", "Ai cũng có, dùng khi chưa chọn (mã trong Characters)", 0, 0},
	}},
	{Key: "rules.equipment", Title: "Trang bị", Desc: "Bonus mốc, tỉ lệ ghép, tỉ lệ bán, hệ số đá vũ khí là các dòng equipment.*.", Fields: []Field{
		{"upgradeStepPerLevel", "Tăng mỗi cấp cường hóa", "Hệ số chỉ số = 1 + cấp × giá trị này + bonus các mốc", 0, 1},
		{"maxUpgradeLevel", "Cấp cường hóa tối đa", "", 1, 100},
		{"maxStoneLevel", "Cấp đá tối đa", "", 1, 100},
		{"mergeMinCount", "Ghép: số viên tối thiểu", "", 1, 100},
		{"mergeMaxCount", "Ghép: số viên tối đa", "", 1, 100},
	}},
	{Key: "rules.economy", Title: "Kinh tế", Fields: []Field{
		{"coinsPerGem", "Coin mỗi gem (gốc)", "Gói coin chỉ hiện bonus khi tốt hơn tỉ lệ này (client)", 1, 1000000},
	}},
	{Key: "rules.bots", Title: "Bot", Desc: "Bot của Xếp hạng và Đấu với máy: cấp, súng, vật phẩm. Độ chính xác, tỉ lệ dùng item, di chuyển, quên hồi máu theo hạng sửa ở Matchmaking (bot_difficulty).", Fields: []Field{
		{"levelSpread", "Lệch cấp tối đa", "Cấp bot = cấp trung bình của người chơi ± tối đa chừng này (từ 1 tới cấp tối đa)", 0, 1000},
		{"weaponUpgradeMin", "Cường hóa súng tối thiểu", "Súng của bot được cường hóa ít nhất chừng này", 0, 100},
		{"weaponUpgradePerLevel", "Cường hóa tối đa / cấp yêu cầu của súng", "Cường hóa tối đa = cấp yêu cầu của súng × giá trị này (không quá cấp cường hóa tối đa)", 0, 10},
		{"defaultItems", "Vật phẩm mặc định", "Mã vật phẩm mọi bot mang, cách nhau bằng dấu phẩy", 0, 0},
		{"randomItems", "Vật phẩm ngẫu nhiên", "Bot mang thêm vật phẩm ngẫu nhiên trong danh sách này (để trống: mọi vật phẩm bot biết dùng)", 0, 0},
		{"randomItemCount", "Số vật phẩm ngẫu nhiên", "", 0, 20},
		{"medkitHpPercent", "Hồi máu khi máu ≤ (%)", "Bot dùng hộp cứu thương khi máu còn chừng này % máu tối đa trở xuống", 1, 100},
		{"teleportLavaMarginPx", "Dịch chuyển: cách dung nham (px)", "Bot không dịch chuyển tới chỗ cách dung nham gần hơn chừng này", 0, 2000},
		{"teleportEnemyMarginPx", "Dịch chuyển: cách địch (px)", "Bot không dịch chuyển tới chỗ cách địch (theo chiều ngang) gần hơn chừng này", 0, 5000},
	}},
	{Key: "rules.elements", Title: "Ngũ hành", Desc: "Hệ của nhân vật và boss sửa ở Characters / Bosses, vòng khắc ở Elements. Áp dụng cho mọi sát thương từ đơn vị lên đơn vị (đạn, kỹ năng, tia, mưa, tự nổ, nổ khi chết), trước phòng thủ / chí mạng / khiên; không áp dụng cho rơi, ra khỏi bản đồ, bỏng, dung nham, hồi máu.", Fields: []Field{
		{"pvpStrongPct", "PvP: bắn hệ bị khắc (+%)", "Hệ người bắn khắc hệ mục tiêu: sát thương + chừng này %", 0, 100},
		{"pvpWeakPct", "PvP: bắn hệ khắc mình (−%)", "Hệ mục tiêu khắc hệ người bắn: sát thương − chừng này %", 0, 100},
		{"bossStrongPct", "Boss: nhân vật khắc boss (+%)", "Nhân vật có hệ khắc hệ boss gây cho boss và lính của nó + chừng này %", 0, 100},
		{"bossTakenPct", "Boss: boss khắc nhân vật (+%)", "Boss và lính gây cho nhân vật bị hệ boss khắc + chừng này %", 0, 100},
	}},
}

// RuleGroup returns the group with key.
func RuleGroup(key string) (Group, bool) {
	for _, g := range RuleGroups {
		if g.Key == key {
			return g, true
		}
	}
	return Group{}, false
}

// target is the Rules struct field the group fills.
func (g Group) target(r *Rules) reflect.Value {
	name := strings.TrimPrefix(g.Key, RuleGroupPrefix)
	v := reflect.ValueOf(r).Elem()
	for i := 0; i < v.NumField(); i++ {
		if tagName(v.Type().Field(i)) == name {
			return v.Field(i)
		}
	}
	panic("gamedata: no Rules field for " + g.Key)
}

// field is the struct field of target with the json key.
func field(target reflect.Value, key string) reflect.Value {
	for i := 0; i < target.NumField(); i++ {
		if tagName(target.Type().Field(i)) == key {
			return target.Field(i)
		}
	}
	panic("gamedata: no field " + key + " in " + target.Type().Name())
}

func tagName(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("json"), ",")[0]
}

// checkValue decodes raw into a new value of t and checks it against f's range.
func (f Field) checkValue(t reflect.Type, raw json.RawMessage) (reflect.Value, error) {
	ptr := reflect.New(t)
	if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("%s: %s is not a valid %s", f.Key, string(raw), kindName(t))
	}
	v := ptr.Elem()
	inRange := func(x float64) error {
		if math.IsNaN(x) || math.IsInf(x, 0) || x < f.Min || x > f.Max {
			return fmt.Errorf("%s: %v is outside %v..%v", f.Key, x, f.Min, f.Max)
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Int:
		return v, inRange(float64(v.Int()))
	case reflect.Float64:
		return v, inRange(v.Float())
	case reflect.String:
		if strings.TrimSpace(v.String()) == "" {
			return v, fmt.Errorf("%s: required", f.Key)
		}
		return v, nil
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if strings.TrimSpace(v.Index(i).String()) == "" {
				return v, fmt.Errorf("%s: empty entry", f.Key)
			}
		}
		return v, nil
	case reflect.Map:
		if v.Len() == 0 {
			return v, fmt.Errorf("%s: at least one entry is required", f.Key)
		}
		for _, k := range v.MapKeys() {
			if strings.TrimSpace(k.String()) == "" {
				return v, fmt.Errorf("%s: empty key", f.Key)
			}
			e := v.MapIndex(k)
			x := 0.0
			if e.Kind() == reflect.Int {
				x = float64(e.Int())
			} else {
				x = e.Float()
			}
			if err := inRange(x); err != nil {
				return v, fmt.Errorf("%s[%s]: %v", f.Key, k.String(), err)
			}
		}
		return v, nil
	}
	return v, nil
}

func kindName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int:
		return "whole number"
	case reflect.Float64:
		return "number"
	case reflect.String:
		return "text"
	case reflect.Slice:
		return "list of texts"
	case reflect.Map:
		if t.Elem().Kind() == reflect.Int {
			return "map of whole numbers"
		}
		return "map of numbers"
	}
	return t.String()
}

// decode fills the group's fields of r from the row's JSON; a missing or invalid field keeps
// r's value. It returns the problems found.
func (g Group) decode(raw string, r *Rules) []error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return []error{fmt.Errorf("%s: not a JSON object: %w", g.Key, err)}
	}
	target := g.target(r)
	var errs []error
	for _, f := range g.Fields {
		v, ok := obj[f.Key]
		if !ok {
			continue
		}
		dst := field(target, f.Key)
		val, err := f.checkValue(dst.Type(), v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s.%v", g.Key, err))
			continue
		}
		dst.Set(val)
	}
	return errs
}

// Encode is the group's row value for r: a JSON object of every field.
func (g Group) Encode(r *Rules) string {
	target := g.target(r)
	var b strings.Builder
	b.WriteString("{")
	for i, f := range g.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		k, _ := json.Marshal(f.Key)
		v, _ := json.Marshal(field(target, f.Key).Interface())
		b.Write(k)
		b.WriteString(": ")
		b.Write(v)
	}
	b.WriteString("}")
	return b.String()
}

// FormValues are r's group fields as admin form text: numbers in their shortest form, a map
// as "key=value" lines in key order, a list as "a, b".
func (g Group) FormValues(r *Rules) map[string]string {
	target := g.target(r)
	out := map[string]string{}
	for _, f := range g.Fields {
		v := field(target, f.Key)
		switch v.Kind() {
		case reflect.Map:
			keys := v.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
			var lines []string
			for _, k := range keys {
				e := v.MapIndex(k)
				if e.Kind() == reflect.Int {
					lines = append(lines, k.String()+"="+strconv.FormatInt(e.Int(), 10))
				} else {
					lines = append(lines, k.String()+"="+strconv.FormatFloat(e.Float(), 'f', -1, 64))
				}
			}
			out[f.Key] = strings.Join(lines, "\n")
		case reflect.Slice:
			out[f.Key] = strings.Join(v.Interface().([]string), ", ")
		case reflect.Int:
			out[f.Key] = strconv.FormatInt(v.Int(), 10)
		case reflect.Float64:
			out[f.Key] = strconv.FormatFloat(v.Float(), 'f', -1, 64)
		default:
			out[f.Key] = v.String()
		}
	}
	return out
}

// IsMap reports whether the field holds a map (edited as key=value lines).
func (g Group) IsMap(key string) bool {
	return field(g.target(DefaultRules()), key).Kind() == reflect.Map
}

// IsList reports whether the field holds a list of texts (edited as "a, b").
func (g Group) IsList(key string) bool {
	return field(g.target(DefaultRules()), key).Kind() == reflect.Slice
}

// IsText reports whether the field holds text.
func (g Group) IsText(key string) bool {
	return field(g.target(DefaultRules()), key).Kind() == reflect.String
}

// ParseForm validates admin form text for every field of the group (FormValues' format) and
// returns the row value to store, built over base (the current rules).
func (g Group) ParseForm(base *Rules, form map[string]string) (string, error) {
	r := base.Clone()
	target := g.target(r)
	for _, f := range g.Fields {
		text, ok := form[f.Key]
		if !ok {
			return "", fmt.Errorf("%s: required", f.Label)
		}
		dst := field(target, f.Key)
		var raw []byte
		switch dst.Kind() {
		case reflect.Map:
			m := map[string]json.RawMessage{}
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				k, v, found := strings.Cut(line, "=")
				if !found {
					return "", fmt.Errorf("%s: %q is not key=value", f.Label, line)
				}
				m[strings.TrimSpace(k)] = json.RawMessage(strings.TrimSpace(v))
			}
			raw, _ = json.Marshal(m)
		case reflect.Slice:
			list := []string{}
			for _, e := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
				if e = strings.TrimSpace(e); e != "" {
					list = append(list, e)
				}
			}
			raw, _ = json.Marshal(list)
		case reflect.String:
			raw, _ = json.Marshal(strings.TrimSpace(text))
		default:
			raw = []byte(strings.TrimSpace(text))
		}
		val, err := f.checkValue(dst.Type(), raw)
		if err != nil {
			return "", fmt.Errorf("%s (%v)", f.Label, err)
		}
		dst.Set(val)
	}
	if err := r.Validate(); err != nil {
		return "", err
	}
	return g.Encode(r), nil
}

// Validate checks the rules that span fields.
func (r *Rules) Validate() error {
	switch {
	case r.Aim.AngleMin >= r.Aim.AngleMax:
		return fmt.Errorf("aim: angleMin must be below angleMax")
	case r.Aim.NoAimPower > r.Aim.PowerMax:
		return fmt.Errorf("aim: noAimPower must not exceed powerMax")
	case r.Equipment.MergeMinCount > r.Equipment.MergeMaxCount:
		return fmt.Errorf("equipment: mergeMinCount must not exceed mergeMaxCount")
	}
	return nil
}

// Clone is a deep copy of r.
func (r *Rules) Clone() *Rules {
	c := *r
	c.AimAid.GuideArcLength = make(map[string]float64, len(r.AimAid.GuideArcLength))
	for k, v := range r.AimAid.GuideArcLength {
		c.AimAid.GuideArcLength[k] = v
	}
	c.SkillRanks.ByTierWord = make(map[string]int, len(r.SkillRanks.ByTierWord))
	for k, v := range r.SkillRanks.ByTierWord {
		c.SkillRanks.ByTierWord[k] = v
	}
	c.Bots.DefaultItems = append([]string{}, r.Bots.DefaultItems...)
	c.Bots.RandomItems = append([]string{}, r.Bots.RandomItems...)
	return &c
}

// RulesFromSettings builds Rules from the game_settings rows (key → value): DefaultRules,
// overlaid with every valid field of the rules.* rows. errs lists what was ignored.
func RulesFromSettings(settings map[string]string) (r *Rules, errs []error) {
	r = DefaultRules()
	for _, g := range RuleGroups {
		if raw, ok := settings[g.Key]; ok {
			errs = append(errs, g.decode(raw, r)...)
		}
	}
	if err := r.Validate(); err != nil {
		errs = append(errs, err)
	}
	return r, errs
}
