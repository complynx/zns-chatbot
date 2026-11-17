package loom

import (
	"context"
	"net/http"
)

// ReasonEffectUncertain marks a run with an unknown step, write or reservation.
// Only an operator can reconcile it; do not cancel or replace such a run.
const ReasonEffectUncertain = "inference_or_effect_uncertain"

// ChatRun is the current run of a chat: its active run, or else its latest run.
// Status is "terminal" for a finished run. Reason is the blocker that a new
// message would meet. BlockingMessageID names an undelivered inbox message that
// blocks later messages and the creation of runs.
type ChatRun struct {
	RunID             string `json:"run_id,omitempty"`
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	BlockingMessageID string `json:"blocking_message_id,omitempty"`
}

// MessageReceipt reports a message that Loom retained in the chat inbox.
// Delivered means that the text reached RunID. Abandoned means that the message
// was withdrawn and Loom never delivers it. Without either, Loom keeps the
// message, and it blocks later messages of the chat.
type MessageReceipt struct {
	Accepted          bool   `json:"accepted"`
	Delivered         bool   `json:"delivered"`
	Abandoned         bool   `json:"abandoned"`
	MessageID         string `json:"message_id"`
	BlockingMessageID string `json:"blocking_message_id,omitempty"`
	RunID             string `json:"run_id,omitempty"`
	InputID           string `json:"input_id,omitempty"`
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
}

// DefaultChat returns the ID of the chat that receives runs created without a chat ID.
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

// CurrentRun locates the run that a new message of the chat would reach.
// RunID is empty when the chat has no runs.
func (c *Client) CurrentRun(ctx context.Context, auth Auth, chat string) (ChatRun, error) {
	var result ChatRun
	path, err := resourcePath("chats", chat, "/active-run")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodGet, path, auth, "", nil, &result, false, false)
	if err == nil && result.Status == "" {
		err = &Error{Outcome: Rejected, Retryable: true}
	}
	return result, err
}

// SendMessage gives text to the current run of the chat through the chat inbox.
// Loom wakes or steers a usable run and replaces a run that spent its allowance.
// Retry with the same key and text, also after an unknown response.
func (c *Client) SendMessage(ctx context.Context, auth Auth, chat, key, text string) (MessageReceipt, error) {
	var result MessageReceipt
	path, err := resourcePath("chats", chat, "/messages")
	if err != nil {
		return result, err
	}
	err = c.call(ctx, http.MethodPost, path, auth, key, struct {
		Text string `json:"text"`
	}{text}, &result, true, true)
	if err == nil && (!result.Accepted || result.MessageID == "" || result.Delivered && result.RunID == "") {
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return result, err
}

// AbandonMessage withdraws an undelivered message, so that it does not block
// later messages of the chat. It does not stop a run.
func (c *Client) AbandonMessage(ctx context.Context, auth Auth, chat, message, key string) error {
	inbox, err := resourcePath("chats", chat, "/messages")
	if err != nil {
		return err
	}
	path, err := resourcePath(inbox[1:], message, "/abandon")
	if err != nil {
		return err
	}
	var result MessageReceipt
	err = c.call(ctx, http.MethodPost, path, auth, key, struct{}{}, &result, true, true)
	if err == nil && (!result.Abandoned || result.Delivered || result.MessageID != message) {
		// The caller uses the receipt as proof that Loom never applies the text.
		err = &Error{Outcome: Unknown, Retryable: true}
	}
	return err
}
