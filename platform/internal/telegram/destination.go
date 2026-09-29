package telegram

import "context"

// ResolveChat freezes a trusted Telegram alias lookup for durable publication.
func (c Client) ResolveChat(ctx context.Context, alias string) (int64, error) {
	var chat struct {
		ID int64 `json:"id"`
	}
	err := RetryControl(ctx, func(callCtx context.Context) error {
		return c.Call(callCtx, "getChat", struct {
			Chat string `json:"chat_id"`
		}{alias}, &chat)
	})
	return chat.ID, err
}
