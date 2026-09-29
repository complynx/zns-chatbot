package appclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// ProvisionTelegram is called only by verified inbound adapters. Background
// recipients and ordinary AuthenticateTelegram lookups must never invoke it.
func (c Host) ProvisionTelegram(ctx context.Context, botID int64, user telegram.User) error {
	if user.ID <= 0 || user.ID >= 1<<52 || user.IsBot {
		return identity.ErrZitadelIdentity
	}
	body, err := json.Marshal(identity.TelegramProvisioningRequest{BotID: botID, User: user})
	if err != nil {
		return err
	}
	if len(body) > identity.MaxProvisioningBytes {
		return identity.ErrZitadelIdentity
	}
	var response struct {
		OK bool `json:"ok"`
	}
	err = c.requestToken(ctx, c.Signer.TelegramProvisioningToken(botID, body), http.MethodPost,
		"/internal/identity/telegram", body, &response)
	if err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); ok && problem.Status == http.StatusConflict &&
			problem.Code == "identity_conflict" {
			return ErrProvisioningDenied
		}
		return err
	}
	if !response.OK {
		return errors.New("identity provisioning unavailable")
	}
	return nil
}
