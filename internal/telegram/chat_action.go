package telegram

import "context"

// SendChatAction refreshes Telegram's short-lived typing indicator. It is best
// effort: callers must not retry an uncertain request as a business delivery.
func (c *Client) SendChatAction(ctx context.Context, chatID int64) error {
	var result bool
	err := c.call(ctx, "sendChatAction", struct {
		ChatID int64  `json:"chat_id"`
		Action string `json:"action"`
	}{chatID, "typing"}, &result, true)
	if err == nil && !result {
		err = &Error{Outcome: Rejected}
	}
	return err
}
