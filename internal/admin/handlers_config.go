package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"battle-squad/internal/shared/observability"
)

// FieldDef describes a form field for the config edit template.
type FieldDef struct {
	Name        string
	Label       string
	Type        string // "text", "number", "textarea"
	Step        string // for number inputs
	Description string
	Value       interface{}
}

// handleConfigList returns an http.HandlerFunc that lists all items of the given config type.
func (s *Server) handleConfigList(configType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var items interface{}
		var err error
		var title string

		switch configType {
		case "characters":
			title = "Characters"
			items, err = s.repo.GetCharacters(ctx)
		case "weapons":
			title = "Weapons"
			items, err = s.repo.GetWeapons(ctx)
		case "skills":
			title = "Skills"
			items, err = s.repo.GetSkills(ctx)
		case "items":
			title = "Items"
			items, err = s.repo.GetItems(ctx)
		case "maps":
			title = "Maps"
			items, err = s.repo.GetMaps(ctx)
		}

		if err != nil {
			observability.Log.Error().Err(err).Str("type", configType).Msg("failed to list config")
		}

		flash := r.URL.Query().Get("flash")
		errMsg := r.URL.Query().Get("error")

		s.render(w, "config_list", map[string]interface{}{
			"ActivePage": configType,
			"ConfigType": configType,
			"Title":      title,
			"Items":      items,
			"Flash":      flash,
			"Error":      errMsg,
		})
	}
}

