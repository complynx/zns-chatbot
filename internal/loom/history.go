package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

type ReactionPolicy struct {
	Emoji []string `json:"emoji"`
}

// ChatEvent uses chat history sequence numbers, not the run's SSE cursor or Telegram IDs.
type ChatEvent struct {
	Seq          int64               `json:"seq"`
	ReactionsSeq int64               `json:"reactions_seq,omitempty"`
	Type         string              `json:"type"`
	RunID        string              `json:"run_id,omitempty"`
	Data         json.RawMessage     `json:"data,omitempty"`
	Reactions    map[string][]string `json:"reactions,omitempty"`
}

func (c *Client) ChatHistory(ctx context.Context, auth Auth, chat string, after int64, limit int) ([]ChatEvent, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, &Error{Outcome: NotStarted}
	}
	path, err := resourcePath("chats", chat, "/history")
	if err != nil {
		return nil, err
	}
	var result struct {
		Events []ChatEvent `json:"events"`
	}
	err = c.call(ctx, http.MethodGet, path+"?after="+strconv.FormatInt(after, 10)+"&limit="+strconv.Itoa(limit),
		auth, "", nil, &result, false, false)
	if err != nil {
		return nil, err
	}
	if len(result.Events) > limit {
		return nil, &Error{Outcome: Rejected}
	}
	for _, event := range result.Events {
		if event.Seq <= after || event.ReactionsSeq < 0 {
			return nil, &Error{Outcome: Rejected}
		}
		after = event.Seq
	}
	return result.Events, nil
}
