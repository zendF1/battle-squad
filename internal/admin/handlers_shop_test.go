package admin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShopEditOfferTypeSelect(t *testing.T) {
	s := &Server{}
	for offerType, want := range map[string]string{
		"coin_pack": `<option value="coin_pack" selected>coin_pack</option>`,
		"legacy":    `<option value="legacy" selected>legacy (unsupported)</option>`,
	} {
		w := httptest.NewRecorder()
		s.render(w, "shop_edit", map[string]interface{}{
			"ActivePage":     "shop",
			"Offer":          ShopOffer{OfferID: "o1", OfferType: offerType},
			"OfferTypes":     shopOfferTypes,
			"KnownOfferType": isShopOfferType(offerType),
		})
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, want) {
			t.Fatalf("%s: status %d, body missing %q", offerType, w.Code, want)
		}
	}
}
