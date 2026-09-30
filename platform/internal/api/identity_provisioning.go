package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type TelegramProvisioner interface {
	EnsureTelegram(context.Context, identityprovision.Telegram) (identityprovision.Binding, error)
}

// WithTelegramProvisioning adds a service-only boundary outside user bearer auth.
// Bot processes never receive provider management credentials or core write access.
func WithTelegramProvisioning(
	next http.Handler,
	service TelegramProvisioner,
	signer identity.Signer,
	botID int64,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", next)
	mux.HandleFunc("/internal/identity/telegram", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			JSON(w, http.StatusMethodNotAllowed, map[string]string{codeField: "method_not_allowed"})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, identity.MaxProvisioningBytes))
		if err != nil {
			JSON(w, http.StatusRequestEntityTooLarge, map[string]string{codeField: invalidJSON})
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || signer.VerifyTelegramProvisioning(token, botID, body) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var input identity.TelegramProvisioningRequest
		if Decode(w, r, &input) != nil || input.BotID != botID || input.User.ID <= 0 || input.User.ID >= 1<<52 ||
			input.User.IsBot {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		_, err = service.EnsureTelegram(r.Context(), identityprovision.Telegram{ID: input.User.ID,
			FirstName: input.User.FirstName, LastName: input.User.LastName, Language: input.User.LanguageCode})
		if err != nil {
			markDatabaseFailure(w, err)
			status, code := http.StatusServiceUnavailable, "identity_provisioning_unavailable"
			if errors.Is(err, identityprovision.ErrConflict) {
				status, code = http.StatusConflict, "identity_conflict"
			}
			if errors.Is(err, identityprovision.ErrInvalid) {
				status, code = http.StatusBadRequest, invalidJSON
			}
			JSON(w, status, map[string]string{codeField: code})
			return
		}
		JSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	return mux
}
