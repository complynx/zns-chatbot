package telegram

import "context"

// SetWebhookRequest keeps the authentication secret out of status/readback data.
type SetWebhookRequest struct {
	URL                string   `json:"url"`
	SecretToken        string   `json:"secret_token"`
	AllowedUpdates     []string `json:"allowed_updates"`
	DropPendingUpdates bool     `json:"drop_pending_updates"`
}

type WebhookInfo struct {
	URL            string   `json:"url"`
	AllowedUpdates []string `json:"allowed_updates"`
}

// SetWebhook reapplies declarative configuration, including the unreadable secret.
// It never retries a lost response automatically.
func (c *Client) SetWebhook(ctx context.Context, input SetWebhookRequest) error {
	var accepted bool
	if err := c.call(ctx, "setWebhook", input, &accepted, true); err != nil {
		return err
	}
	if !accepted {
		return &Error{Outcome: Rejected}
	}
	return nil
}

func (c *Client) GetWebhookInfo(ctx context.Context) (WebhookInfo, error) {
	var info WebhookInfo
	err := c.call(ctx, "getWebhookInfo", struct{}{}, &info, false)
	return info, err
}
