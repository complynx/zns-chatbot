package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (c Client) LegacyMassageDraft(ctx context.Context, owner, event, id string) (massage.LegacyDraft, error) {
	var result massage.LegacyDraft
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/massage/legacy-draft?"+url.Values{knowledgeEventQuery: {event}, "id": {id}}.Encode(),
		nil,
		&result,
	)
	return result, err
}

func (c Client) ExecuteLegacyMassage(
	ctx context.Context,
	owner string,
	command massage.LegacyCommand,
) (massage.LegacyDraft, error) {
	var result massage.LegacyDraft
	err := c.Call(ctx, owner, http.MethodPost, "/v1/massage/legacy-draft", command, &result)
	return result, err
}
