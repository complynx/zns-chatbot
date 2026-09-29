package bot

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (c APIClient) Preferences(ctx context.Context, owner string) (core.Preferences, error) {
	var result core.Preferences
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/preferences", nil, &result)
	return result, err
}

func (c APIClient) SetLanguage(ctx context.Context, owner, language string, initialize bool) (core.Preferences, error) {
	return c.SetLanguageWithOperation(ctx, owner, language, initialize, "")
}

func (c APIClient) SetLanguageWithOperation(
	ctx context.Context,
	owner, language string,
	initialize bool,
	key core.LanguageOperationKey,
) (core.Preferences, error) {
	var result core.Preferences
	input := struct {
		OperationKey core.LanguageOperationKey `json:"operation_key,omitempty"`
		Language     string                    `json:"language"`
		Initialize   bool                      `json:"initialize"`
	}{key, language, initialize}
	err := c.call(ctx, owner, http.MethodPut, "/v1/me/preferences/language", input, &result)
	return result, err
}
