package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func (c Client) ConversationWindow(ctx context.Context, owner string, count int) (conversation.Window, error) {
	if c.LocalHistory != nil {
		return directHistory(ctx, c, owner, func(s conversation.Service, actor string) (conversation.Window, error) {
			return s.Window(ctx, actor, count)
		})
	}
	var result conversation.Window
	err := c.Call(ctx, owner, http.MethodGet, "/v1/me/history/context?count="+strconv.Itoa(count), nil, &result)
	if err != nil {
		return conversation.Window{}, err
	}
	return result, err
}

func (c Client) ConversationHistory(
	ctx context.Context,
	owner string,
	q conversation.Query,
) (conversation.Page, error) {
	if c.LocalHistory != nil {
		return directHistory(ctx, c, owner, func(s conversation.Service, actor string) (conversation.Page, error) {
			return s.Read(ctx, actor, q)
		})
	}
	var result conversation.Page
	values := url.Values{
		"before": {strconv.FormatInt(q.Before, 10)},
		"after":  {strconv.FormatInt(q.After, 10)},
		"limit":  {strconv.Itoa(q.Limit)},
	}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/me/history?"+values.Encode(), nil, &result)
	if err != nil {
		return conversation.Page{}, err
	}
	return result, err
}

func (c Client) HistoryGeneration(ctx context.Context, owner string) (int64, error) {
	if c.LocalHistory != nil {
		return directHistory(ctx, c, owner, func(s conversation.Service, actor string) (int64, error) {
			return s.Generation(ctx, actor)
		})
	}
	var result struct {
		Generation int64 `json:"generation"`
	}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/me/history/generation", nil, &result)
	if err != nil {
		return 0, err
	}
	return result.Generation, err
}

func (c Client) ConversationText(ctx context.Context, owner string, id int64,
	offset, limit int, digest string,
) (conversation.TextChunk, error) {
	if c.LocalHistory != nil {
		return directHistory(ctx, c, owner, func(s conversation.Service, actor string) (conversation.TextChunk, error) {
			return s.ReadText(ctx, actor, id, offset, limit, digest)
		})
	}
	query := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}, "digest": {digest}}
	var result conversation.TextChunk
	err := c.Call(ctx, owner, http.MethodGet,
		"/v1/me/history/"+strconv.FormatInt(id, 10)+"/text?"+query.Encode(), nil, &result)
	if err != nil {
		return conversation.TextChunk{}, err
	}
	return result, nil
}

func (c Client) CheckHistoryGeneration(ctx context.Context, owner string, expected int64) error {
	generation, err := c.HistoryGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if generation != expected {
		return ErrReadStale
	}
	return nil
}
