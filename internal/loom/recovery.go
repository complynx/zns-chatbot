package loom

import (
	"context"
	"net/http"
)

type RecoverMessage struct {
	Text         string `json:"text"`
	ReplaceRunID string `json:"replace_run_id,omitempty"`
}

type RecoveryReceipt struct {
	Accepted          bool   `json:"accepted"`
	Delivered         bool   `json:"delivered"`
	Abandoned         bool   `json:"abandoned"`
	MessageID         string `json:"message_id,omitempty"`
	BlockingMessageID string `json:"blocking_message_id,omitempty"`
	Sequence          int64  `json:"sequence,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	InputID           string `json:"input_id,omitempty"`
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	NextAction        string `json:"next_action"`
}

type ActiveRun struct {
	RunID      string `json:"run_id,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	NextAction string `json:"next_action"`
}

func (c *Client) SupportsConversationRecovery(ctx context.Context, auth Auth) (bool, error) {
	var result struct {
		Recovery struct {
			Version int `json:"version"`
		} `json:"conversation_recovery"`
	}
	err := c.call(ctx, http.MethodGet, "/capabilities", auth, "", nil, &result, false, false)
	return result.Recovery.Version == 1, err
}

func (c *Client) DefaultChat(ctx context.Context, auth Auth) (string, error) {
	var result struct {
		ID string `json:"id"`
	}
	err := c.call(ctx, http.MethodGet, "/chats/default", auth, "", nil, &result, false, false)
	if err == nil && result.ID == "" {
		err = &Error{Outcome: Rejected, Retryable: true}
	}
	return result.ID, err
}

func (c *Client) ActiveRun(ctx context.Context, auth Auth, chat string) (ActiveRun, error) {
	var result ActiveRun
	path, err := resourcePath("chats", chat, "/active-run")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodGet, path, auth, "", nil, &result, false, false)
	if err == nil && (result.Status == "" || result.NextAction == "") {
		err = &Error{Outcome: Rejected, Retryable: true}
	}
	return result, err
}

// RecoverMessage retries use the original key and body, including after an unknown response.
func (c *Client) RecoverMessage(
	ctx context.Context,
	auth Auth,
	chat, key string,
	input RecoverMessage,
) (RecoveryReceipt, error) {
	var result RecoveryReceipt
	path, err := resourcePath("chats", chat, "/messages")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodPost, path, auth, key, input, &result, true, true)
	if err == nil && (!result.Accepted || result.MessageID == "" || result.Sequence <= 0 || result.Status == "" ||
		result.NextAction == "" || result.Delivered && result.RunID == "") {
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return result, err
}

// AbandonMessage withdraws retained input as its original actor. It never cancels a run.
func (c *Client) AbandonMessage(ctx context.Context, auth Auth, chat, message, key string) (RecoveryReceipt, error) {
	var result RecoveryReceipt
	path, err := resourcePath("chats", chat, "/messages")
	if err != nil {
		return result, err
	}
	path, err = resourcePath(path[1:], message, "/abandon")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodPost, path, auth, key, struct{}{}, &result, true, true)
	if err == nil && (!result.Accepted || !result.Abandoned || result.Delivered || result.MessageID != message) {
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return result, err
}
