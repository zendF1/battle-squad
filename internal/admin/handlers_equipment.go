package admin

import (
	"net/http"
	"strings"

	"battle-squad/internal/shared/observability"
)

// ---------------------------------------------------------------------------
// Equipment Items
// ---------------------------------------------------------------------------

func (s *Server) handleEquipmentItemsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.GetAllEquipmentItems(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get equipment items")
	}
	s.render(w, "equipment_items", map[string]interface{}{
		"ActivePage": "equipment-items",
		"Items":      items,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleEquipmentItemEdit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	isNew := id == ""

	item := map[string]interface{}{
		"ItemID":        "",
		"Name":          "",
		"Slot":          "",
		"Category":      "",
		"Tier":          "",
		"RequiredLevel": 1,
		"CharacterID":   "",
		"GemSlots":      1,
		"StatHP":        0,
		"StatDMG":       0,
		"StatDEF":       0,
		"StatCrit":      0.0,
		"StatMoveEnergy": 0,
		"PriceCoin":     0,
		"PriceGem":      0,
		"IsActive":      true,
	}

	if !isNew {
		items, err := s.repo.GetAllEquipmentItems(r.Context())
		if err != nil {
			http.Redirect(w, r, "/equipment-items?error=Failed+to+load+item", http.StatusSeeOther)
			return
		}
		found := false
		for _, it := range items {
			if it["ItemID"] == id {
				item = it
				found = true
				break
			}
		}
		if !found {
			http.Redirect(w, r, "/equipment-items?error=Item+not+found", http.StatusSeeOther)
			return
		}
	}

	s.render(w, "equipment_item_edit", map[string]interface{}{
		"ActivePage": "equipment-items",
		"Item":       item,
		"IsNew":      isNew,
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleEquipmentItemSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-items?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	itemID := strings.TrimSpace(r.FormValue("item_id"))
	if itemID == "" {
		http.Redirect(w, r, "/equipment-items/edit?error=Item+ID+is+required", http.StatusSeeOther)
		return
	}

	var tier *string
	if v := strings.TrimSpace(r.FormValue("tier")); v != "" {
		tier = &v
	}
	var characterID *string
	if v := strings.TrimSpace(r.FormValue("character_id")); v != "" {
		characterID = &v
	}

	err := s.repo.UpsertEquipmentItem(r.Context(),
		itemID,
		r.FormValue("name"),
		r.FormValue("slot"),
		r.FormValue("category"),
		tier,
		characterID,
		formInt(r, "required_level"),
		formInt(r, "gem_slots"),
		formInt(r, "stat_hp"),
		formInt(r, "stat_damage"),
		formInt(r, "stat_defense"),
		formInt(r, "stat_move_energy"),
		formInt(r, "price_coin"),
		formInt(r, "price_gem"),
		formFloat(r, "stat_crit"),
		r.FormValue("is_active") == "true",
	)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert equipment item")
		http.Redirect(w, r, "/equipment-items?error=Failed+to+save+item", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/equipment-items?flash=Item+saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleEquipmentItemDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-items?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	id := r.FormValue("id")
	if id == "" {
		http.Redirect(w, r, "/equipment-items?error=Missing+ID", http.StatusSeeOther)
		return
	}

	if err := s.repo.DeleteEquipmentItem(r.Context(), id); err != nil {
		observability.Log.Error().Err(err).Msg("failed to delete equipment item")
		http.Redirect(w, r, "/equipment-items?error=Failed+to+delete+item", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/equipment-items?flash=Item+deleted+successfully", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Upgrade Rates
// ---------------------------------------------------------------------------

func (s *Server) handleUpgradeRates(w http.ResponseWriter, r *http.Request) {
	rates, err := s.repo.GetAllUpgradeRates(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get upgrade rates")
	}
	s.render(w, "upgrade_rates", map[string]interface{}{
		"ActivePage": "upgrade-rates",
		"Rates":      rates,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleUpgradeRateSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/upgrade-rates?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	err := s.repo.UpsertUpgradeRate(r.Context(),
		formInt(r, "from_level"),
		formInt(r, "to_level"),
		formInt(r, "upgrade_cost"),
		formFloat(r, "max_percent"),
		formInt(r, "fail_reset_to"),
	)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert upgrade rate")
		http.Redirect(w, r, "/upgrade-rates?error=Failed+to+save+rate", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/upgrade-rates?flash=Rate+saved+successfully", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Stone Configs
// ---------------------------------------------------------------------------

func (s *Server) handleStoneConfigs(w http.ResponseWriter, r *http.Request) {
	stones, err := s.repo.GetAllStoneConfigs(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get stone configs")
	}
	s.render(w, "equipment_stones", map[string]interface{}{
		"ActivePage": "equipment-stones",
		"Stones":     stones,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleStoneConfigSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-stones?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	err := s.repo.UpsertStoneConfig(r.Context(),
		formInt(r, "stone_level"),
		formInt(r, "power"),
		formInt(r, "price_coin"),
		formInt(r, "price_gem"),
		r.FormValue("source"),
	)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert stone config")
		http.Redirect(w, r, "/equipment-stones?error=Failed+to+save+stone", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/equipment-stones?flash=Stone+saved+successfully", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Gem Configs
// ---------------------------------------------------------------------------

func (s *Server) handleGemConfigs(w http.ResponseWriter, r *http.Request) {
	gems, err := s.repo.GetAllGemConfigs(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get gem configs")
	}
	s.render(w, "equipment_gems", map[string]interface{}{
		"ActivePage": "equipment-gems",
		"Gems":       gems,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleGemConfigSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-gems?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	err := s.repo.UpsertGemConfig(r.Context(),
		r.FormValue("gem_type"),
		formInt(r, "gem_level"),
		formFloat(r, "stat_value"),
	)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert gem config")
		http.Redirect(w, r, "/equipment-gems?error=Failed+to+save+gem", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/equipment-gems?flash=Gem+saved+successfully", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Set Bonuses
// ---------------------------------------------------------------------------

func (s *Server) handleSetBonuses(w http.ResponseWriter, r *http.Request) {
	bonuses, err := s.repo.GetAllSetBonuses(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get set bonuses")
	}
	s.render(w, "set_bonuses", map[string]interface{}{
		"ActivePage": "set-bonuses",
		"Bonuses":    bonuses,
		"Flash":      r.URL.Query().Get("flash"),
		"Error":      r.URL.Query().Get("error"),
	})
}

func (s *Server) handleSetBonusSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/set-bonuses?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	err := s.repo.UpsertSetBonus(r.Context(),
		r.FormValue("tier"),
		formInt(r, "pieces_required"),
		formFloat(r, "bonus_hp_pct"),
		formFloat(r, "bonus_dmg_pct"),
		formFloat(r, "bonus_def_pct"),
		formFloat(r, "bonus_crit_pct"),
	)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert set bonus")
		http.Redirect(w, r, "/set-bonuses?error=Failed+to+save+bonus", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/set-bonuses?flash=Set+bonus+saved+successfully", http.StatusSeeOther)
}