// handleConfigEdit returns an http.HandlerFunc that shows the edit form for the given config type.
func (s *Server) handleConfigEdit(configType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.URL.Query().Get("id")
		isNew := id == ""

		var fields []FieldDef
		var title string
		var originalID string

		switch configType {
		case "characters":
			title = "Character"
			var c ConfigCharacter
			if !isNew {
				found, err := s.repo.GetCharacter(ctx, id)
				if err != nil {
					http.Redirect(w, r, "/characters?error=Character+not+found", http.StatusSeeOther)
					return
				}
				c = *found
				originalID = c.CharacterID
			}
			fields = []FieldDef{
				{Name: "character_id", Label: "Character ID", Type: "text", Description: "Unique identifier (e.g. char_tank_01)", Value: c.CharacterID},
				{Name: "name", Label: "Name", Type: "text", Description: "Display name shown in-game", Value: c.Name},
				{Name: "role", Label: "Role", Type: "text", Description: "Character role (tank, dps, support, etc.)", Value: c.Role},
				{Name: "hp", Label: "HP", Type: "number", Step: "1", Description: "Base hit points", Value: c.HP},
				{Name: "damage", Label: "Damage", Type: "number", Step: "1", Description: "Base damage stat", Value: c.Damage},
				{Name: "mobility", Label: "Mobility", Type: "number", Step: "1", Description: "Movement energy per turn", Value: c.Mobility},
				{Name: "defense", Label: "Defense", Type: "number", Step: "1", Description: "Damage reduction stat", Value: c.Defense},
				{Name: "skill_power", Label: "Skill Power", Type: "number", Step: "1", Description: "Multiplier for skill effects", Value: c.SkillPower},
				{Name: "terrain_damage", Label: "Terrain Damage", Type: "number", Step: "1", Description: "Bonus terrain destruction", Value: c.TerrainDamage},
				{Name: "difficulty", Label: "Difficulty", Type: "number", Step: "1", Description: "Difficulty rating (1-5)", Value: c.Difficulty},
				{Name: "weapon_id", Label: "Weapon ID", Type: "text", Description: "ID of the default weapon", Value: c.WeaponID},
				{Name: "skill_id", Label: "Skill ID", Type: "text", Description: "ID of the character's skill", Value: c.SkillID},
				{Name: "description", Label: "Description", Type: "textarea", Description: "Character description", Value: c.Description},
			}

		case "weapons":
			title = "Weapon"
			var wp ConfigWeapon
			if !isNew {
				found, err := s.repo.GetWeapon(ctx, id)
				if err != nil {
					http.Redirect(w, r, "/weapons?error=Weapon+not+found", http.StatusSeeOther)
					return
				}
				wp = *found
				originalID = wp.WeaponID
			}
			fields = []FieldDef{
				{Name: "weapon_id", Label: "Weapon ID", Type: "text", Description: "Unique identifier (e.g. wpn_cannon)", Value: wp.WeaponID},
				{Name: "name", Label: "Name", Type: "text", Description: "Display name", Value: wp.Name},
				{Name: "damage", Label: "Damage", Type: "number", Step: "1", Description: "Base damage dealt on hit", Value: wp.Damage},
				{Name: "explosion_radius", Label: "Explosion Radius", Type: "number", Step: "1", Description: "Radius of explosion in pixels", Value: wp.ExplosionRadius},
				{Name: "terrain_damage", Label: "Terrain Damage", Type: "number", Step: "1", Description: "Terrain destruction radius", Value: wp.TerrainDamage},
				{Name: "projectile_weight", Label: "Projectile Weight", Type: "number", Step: "0.1", Description: "Affects gravity pull on projectile (higher = drops faster)", Value: wp.ProjectileWeight},
				{Name: "wind_influence", Label: "Wind Influence", Type: "number", Step: "0.1", Description: "How much wind affects the projectile (0=none, 1=full)", Value: wp.WindInfluence},
				{Name: "multi_hit", Label: "Multi Hit", Type: "number", Step: "1", Description: "Number of projectiles fired (1=single shot)", Value: wp.MultiHit},
				{Name: "description", Label: "Description", Type: "textarea", Description: "Weapon description", Value: wp.Description},
			}

		case "skills":
			title = "Skill"
			var sk ConfigSkill
			if !isNew {
				found, err := s.repo.GetSkill(ctx, id)
				if err != nil {
					http.Redirect(w, r, "/skills?error=Skill+not+found", http.StatusSeeOther)
					return
				}
				sk = *found
				originalID = sk.SkillID
			}
			fields = []FieldDef{
				{Name: "skill_id", Label: "Skill ID", Type: "text", Description: "Unique identifier (e.g. skill_barrage)", Value: sk.SkillID},
				{Name: "character_id", Label: "Character ID", Type: "text", Description: "Which character owns this skill", Value: sk.CharacterID},
				{Name: "name", Label: "Name", Type: "text", Description: "Display name", Value: sk.Name},
				{Name: "cooldown_turn", Label: "Cooldown (turns)", Type: "number", Step: "1", Description: "Number of turns before skill can be used again", Value: sk.CooldownTurn},
				{Name: "effect_type", Label: "Effect Type", Type: "text", Description: "Type of effect (damage, heal, buff, debuff, etc.)", Value: sk.EffectType},
				{Name: "projectile_count", Label: "Projectile Count", Type: "number", Step: "1", Description: "Number of projectiles fired by skill", Value: sk.ProjectileCount},
				{Name: "status_effect_id", Label: "Status Effect ID", Type: "text", Description: "ID of status effect applied (if any)", Value: sk.StatusEffectID},
				{Name: "damage_multiplier", Label: "Damage Multiplier", Type: "number", Step: "0.1", Description: "Multiplier applied to base damage", Value: sk.DamageMultiplier},
				{Name: "description", Label: "Description", Type: "textarea", Description: "Skill description", Value: sk.Description},
			}

		case "items":
			title = "Item"
			var it ConfigItem
			if !isNew {
				found, err := s.repo.GetItem(ctx, id)
				if err != nil {
					http.Redirect(w, r, "/items?error=Item+not+found", http.StatusSeeOther)
					return
				}
				it = *found
				originalID = it.ItemID
			}
			fields = []FieldDef{
				{Name: "item_id", Label: "Item ID", Type: "text", Description: "Unique identifier (e.g. item_heal_small)", Value: it.ItemID},
				{Name: "name", Label: "Name", Type: "text", Description: "Display name", Value: it.Name},
				{Name: "type", Label: "Type", Type: "text", Description: "Item type (heal, damage, buff, teleport, etc.)", Value: it.Type},
				{Name: "target_type", Label: "Target Type", Type: "text", Description: "Who/what this item targets (self, enemy, terrain)", Value: it.TargetType},
				{Name: "value", Label: "Value", Type: "number", Step: "0.1", Description: "Effect value (heal amount, damage amount, etc.)", Value: it.Value},
				{Name: "max_use_per_match", Label: "Max Use Per Match", Type: "number", Step: "1", Description: "Maximum times this item can be used in a single match", Value: it.MaxUsePerMatch},
				{Name: "cooldown", Label: "Cooldown (turns)", Type: "number", Step: "1", Description: "Turns to wait before item can be used again", Value: it.Cooldown},
				{Name: "description", Label: "Description", Type: "textarea", Description: "Item description", Value: it.Description},
			}

		}

		s.render(w, "config_edit", map[string]interface{}{
			"ActivePage": configType,
			"ConfigType": configType,
			"Title":      title,
			"Fields":     fields,
			"IsNew":      isNew,
			"OriginalID": originalID,
		})
	}
}

