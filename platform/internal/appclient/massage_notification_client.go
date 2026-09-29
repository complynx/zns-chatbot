package appclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (c Host) MassageNoticeRecipients(ctx context.Context) ([]massage.NoticeRecipient, error) {
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

func (c Host) MassageDeliveryNotices(ctx context.Context, owner string) ([]massage.DeliveryNotice, error) {
	var result []massage.DeliveryNotice
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost,
		"/internal/massage-notifications/"+url.PathEscape(owner)+"/pending", nil, &result)
	return result, err
}

func (c Host) BeginMassageNotice(
	ctx context.Context,
	owner string,
	input delivery.Attempt,
) (delivery.Admission, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return delivery.Admission{}, err
	}
	var out delivery.Admission
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/massage-notifications/"+url.PathEscape(owner)+"/begin",
		body,
		&out,
	)
	if err != nil {
		return delivery.Admission{}, err
	}
	return out, nil
}
func (c Host) CompleteMassageNotice(ctx context.Context, owner string, input massage.NotificationCompletion) error {
	return c.deliveryCommand(ctx, "/internal/massage-notifications/"+url.PathEscape(owner)+"/complete", input)
}

func (c Host) CompleteMassageNoticeFollowup(
	ctx context.Context,
	owner string,
	input massage.NotificationFollowup,
) error {
	return c.deliveryCommand(ctx, "/internal/massage-notifications/"+url.PathEscape(owner)+"/followup", input)
}

func (c Host) MassageNoticeDeliveryStatus(
	ctx context.Context,
	owner string,
	id int64,
) (massage.NotificationDeliveryStatus, error) {
	var out massage.NotificationDeliveryStatus
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodGet,
		"/internal/massage-notifications/"+url.PathEscape(owner)+"/"+strconv.FormatInt(id, 10),
		nil,
		&out,
	)
	if err != nil {
		return massage.NotificationDeliveryStatus{}, err
	}
	return out, nil
}

func (c Host) PrepareMassageNotification(ctx context.Context, id int64) (massage.DeliveryNotice, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return massage.DeliveryNotice{}, false, err
	}
	var out struct {
		Notice massage.DeliveryNotice `json:"notice"`
		Found  bool                   `json:"found"`
	}
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/massage-notifications/prepare",
		body,
		&out,
	)
	if err != nil {
		return massage.DeliveryNotice{}, false, err
	}
	return out.Notice, out.Found, nil
}

func (c Host) RecoverMassageNotifications(ctx context.Context) ([]massage.DeliveryNotice, error) {
	var out []massage.DeliveryNotice
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/massage-notifications/recover",
		nil,
		&out,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}
