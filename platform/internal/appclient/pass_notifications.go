package appclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Host) PendingPassNotifications(ctx context.Context) ([]passbooking.Notification, error) {
	var notices []passbooking.Notification
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodGet, "/internal/pass-notifications", nil, &notices)
	return registrationHTTPResult(notices, err)
}

func (c Host) BeginPassNotification(
	ctx context.Context,
	input passbooking.NotificationAttempt,
) (passbooking.NotificationAdmission, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return passbooking.NotificationAdmission{}, err
	}
	var out passbooking.NotificationAdmission
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-notifications/begin",
		body,
		&out,
	)
	if err != nil {
		return passbooking.NotificationAdmission{}, err
	}
	return out, nil
}
func (c Host) CompletePassNotification(ctx context.Context, input passbooking.NotificationCompletion) error {
	return c.deliveryCommand(ctx, "/internal/pass-notifications/complete", input)
}
func (c Host) CompletePassNotificationFollowup(ctx context.Context, input passbooking.NotificationFollowup) error {
	return c.deliveryCommand(ctx, "/internal/pass-notifications/followup", input)
}

func (c Host) PassNotificationDeliveryStatus(
	ctx context.Context,
	id int64,
) (passbooking.NotificationDeliveryStatus, error) {
	var out passbooking.NotificationDeliveryStatus
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodGet,
		"/internal/pass-notifications/"+strconv.FormatInt(id, 10),
		nil,
		&out,
	)
	if err != nil {
		return passbooking.NotificationDeliveryStatus{}, err
	}
	return out, nil
}

func (c Host) PreparePassNotification(ctx context.Context, id int64) (passbooking.Notification, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return passbooking.Notification{}, false, err
	}
	var out struct {
		Notice passbooking.Notification `json:"notice"`
		Found  bool                     `json:"found"`
	}
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-notifications/prepare",
		body,
		&out,
	)
	if err != nil {
		return passbooking.Notification{}, false, err
	}
	return out.Notice, out.Found, nil
}

func (c Host) RecoverPassNotifications(ctx context.Context) ([]passbooking.Notification, error) {
	var out []passbooking.Notification
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-notifications/recover",
		nil,
		&out,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}