// handleConfigSave returns an http.HandlerFunc that saves (creates or updates) a config item.
func (s *Server) handleConfigSave(configType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/"+configType+"?error=Invalid+form+data", http.StatusSeeOther)
			return
		}
		ctx := r.Context()

		switch configType {
		case "characters":
			c := &ConfigCharacter{
				CharacterID:   strings.TrimSpace(r.FormValue("character_id")),
				Name:          r.FormValue("name"),
				Role:          r.FormValue("role"),
				HP:            formInt(r, "hp"),
				Damage:        formInt(r, "damage"),
				Mobility:      formInt(r, "mobility"),
				Defense:       formInt(r, "defense"),
				SkillPower:    formInt(r, "skill_power"),
				TerrainDamage: formInt(r, "terrain_damage"),
				Difficulty:    formInt(r, "difficulty"),
				WeaponID:      r.FormValue("weapon_id"),
				SkillID:       r.FormValue("skill_id"),
				Description:   r.FormValue("description"),
			}
			if c.CharacterID == "" {
				http.Redirect(w, r, "/characters/edit?error=Character+ID+is+required", http.StatusSeeOther)
				return
			}
			if err := s.repo.UpsertCharacter(ctx, c); err != nil {
				observability.Log.Error().Err(err).Msg("failed to upsert character")
				http.Redirect(w, r, "/characters?error=Failed+to+save+character", http.StatusSeeOther)
				return
			}

		case "weapons":
			wp := &ConfigWeapon{
				WeaponID:         strings.TrimSpace(r.FormValue("weapon_id")),
				Name:             r.FormValue("name"),
				Damage:           formInt(r, "damage"),
				ExplosionRadius:  formInt(r, "explosion_radius"),
				TerrainDamage:    formInt(r, "terrain_damage"),
				ProjectileWeight: formFloat(r, "projectile_weight"),
				WindInfluence:    formFloat(r, "wind_influence"),
				MultiHit:         formInt(r, "multi_hit"),
				Description:      r.FormValue("description"),
			}
			if wp.WeaponID == "" {
				http.Redirect(w, r, "/weapons/edit?error=Weapon+ID+is+required", http.StatusSeeOther)
				return
			}
			if err := s.repo.UpsertWeapon(ctx, wp); err != nil {
				observability.Log.Error().Err(err).Msg("failed to upsert weapon")
				http.Redirect(w, r, "/weapons?error=Failed+to+save+weapon", http.StatusSeeOther)
				return
			}

		case "skills":
			sk := &ConfigSkill{
				SkillID:          strings.TrimSpace(r.FormValue("skill_id")),
				CharacterID:      r.FormValue("character_id"),
				Name:             r.FormValue("name"),
				CooldownTurn:     formInt(r, "cooldown_turn"),
				EffectType:       r.FormValue("effect_type"),
				ProjectileCount:  formInt(r, "projectile_count"),
				StatusEffectID:   r.FormValue("status_effect_id"),
				DamageMultiplier: formFloat(r, "damage_multiplier"),
				Description:      r.FormValue("description"),
			}
			if sk.SkillID == "" {
				http.Redirect(w, r, "/skills/edit?error=Skill+ID+is+required", http.StatusSeeOther)
				return
			}
			if err := s.repo.UpsertSkill(ctx, sk); err != nil {
				observability.Log.Error().Err(err).Msg("failed to upsert skill")
				http.Redirect(w, r, "/skills?error=Failed+to+save+skill", http.StatusSeeOther)
				return
			}

		case "items":
			it := &ConfigItem{
				ItemID:         strings.TrimSpace(r.FormValue("item_id")),
				Name:           r.FormValue("name"),
				Type:           r.FormValue("type"),
				TargetType:     r.FormValue("target_type"),
				Value:          formFloat(r, "value"),
				MaxUsePerMatch: formInt(r, "max_use_per_match"),
				Cooldown:       formInt(r, "cooldown"),
				Description:    r.FormValue("description"),
			}
			if it.ItemID == "" {
				http.Redirect(w, r, "/items/edit?error=Item+ID+is+required", http.StatusSeeOther)
				return
			}
			if err := s.repo.UpsertItem(ctx, it); err != nil {
				observability.Log.Error().Err(err).Msg("failed to upsert item")
				http.Redirect(w, r, "/items?error=Failed+to+save+item", http.StatusSeeOther)
				return
			}

		}

		http.Redirect(w, r, "/"+configType+"?flash=Saved+successfully", http.StatusSeeOther)
	}
}

