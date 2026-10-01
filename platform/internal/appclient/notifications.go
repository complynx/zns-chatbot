package appclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Host) PendingNotifications(ctx context.Context) ([]orders.Notification, error) {
	var notices []orders.Notification
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodGet, "/internal/notifications", nil, &notices)
	return notices, err
}

func (c Host) BeginNotification(
	ctx context.Context,
	input orders.NotificationAttempt,
) (orders.NotificationAdmission, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return orders.NotificationAdmission{}, err
	}
	var out orders.NotificationAdmission
	err = c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost, "/internal/notifications/begin", body, &out)
	if err != nil {
		return orders.NotificationAdmission{}, err
	}
	return out, nil
}
func (c Host) CompleteNotification(ctx context.Context, input orders.NotificationCompletion) error {
	return c.deliveryCommand(ctx, "/internal/notifications/complete", input)
}
func (c Host) CompleteNotificationFollowup(ctx context.Context, input orders.NotificationFollowup) error {
	return c.deliveryCommand(ctx, "/internal/notifications/followup", input)
}

func (c Host) NotificationDeliveryStatus(ctx context.Context, id int64) (orders.NotificationDeliveryStatus, error) {
	var out orders.NotificationDeliveryStatus
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodGet,
		"/internal/notifications/"+strconv.FormatInt(id, 10),
		nil,
		&out,
	)
	if err != nil {
		return orders.NotificationDeliveryStatus{}, err
	}
	return out, nil
}

func (c Host) PrepareOrderNotification(ctx context.Context, id int64) (orders.Notification, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return orders.Notification{}, false, err
	}
	var out struct {
		Notice orders.Notification `json:"notice"`
		Found  bool                `json:"found"`
	}
	err = c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost, "/internal/notifications/prepare", body, &out)
	if err != nil {
		return orders.Notification{}, false, err
	}
	return out.Notice, out.Found, nil
}

func (c Host) RecoverOrderNotifications(ctx context.Context) ([]orders.Notification, error) {
	var out []orders.Notification
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost, "/internal/notifications/recover", nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
