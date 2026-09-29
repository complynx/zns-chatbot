package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/account"
)

func (c Client) Preferences(ctx context.Context, owner string) (account.Preferences, error) {
	var result account.Preferences
	err := c.Call(ctx, owner, http.MethodGet, "/v1/me/preferences", nil, &result)
	return result, err
}

func (c Client) SetLanguage(ctx context.Context, owner, language string, initialize bool) (account.Preferences, error) {
	return c.SetLanguageWithOperation(ctx, owner, language, initialize, "")
}

func (c Client) SetLanguageWithOperation(
	ctx context.Context,
	owner, language string,
	initialize bool,
	key account.LanguageOperationKey,
) (account.Preferences, error) {
	var result account.Preferences
	input := struct {
		OperationKey account.LanguageOperationKey `json:"operation_key,omitempty"`
		Language     string                       `json:"language"`
		Initialize   bool                         `json:"initialize"`
	}{key, language, initialize}
	err := c.Call(ctx, owner, http.MethodPut, "/v1/me/preferences/language", input, &result)
	return result, err
}