// handleConfigDelete returns an http.HandlerFunc that deletes a config item.
func (s *Server) handleConfigDelete(configType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/"+configType+"?error=Invalid+form+data", http.StatusSeeOther)
			return
		}
		ctx := r.Context()
		id := r.FormValue("id")
		if id == "" {
			http.Redirect(w, r, "/"+configType+"?error=Missing+ID", http.StatusSeeOther)
			return
		}

		var err error
		switch configType {
		case "characters":
			err = s.repo.DeleteCharacter(ctx, id)
		case "weapons":
			err = s.repo.DeleteWeapon(ctx, id)
		case "skills":
			err = s.repo.DeleteSkill(ctx, id)
		case "items":
			err = s.repo.DeleteItem(ctx, id)
		case "maps":
			err = s.repo.DeleteMap(ctx, id)
		}

		if err != nil {
			observability.Log.Error().Err(err).Str("type", configType).Str("id", id).Msg("failed to delete config")
			http.Redirect(w, r, "/"+configType+"?error=Failed+to+delete", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/"+configType+"?flash=Deleted+successfully", http.StatusSeeOther)
	}
}

// handleCharacterDetail shows a combined character + skill edit form.
func (s *Server) handleCharacterDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.URL.Query().Get("id")
		isNew := id == ""

		var char ConfigCharacter
		var skill ConfigSkill

		if !isNew {
			found, err := s.repo.GetCharacter(ctx, id)
			if err != nil {
				http.Redirect(w, r, "/characters?error=Character+not+found", http.StatusSeeOther)
				return
			}
			char = *found

			// Load associated skill
			sk, err := s.repo.GetSkillByCharacterID(ctx, id)
			if err == nil {
				skill = *sk
			}
		}

		flash := r.URL.Query().Get("flash")
		errMsg := r.URL.Query().Get("error")

		s.render(w, "character_detail", map[string]interface{}{
			"ActivePage": "characters",
			"Character":  char,
			"Skill":      skill,
			"IsNew":      isNew,
			"Flash":      flash,
			"Error":      errMsg,
		})
	}
}

// handleCharacterDetailSave saves both character and skill config together.
func (s *Server) handleCharacterDetailSave() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/characters?error=Invalid+form+data", http.StatusSeeOther)
			return
		}
		ctx := r.Context()

		charID := strings.TrimSpace(r.FormValue("character_id"))
		if charID == "" {
			http.Redirect(w, r, "/characters/detail?error=Character+ID+is+required", http.StatusSeeOther)
			return
		}

		skillID := strings.TrimSpace(r.FormValue("skill_id"))

		// Save character (link skill_id)
		c := &ConfigCharacter{
			CharacterID:   charID,
			Name:          r.FormValue("name"),
			Role:          r.FormValue("role"),
			HP:            formInt(r, "hp"),
			Damage:        formInt(r, "damage"),
			Mobility:      formInt(r, "mobility"),
			Defense:       formInt(r, "defense"),
			SkillPower:    formInt(r, "skill_power"),
			TerrainDamage: formInt(r, "terrain_damage"),
			Difficulty:    formInt(r, "difficulty"),
			WeaponID:      r.FormValue("weapon_id"),
			SkillID:       skillID,
			Description:   r.FormValue("char_description"),
		}
		if err := s.repo.UpsertCharacter(ctx, c); err != nil {
			observability.Log.Error().Err(err).Msg("failed to upsert character")
			http.Redirect(w, r, "/characters?error=Failed+to+save+character", http.StatusSeeOther)
			return
		}

		// Save skill if skill_id is provided
		if skillID != "" {
			sk := &ConfigSkill{
				SkillID:          skillID,
				CharacterID:      charID,
				Name:             r.FormValue("skill_name"),
				CooldownTurn:     0, // no longer used (skill energy system)
				EffectType:       r.FormValue("effect_type"),
				ProjectileCount:  formInt(r, "projectile_count"),
				StatusEffectID:   r.FormValue("status_effect_id"),
				DamageMultiplier: formFloat(r, "damage_multiplier"),
				Description:      r.FormValue("skill_description"),
			}
			if err := s.repo.UpsertSkill(ctx, sk); err != nil {
				observability.Log.Error().Err(err).Msg("failed to upsert skill")
				http.Redirect(w, r, "/characters/detail?id="+charID+"&error=Failed+to+save+skill", http.StatusSeeOther)
				return
			}
		}

		http.Redirect(w, r, "/characters/detail?id="+charID+"&flash=Saved+successfully", http.StatusSeeOther)
	}
}

// jsonString converts json.RawMessage to a string for template display.
func jsonString(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	// Pretty-print the JSON
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// formInt parses an integer from a form value, returning 0 on error.
func formInt(r *http.Request, key string) int {
	v := r.FormValue(key)
	if v == "" {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// formFloat parses a float64 from a form value, returning 0 on error.
func formFloat(r *http.Request, key string) float64 {
	v := r.FormValue(key)
	if v == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}
