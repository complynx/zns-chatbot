package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func (c APIClient) ConversationWindow(ctx context.Context, owner string, count int) (conversation.Window, error) {
	var result conversation.Window
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/history/context?count="+strconv.Itoa(count), nil, &result)
	return result, err
}

func (c APIClient) ConversationHistory(
	ctx context.Context,
	owner string,
	q conversation.Query,
) (conversation.Page, error) {
	var result conversation.Page
	values := url.Values{
		"before": {strconv.FormatInt(q.Before, 10)},
		"after":  {strconv.FormatInt(q.After, 10)},
		"limit":  {strconv.Itoa(q.Limit)},
	}
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/history?"+values.Encode(), nil, &result)
	return result, err
}

func (c APIClient) historyGeneration(ctx context.Context, owner string) (int64, error) {
	var result struct {
		Generation int64 `json:"generation"`
	}
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/history/generation", nil, &result)
	return result.Generation, err
}

func (c APIClient) checkHistoryGeneration(ctx context.Context, owner string, expected int64) error {
	generation, err := c.historyGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if generation != expected {
		return errScriptReadStale
	}
	return nil
}
