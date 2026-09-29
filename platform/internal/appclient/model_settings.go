package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func (c Client) EffectiveModelSelection(ctx context.Context, owner string) (modelsettings.Selection, error) {
	var selection modelsettings.Selection
	err := c.Call(ctx, owner, http.MethodGet, "/v1/model-settings/effective", nil, &selection)
	return selection, err
}
