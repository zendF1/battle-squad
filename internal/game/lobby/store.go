package lobby

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"

	"github.com/jackc/pgx/v5"
)

// memberStore reads what the lobby shows of a player: the DB (dbStore); tests stub it.
type memberStore interface {
	// Member is a player as they join a lobby: name, level, saved loadout, rating, gun.
	// Ready is false.
	Member(ctx context.Context, playerID string) LobbyMember
	// Look is the player's level and the gun characterID holds (UpdateLoadout).
	Look(ctx context.Context, playerID, characterID string) (int, MemberGun)
	// Rating is the player's rating in the active season with its tier and division.
	Rating(ctx context.Context, playerID string) (int, string, int)
	DisplayName(ctx context.Context, playerID string) string
	// Players are the invite list cards of the ids that have a profile (Status unset),
	// with their ratings, in one query.
	Players(ctx context.Context, playerIDs []string) (map[string]playerCard, error)
}

// playerCard is an OnlinePlayer with the rating the invite list sorts by.
type playerCard struct {
	OnlinePlayer
	Rating int
}

type dbStore struct {
	db *database.PostgresDB
}

func (s dbStore) Member(ctx context.Context, playerID string) LobbyMember {
	characterID, items := loadPlayerLoadout(ctx, s.db, playerID)
	rating, tier, division := loadPlayerRating(ctx, s.db, playerID)
	name, level := s.profile(ctx, playerID)
	return LobbyMember{
		PlayerID:    playerID,
		DisplayName: name,
		CharacterID: characterID,
		Items:       items,
		Rating:      rating,
		Tier:        tier,
		Division:    division,
		Level:       level,
		Gun:         loadMemberGun(ctx, s.db, playerID, characterID),
	}
}

func (s dbStore) Look(ctx context.Context, playerID, characterID string) (int, MemberGun) {
	_, level := s.profile(ctx, playerID)
	return level, loadMemberGun(ctx, s.db, playerID, characterID)
}

func (s dbStore) Rating(ctx context.Context, playerID string) (int, string, int) {
	return loadPlayerRating(ctx, s.db, playerID)
}

func (s dbStore) DisplayName(ctx context.Context, playerID string) string {
	name, _ := s.profile(ctx, playerID)
	return name
}

// profile is the player's display name ("Player_<id prefix>" without a profile) and level.
func (s dbStore) profile(ctx context.Context, playerID string) (string, int) {
	var name string
	level := 1
	err := s.db.Pool.QueryRow(ctx,
		`SELECT display_name, level FROM player_profiles WHERE player_id = $1`, playerID).
		Scan(&name, &level)
	if err != nil {
		return "Player_" + playerID[:min(6, len(playerID))], 1
	}
	return name, level
}

// Players reads the names, levels, saved characters and active-season ratings of the
// ids; a player without a rank row has the elo config's default rating.
func (s dbStore) Players(ctx context.Context, playerIDs []string) (map[string]playerCard, error) {
	out := make(map[string]playerCard, len(playerIDs))
	if len(playerIDs) == 0 {
		return out, nil
	}
	elo, _ := ranked.LoadEloConfig(ctx, s.db.Pool)
	rows, err := s.db.Pool.Query(ctx, `
		SELECT p.player_id, p.display_name, p.level, COALESCE(l.character_id, ''), r.rating
		FROM player_profiles p
		LEFT JOIN player_loadouts l ON l.player_id = p.player_id
		LEFT JOIN LATERAL (
			SELECT pr.rating
			FROM player_ranks pr
			JOIN rank_seasons rs ON rs.season_id = pr.season_id
			WHERE pr.player_id = p.player_id AND rs.status = 'active'
			ORDER BY rs.starts_at DESC
			LIMIT 1) r ON TRUE
		WHERE p.player_id = ANY($1)`, playerIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c playerCard
		var rating *int
		if err := rows.Scan(&c.PlayerID, &c.DisplayName, &c.Level, &c.CharacterID, &rating); err != nil {
			return nil, err
		}
		if c.CharacterID == "" {
			c.CharacterID = "rookie"
		}
		c.Rating = elo.DefaultRating
		if rating != nil {
			c.Rating = *rating
		}
		c.Tier, c.Division = ranked.TierAndDivision(c.Rating)
		out[c.PlayerID] = c
	}
	return out, rows.Err()
}

// loadPlayerLoadout reads characterID and items from player_loadouts.
// Defaults to "rookie" / empty slice when no row is found.
func loadPlayerLoadout(ctx context.Context, db *database.PostgresDB, playerID string) (string, []string) {
	const q = `SELECT character_id, items FROM player_loadouts WHERE player_id = $1`

	var characterID string
	var itemsRaw []byte

	row := db.Pool.QueryRow(ctx, q, playerID)
	if err := row.Scan(&characterID, &itemsRaw); err != nil {
		return "rookie", []string{}
	}

	var items []string
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		items = []string{}
	}
	return characterID, items
}

// loadPlayerRating reads the player's rating in the active season with its
// tier and division. A player without a row has the elo config's default
// rating (1000 = silver 3).
func loadPlayerRating(ctx context.Context, db *database.PostgresDB, playerID string) (int, string, int) {
	const q = `
		SELECT pr.rating
		FROM player_ranks pr
		JOIN rank_seasons rs ON rs.season_id = pr.season_id
		WHERE pr.player_id = $1 AND rs.status = 'active'
		ORDER BY rs.starts_at DESC
		LIMIT 1`

	var rating int
	if err := db.Pool.QueryRow(ctx, q, playerID).Scan(&rating); err != nil {
		elo, _ := ranked.LoadEloConfig(ctx, db.Pool)
		rating = elo.DefaultRating
	}
	tier, division := ranked.TierAndDivision(rating)
	return rating, tier, division
}

