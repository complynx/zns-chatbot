package telegram

import "context"

// SendChatAction refreshes Telegram's short-lived activity indicator. It is best
// effort: callers must not retry an uncertain request as a business delivery.
func (c *Client) SendChatAction(ctx context.Context, chatID int64) error {
	return c.SendActivity(ctx, chatID, false)
}

// SendActivity uses sticker selection while processing incoming visual symbols.
func (c *Client) SendActivity(ctx context.Context, chatID int64, choosingSticker bool) error {
	action := "typing"
	if choosingSticker {
		action = "choose_sticker"
	}
	var result bool
	err := c.call(ctx, "sendChatAction", struct {
		ChatID int64  `json:"chat_id"`
		Action string `json:"action"`
	}{chatID, action}, &result, true)
	if err == nil && !result {
		err = &Error{Outcome: Rejected}
	}
	return err
}
