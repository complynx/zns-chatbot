package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (c APIClient) BrowserAuthRecipient(ctx context.Context, username string) (browserauth.Recipient, error) {
	body, err := json.Marshal(map[string]string{"username": username})
	if err != nil {
		return browserauth.Recipient{}, err
	}
	var recipient browserauth.Recipient
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/browser-auth/recipient",
		body,
		&recipient,
	)
	return recipient, err
}

func (b *Bot) isBrowserAuth(u telegram.Update) bool {
	return b.BrowserAuth != nil && u.Callback != nil && strings.HasPrefix(u.Callback.Data, "ba|")
}

func (b *Bot) handleBrowserAuth(ctx context.Context, owner string, u telegram.Update) error {
	prefs, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	return b.BrowserAuth.Decide(ctx, *u.Callback, prefs.Language)
}
