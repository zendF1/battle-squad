package room

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"

	"battle-squad/internal/api/character"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/shared/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Bot loadouts. A matchmade bot or the "Đấu với máy" bot gets a playable character (bots of
// one match differ while there are enough), a level around the humans' average, a real gun
// from config_equipment_items for that character and level with a random upgrade, the
// battle stats a player of that level with that gun has (battlestats, as getActualStats), and
// rules.bots defaultItems plus randomItemCount items of the pool. Nothing is written for a
// bot: no player rows, no inventory, no reservations.

// botWeaponOption is an active config_equipment_items weapon a bot may carry.
type botWeaponOption struct {
	ItemID, Category, Tier, CharacterID string // CharacterID "" = any character
	RequiredLevel                       int
	HP, Damage, Defense, MoveEnergy     int
	Crit                                float64
}

// loadBotWeapons reads the active weapons of config_equipment_items.
func loadBotWeapons(ctx context.Context, q battlestats.Querier) ([]botWeaponOption, error) {
	rows, err := q.Query(ctx, `
		SELECT item_id, category, COALESCE(tier, ''), COALESCE(character_id, ''), required_level,
		       stat_hp, stat_damage, stat_defense, stat_move_energy, stat_crit
		FROM config_equipment_items
		WHERE slot = 'weapon' AND is_active = TRUE
		ORDER BY item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []botWeaponOption
	for rows.Next() {
		var w botWeaponOption
		if err := rows.Scan(&w.ItemID, &w.Category, &w.Tier, &w.CharacterID, &w.RequiredLevel,
			&w.HP, &w.Damage, &w.Defense, &w.MoveEnergy, &w.Crit); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// querier is pool as a Querier, nil without a pool.
func querier(pool *pgxpool.Pool) battlestats.Querier {
	if pool == nil {
		return nil
	}
	return pool
}

// loadBotWeaponsOrNone is loadBotWeapons, no weapons (bots play with their basic gun) when the
// catalog cannot be read.
func loadBotWeaponsOrNone(ctx context.Context, q battlestats.Querier) []botWeaponOption {
	if q == nil {
		return nil
	}
	w, err := loadBotWeapons(ctx, q)
	if err != nil {
		observability.Log.Warn().Err(err).Msg("failed to load the weapons for bots; bots play with their basic gun")
	}
	return w
}

// botLoadout is what a bot brings into a match.
type botLoadout struct {
	CharacterID string
	Level       int
	Items       []string
	Stats       battleStats
	Look        cosmetics
}

// botOutfitter equips the bots of one match from the game data gd and the weapon catalog.
type botOutfitter struct {
	gd      *gamedata.Snapshot
	weapons []botWeaponOption
	intn    func(n int) int // uniform in [0, n)
	used    map[string]bool // characters already given to a bot of this match
}

func newBotOutfitter(gd *gamedata.Snapshot, weapons []botWeaponOption, intn func(n int) int) *botOutfitter {
	if intn == nil {
		intn = rand.IntN
	}
	return &botOutfitter{gd: gd, weapons: weapons, intn: intn, used: map[string]bool{}}
}

// outfit equips one bot for humans of average level avgLevel.
func (o *botOutfitter) outfit(avgLevel int) botLoadout {
	charID := o.character()
	level := o.level(avgLevel)
	l := botLoadout{CharacterID: charID, Level: level, Items: o.items()}
	weapon, ok := o.weapon(charID, level)
	upgrade := 0
	if ok {
		upgrade = o.upgrade(weapon)
		l.Look = cosmetics{WeaponItemID: weapon.ItemID, WeaponUpgradeLevel: upgrade}
	}
	var w *botWeaponOption
	if ok {
		w = &weapon
	}
	l.Stats = botStats(o.gd, charID, level, w, upgrade)
	return l
}

// character picks a playable character (one whose weapon is in the game data) no other bot
// of the match has, any one when all are taken; the default character without any.
func (o *botOutfitter) character() string {
	var all, free []string
	for id, c := range o.gd.Data.Characters {
		if _, ok := o.gd.Data.Weapons[c.WeaponID]; ok {
			all = append(all, id)
		}
	}
	sort.Strings(all)
	for _, id := range all {
		if !o.used[id] {
			free = append(free, id)
		}
	}
	if len(free) == 0 {
		free = all
	}
	if len(free) == 0 {
		return o.gd.Rules.Player.DefaultCharacterID
	}
	id := free[o.intn(len(free))]
	o.used[id] = true
	return id
}

// maxLevel is the top of the character_levels curve, 0 (no cap) without one.
func (o *botOutfitter) maxLevel() int {
	top := 0
	for _, e := range character.LoadLevelsConfig(o.gd.Settings[character.SettingsKeyLevels]).Levels {
		top = max(top, e.Level)
	}
	return top
}

// level is avgLevel ± up to rules.bots levelSpread, between 1 and the max level.
func (o *botOutfitter) level(avgLevel int) int {
	spread := max(o.gd.Rules.Bots.LevelSpread, 0)
	level := max(avgLevel, 1) + o.intn(2*spread+1) - spread
	if top := o.maxLevel(); top > 0 {
		level = min(level, top)
	}
	return max(level, 1)
}

// weapon is the gun a player of level would normally carry on charID: the normal weapon of
// that character (or a generic one) with the highest required level at or below level, or the
// crafted weapon likewise, one of the two at random. false: no weapon is reachable at that
// level (below the first tier), the bot keeps its basic gun like a new player.
func (o *botOutfitter) weapon(charID string, level int) (botWeaponOption, bool) {
	var normal, crafted *botWeaponOption
	better := func(cur *botWeaponOption, w botWeaponOption) bool {
		return cur == nil || w.RequiredLevel > cur.RequiredLevel
	}
	for i := range o.weapons {
		w := o.weapons[i]
		if w.RequiredLevel > level || (w.CharacterID != "" && w.CharacterID != charID) {
			continue
		}
		if w.Category == "crafted" {
			if better(crafted, w) {
				crafted = &o.weapons[i]
			}
		} else if better(normal, w) || (normal.CharacterID == "" && w.CharacterID != "" && w.RequiredLevel == normal.RequiredLevel) {
			normal = &o.weapons[i] // the character's own gun before a generic one of the same tier
		}
	}
	var options []botWeaponOption
	for _, w := range []*botWeaponOption{normal, crafted} {
		if w != nil {
			options = append(options, *w)
		}
	}
	if len(options) == 0 {
		return botWeaponOption{}, false
	}
	return options[o.intn(len(options))], true
}

// upgrade is a random upgrade level from rules.bots weaponUpgradeMin to the gun's required
// level × weaponUpgradePerLevel, never above rules.equipment maxUpgradeLevel.
func (o *botOutfitter) upgrade(w botWeaponOption) int {
	r := o.gd.Rules
	top := r.Equipment.MaxUpgradeLevel
	lo := min(max(r.Bots.WeaponUpgradeMin, 0), top)
	hi := min(int(math.Floor(float64(w.RequiredLevel)*r.Bots.WeaponUpgradePerLevel)), top)
	if hi <= lo {
		return lo
	}
	return lo + o.intn(hi-lo+1)
}

// items are rules.bots defaultItems, then randomItemCount distinct items drawn from
// randomItems (every item the bot can use when empty). Only items in the game data that the
// bot knows how to use (match.BotUsableItems) count, within the rules a player's selection
// follows: at most rules.items maxLoadout, each at most its maxUsePerMatch.
func (o *botOutfitter) items() []string {
	rules := o.gd.Rules
	usable := map[string]bool{}
	for _, id := range match.BotUsableItems {
		usable[id] = true
	}
	items := []string{}
	counts := map[string]int{}
	add := func(id string) {
		it, ok := o.gd.Data.Items[id]
		if !ok || !usable[id] || len(items) >= rules.Items.MaxLoadout || counts[id] >= max(it.MaxUsePerMatch, 1) {
			return
		}
		items = append(items, id)
		counts[id]++
	}
	for _, id := range rules.Bots.DefaultItems {
		add(id)
	}
	pool := rules.Bots.RandomItems
	if len(pool) == 0 {
		pool = match.BotUsableItems
	}
	var draw []string
	for _, id := range pool {
		if _, ok := o.gd.Data.Items[id]; ok && usable[id] && counts[id] == 0 && !contains(draw, id) {
			draw = append(draw, id)
		}
	}
	for n := 0; n < rules.Bots.RandomItemCount && len(draw) > 0; n++ {
		k := o.intn(len(draw))
		add(draw[k])
		draw = append(draw[:k], draw[k+1:]...)
	}
	return items
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// botStats are the battle stats of a player of level on charID wearing only weapon (nil:
// none) at upgrade, with the stat points of that level (character_progression pointsPerLevel
// per level above 1) spread evenly: battlestats.Compute, as getActualStats.
func botStats(gd *gamedata.Snapshot, charID string, level int, weapon *botWeaponOption, upgrade int) battleStats {
	cfg := character.LoadProgressionConfig(gd.Settings[character.SettingsKeyProgression])
	pts := evenPoints(max(level-1, 0) * cfg.PointsPerLevel)
	var eq battlestats.EquipmentPoints
	if weapon != nil {
		eq = battlestats.SumEquipment([]battlestats.EquippedItem{{
			ItemID: weapon.ItemID, Slot: "weapon", HP: weapon.HP, Damage: weapon.Damage, Defense: weapon.Defense,
			Crit: weapon.Crit, MoveEnergy: weapon.MoveEnergy, UpgradeLevel: upgrade, Category: weapon.Category, Tier: weapon.Tier,
		}}, nil, battlestats.MilestonesFromSettings(gd.Settings))
	}
	return battlestats.Compute(charID, pts, cfg.Multipliers(), eq).Total
}

// evenPoints spreads total stat points over the six stats, the remainder to the first ones
// (HP, damage, defense, mobility, skill power, terrain damage).
func evenPoints(total int) statPoints {
	n := [6]int{}
	for i := range n {
		n[i] = total / 6
		if i < total%6 {
			n[i]++
		}
	}
	return statPoints{HP: n[0], Damage: n[1], Defense: n[2], Mobility: n[3], SkillPower: n[4], TerrainDamage: n[5]}
}

// botPlayer is the room entry of a bot with loadout l.
func botPlayer(id, name string, teamID int, l botLoadout) RoomPlayer {
	return RoomPlayer{PlayerID: id, DisplayName: name, TeamID: teamID, CharacterID: l.CharacterID, Items: l.Items, IsReady: true}
}
