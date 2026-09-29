package bot

import (
	"context"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

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
