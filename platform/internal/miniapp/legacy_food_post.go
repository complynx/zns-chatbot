package miniapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Python clients send signed initData in the query and raw meal JSON as the
// body. The URL order/message/chat identifiers never choose the acting owner.
func (g Gateway) legacyFoodPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	initData := r.URL.Query().Get("initData")
	user, err := telegram.VerifyWebApp(initData, g.Token, time.Now())
	if err != nil {
		api.JSON(w, http.StatusUnauthorized, map[string]string{codeField: "invalid_telegram_identity"})
		return
	}
	if err = g.onboardVerified(r.Context(), user); err != nil {
		api.JSON(w, http.StatusServiceUnavailable, map[string]string{codeField: "identity_provisioning_unavailable"})
		return
	}
	ctx, owner, err := g.API.AuthenticateTelegram(r.Context(), user.ID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var meals legacyfood.MealSelection
	if api.Decode(w, r, &meals) != nil {
		api.JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidFoodJSON})
		return
	}
	view, err := g.API.FoodView(ctx, owner, r.URL.Query().Get("pass_key"), "")
	if err != nil {
		respond(w, nil, err)
		return
	}
	raw, err := json.Marshal(meals)
	if err != nil {
		respond(w, nil, err)
		return
	}
	sum := sha256.Sum256(append([]byte(initData+"\x00"+view.Event.ID+"\x00"), raw...))
	_, err = g.API.FoodLegacyMenu(ctx, owner, view.Event.ID, hex.EncodeToString(sum[:]), meals)
	respond(w, map[string]string{"message": "Order saved successfully"}, err)
}