// ---------------------------------------------------------------------------
// Gun
// ---------------------------------------------------------------------------

// gunTierBasic is the tier of a character's basic gun (no weapon worn), the client's
// Art.GUN_BASIC.
const gunTierBasic = "basic"

// wornWeapon is the weapon a character wears: its catalog item (Name "" when the
// catalog does not know it).
type wornWeapon struct {
	ItemID, Name, Category, Tier, CharacterID string
}

// loadMemberGun is the gun characterID holds: the weapon worn on it (gunOf), its
// basic gun when none.
func loadMemberGun(ctx context.Context, db *database.PostgresDB, playerID, characterID string) MemberGun {
	var w wornWeapon
	var name, category, tier, owner *string
	err := db.Pool.QueryRow(ctx, `
		SELECT pe.item_id, cei.name, cei.category, cei.tier, cei.character_id
		FROM player_equipment pe
		LEFT JOIN config_equipment_items cei ON cei.item_id = pe.item_id
		WHERE pe.player_id = $1 AND pe.equipped_on = $2 AND pe.is_equipped = TRUE AND pe.slot = 'weapon'
		LIMIT 1`, playerID, characterID).Scan(&w.ItemID, &name, &category, &tier, &owner)
	snap := gamedata.Current()
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			observability.Log.Warn().Err(err).Str("player_id", playerID).Msg("failed to load the worn weapon")
		}
		return gunOf(snap, characterID, nil)
	}
	if name == nil {
		w = weaponFromID(snap, w.ItemID)
	} else {
		w.Name, w.Category = *name, deref(category)
		w.Tier, w.CharacterID = deref(tier), deref(owner)
	}
	return gunOf(snap, characterID, &w)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// gunOf is the gun characterID holds, as the client's EquipmentModel.gun computes it:
//   - no weapon: the character's gun kind, tier "basic";
//   - a weapon: the gun kind of the character it is exclusive to, else of its name's type
//     word ("Iron Bazooka" → bazooka), the blaster when that is no known gun kind;
//     its tier: the item's, else its name's first word ("Iron" → iron), else the tier
//     of its id's suffix (_lv10 → iron), else the first tier of its category.
func gunOf(snap *gamedata.Snapshot, characterID string, w *wornWeapon) MemberGun {
	known := func(kind string) bool {
		for _, g := range snap.GunKinds {
			if g.ID == kind && kind != "" {
				return true
			}
		}
		return false
	}
	charGun := func(id string) string {
		if c, ok := snap.Data.Characters[id]; ok {
			return c.GunKind
		}
		return ""
	}
	if w == nil {
		kind := charGun(characterID)
		if !known(kind) && snap.Rules != nil {
			kind = charGun(snap.Rules.Player.DefaultCharacterID)
		}
		if !known(kind) {
			kind = "blaster"
		}
		return MemberGun{Kind: kind, Tier: gunTierBasic}
	}

	words := strings.Fields(strings.ToLower(w.Name))
	kind := ""
	if _, ok := snap.Data.Characters[w.CharacterID]; ok {
		kind = charGun(w.CharacterID)
	} else if len(words) >= 2 {
		typeWord := strings.Join(words[1:], " ")
		for _, tw := range snap.TypeWords {
			if tw.Word == typeWord {
				kind = tw.GunKind
			}
		}
	}
	if !known(kind) {
		kind = "blaster"
	}
	return MemberGun{Kind: kind, Tier: weaponTier(snap, w, words)}
}

func weaponTier(snap *gamedata.Snapshot, w *wornWeapon, nameWords []string) string {
	isTier := func(t string) bool {
		for _, et := range snap.EquipTiers {
			if et.Tier == t {
				return true
			}
		}
		return false
	}
	if t := strings.ToLower(w.Tier); isTier(t) {
		return t
	}
	if len(nameWords) > 0 && isTier(nameWords[0]) {
		return nameWords[0]
	}
	for _, et := range snap.EquipTiers {
		if et.IDSuffix != "" && strings.HasSuffix(w.ItemID, "_"+et.IDSuffix) {
			return et.Tier
		}
	}
	kind := "normal"
	if w.Category == "crafted" {
		kind = "crafted"
	}
	for _, et := range snap.EquipTiers {
		if et.Kind == kind {
			return et.Tier
		}
	}
	return ""
}

// weaponFromID is a stand-in catalog item for a weapon id the catalog does not know
// (the client's EquipmentModel.weapon_from_id): crafted_weapon_<tier> is crafted in that
// tier, <character>_weapon_lvN is that character's.
func weaponFromID(snap *gamedata.Snapshot, itemID string) wornWeapon {
	w := wornWeapon{ItemID: itemID, Category: "normal"}
	if strings.HasPrefix(itemID, "crafted_") {
		w.Category = "crafted"
		w.Tier = itemID[strings.LastIndex(itemID, "_")+1:]
		return w
	}
	owner, _, _ := strings.Cut(itemID, "_")
	if _, ok := snap.Data.Characters[owner]; ok && strings.HasPrefix(itemID, owner+"_weapon_") {
		w.CharacterID = owner
	}
	return w
}
