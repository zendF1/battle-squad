package equipment

import (
	"encoding/json"
	"net/http"
	"strings"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func getPlayerID(r *http.Request) string {
	id, _ := r.Context().Value(observability.PlayerIDKey).(string)
	return id
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if appErr, ok := err.(model.AppError); ok {
		model.WriteError(w, r, appErr)
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "insufficient coin balance") || strings.Contains(msg, "insufficient gem balance") {
		model.WriteError(w, r, model.ErrInsufficientBalance)
		return
	}
	model.WriteError(w, r, model.ErrInternalServer)
}

// ---------------------------------------------------------------------------
// Inventory
// ---------------------------------------------------------------------------

func (h *Handler) GetEquipment(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	items, err := h.service.GetPlayerEquipment(r.Context(), playerID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []PlayerEquipment{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) GetStones(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	items, err := h.service.GetPlayerStones(r.Context(), playerID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []PlayerStone{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) GetGems(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	items, err := h.service.GetPlayerGems(r.Context(), playerID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []PlayerGem{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) GetMaterials(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	items, err := h.service.GetPlayerMaterials(r.Context(), playerID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []PlayerMaterial{}
	}
	writeJSON(w, http.StatusOK, items)
}

// ---------------------------------------------------------------------------
// Equip / Unequip
// ---------------------------------------------------------------------------

func (h *Handler) EquipItem(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req EquipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.EquipItem(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) UnequipItem(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req UnequipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.UnequipItem(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ---------------------------------------------------------------------------
// Upgrade / Dismantle
// ---------------------------------------------------------------------------

func (h *Handler) Upgrade(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req UpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	result, err := h.service.UpgradeEquipment(r.Context(), playerID, req)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Dismantle(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req DismantleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.DismantleEquipment(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ---------------------------------------------------------------------------
// Socket / Unsocket Gem
// ---------------------------------------------------------------------------

func (h *Handler) SocketGem(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req SocketGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.SocketGem(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) UnsocketGem(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req UnsocketGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.UnsocketGem(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ---------------------------------------------------------------------------
// Shop — Equipment
// ---------------------------------------------------------------------------

func (h *Handler) GetShopEquipment(w http.ResponseWriter, r *http.Request) {
	characterID := r.URL.Query().Get("characterId")

	items, err := h.service.GetShopEquipment(r.Context(), characterID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []EquipmentItemConfig{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) BuyEquipment(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req BuyEquipmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	equipmentID, err := h.service.BuyEquipment(r.Context(), playerID, req)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"equipmentId": equipmentID})
}

// ---------------------------------------------------------------------------
// Shop — Stones
// ---------------------------------------------------------------------------

func (h *Handler) GetShopStones(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.GetShopStones(r.Context())
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []StoneConfig{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) BuyStones(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req BuyStoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.BuyStones(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ---------------------------------------------------------------------------
// Shop — Gems
// ---------------------------------------------------------------------------

func (h *Handler) GetShopGems(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.GetShopGems(r.Context())
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []GemConfig{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) BuyGems(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req BuyGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.BuyGems(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ---------------------------------------------------------------------------
// Shop — Materials
// ---------------------------------------------------------------------------

func (h *Handler) GetShopMaterials(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.GetShopMaterials(r.Context())
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []MaterialConfig{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) BuyMaterials(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req BuyMaterialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.BuyMaterials(r.Context(), playerID, req); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ---------------------------------------------------------------------------
// Merge
// ---------------------------------------------------------------------------

func (h *Handler) MergeStones(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req MergeStoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	result, err := h.service.MergeStones(r.Context(), playerID, req)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) MergeGems(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req MergeGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	result, err := h.service.MergeGems(r.Context(), playerID, req)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Crafting
// ---------------------------------------------------------------------------

func (h *Handler) GetRecipes(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.GetCraftingRecipes(r.Context())
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if items == nil {
		items = []CraftingRecipe{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) Craft(w http.ResponseWriter, r *http.Request) {
	playerID := getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req CraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	equipmentID, err := h.service.CraftEquipment(r.Context(), playerID, req)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"equipmentId": equipmentID})
}
