package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type TelegramOnboarding func(context.Context, telegram.User) error

var ErrProvisioningDenied = errors.New("identity provisioning denied")

// ProvisionTelegram is called only by verified inbound adapters. Background
// recipients and ordinary AuthenticateTelegram lookups must never invoke it.
func (c APIClient) ProvisionTelegram(ctx context.Context, botID int64, user telegram.User) error {
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

func (b *Bot) denyOnboarding(ctx context.Context, in incoming, update telegram.Update) {
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	text, err := i18n.Translate(in.language, i18n.IdentityUnavailable, nil)
	if err == nil {
		_, _ = b.TG.Send(ctx, telegram.Send{ChatID: in.chat, Text: text})
	}
}

func (b *Bot) onboardIncoming(ctx context.Context, update telegram.Update) error {
	if update.Callback != nil {
		return b.Onboarding(ctx, update.Callback.From)
	}
	if update.Message != nil {
		return b.Onboarding(ctx, update.Message.From)
	}
	return identity.ErrZitadelIdentity
}
