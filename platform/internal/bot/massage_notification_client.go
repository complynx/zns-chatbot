package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (c APIClient) MassageNoticeRecipients(ctx context.Context) ([]massage.NoticeRecipient, error) {
	var result []massage.NoticeRecipient
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodGet,
		"/internal/massage-notifications",
		nil,
		&result,
	)
	return result, err
}

func (c APIClient) MassageDeliveryNotices(ctx context.Context, owner string) ([]massage.DeliveryNotice, error) {
	var result []massage.DeliveryNotice
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost,
		"/internal/massage-notifications/"+url.PathEscape(owner)+"/pending", nil, &result)
	return result, err
}

func (c APIClient) CompleteMassageNotice(ctx context.Context, owner string, id int64) error {
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/massage-notifications/"+url.PathEscape(
			owner,
		)+"/"+strconv.FormatInt(
			id,
			10,
		)+"/complete",
		nil,
		&result,
	)
}
