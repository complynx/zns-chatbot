package bot

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

// modelSettingsContext freezes the effective choice for this logical request.
func (b *Bot) modelSettingsContext(ctx context.Context, owner string) (context.Context, error) {
	var selection modelsettings.Selection
	if err := b.API.call(ctx, owner, http.MethodGet, "/v1/model-settings/effective", nil, &selection); err != nil {
		return nil, err
	}
	return modelsettings.WithSelection(ctx, selection), nil
}
