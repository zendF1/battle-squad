package admin

import (
	"net/http"
	"strconv"
	"strings"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handleShopList(w http.ResponseWriter, r *http.Request) {
	offers, err := s.repo.GetShopOffers(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get shop offers")
	}

	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")

	s.render(w, "shop", map[string]interface{}{
		"ActivePage": "shop",
		"Offers":     offers,
		"Flash":      flash,
		"Error":      errMsg,
	})
}

func (s *Server) handleShopEdit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	isNew := id == ""

	var offer ShopOffer
	if !isNew {
		found, err := s.repo.GetShopOffer(r.Context(), id)
		if err != nil {
			http.Redirect(w, r, "/shop?error=Offer+not+found", http.StatusSeeOther)
			return
		}
		offer = *found
	} else {
		offer.IsActive = true
	}

	s.render(w, "shop_edit", map[string]interface{}{
		"ActivePage": "shop",
		"Offer":      offer,
		"IsNew":      isNew,
	})
}

func (s *Server) handleShopSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/shop?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	offerID := strings.TrimSpace(r.FormValue("offer_id"))
	if offerID == "" {
		http.Redirect(w, r, "/shop/edit?error=Offer+ID+is+required", http.StatusSeeOther)
		return
	}

	var limitPerPlayer *int
	if v := r.FormValue("limit_per_player"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			limitPerPlayer = &n
		}
	}

	o := &ShopOffer{
		OfferID:        offerID,
		ItemID:         r.FormValue("item_id"),
		OfferType:      r.FormValue("offer_type"),
		PriceCurrency:  r.FormValue("price_currency"),
		PriceAmount:    formInt(r, "price_amount"),
		Quantity:       formInt(r, "quantity"),
		LimitPerPlayer: limitPerPlayer,
		IsActive:       r.FormValue("is_active") == "true",
	}

	if err := s.repo.UpsertShopOffer(r.Context(), o); err != nil {
		observability.Log.Error().Err(err).Msg("failed to upsert shop offer")
		http.Redirect(w, r, "/shop?error=Failed+to+save+offer", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/shop?flash=Offer+saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleShopDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/shop?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	id := r.FormValue("id")
	if id == "" {
		http.Redirect(w, r, "/shop?error=Missing+ID", http.StatusSeeOther)
		return
	}

	if err := s.repo.DeleteShopOffer(r.Context(), id); err != nil {
		observability.Log.Error().Err(err).Msg("failed to delete shop offer")
		http.Redirect(w, r, "/shop?error=Failed+to+delete+offer", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/shop?flash=Offer+deleted+successfully", http.StatusSeeOther)
}
