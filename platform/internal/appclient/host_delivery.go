package appclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Host) deliveryCommand(ctx context.Context, path string, input any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var out struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost, path, body, &out)
}
func (c Host) RegisterAdminMessageSource(ctx context.Context, input adminmessage.Source) error {
	return c.deliveryCommand(ctx, "/internal/admin-messages/source", input)
}
func (c Host) ClaimAdminInputExpiry(ctx context.Context) (adminmessage.InputExpiry, bool, error) {
	var out struct {
		Expiry adminmessage.InputExpiry `json:"expiry"`
		Found  bool                     `json:"found"`
	}
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/input-expiry/claim",
		nil,
		&out,
	)
	return out.Expiry, out.Found, err
}
func (c Host) CompleteAdminInputExpiry(ctx context.Context, id, attempt int64) error {
	return c.deliveryCommand(
		ctx,
		"/internal/admin-messages/input-expiry/complete",
		map[string]int64{"id": id, "attempt": attempt},
	)
}
func (c Host) FoodNotifications(ctx context.Context) ([]legacyfood.Notification, error) {
	var out []legacyfood.Notification
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodGet, "/internal/food/notifications", nil, &out)
	return out, err
}
func (c Host) BeginFoodNotification(ctx context.Context, input delivery.Attempt) (delivery.Admission, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return delivery.Admission{}, err
	}
	var out delivery.Admission
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/food/notifications/begin",
		body,
		&out,
	)
	if err != nil {
		return delivery.Admission{}, err
	}
	return out, nil
}
func (c Host) CompleteFoodNotification(ctx context.Context, input legacyfood.NotificationCompletion) error {
	return c.deliveryCommand(ctx, "/internal/food/notifications/complete", input)
}
func (c Host) CompleteFoodNotificationFollowup(ctx context.Context, input legacyfood.NotificationFollowup) error {
	return c.deliveryCommand(ctx, "/internal/food/notifications/followup", input)
}
func (c Host) ClaimRegistrationAnnouncement(ctx context.Context) (passbooking.RegistrationAnnouncement, bool, error) {
	var out struct {
		Announcement passbooking.RegistrationAnnouncement `json:"announcement"`
		Found        bool                                 `json:"found"`
	}
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-announcements/claim",
		nil,
		&out,
	)
	out, err = registrationHTTPResult(out, err)
	return out.Announcement, out.Found, err
}
func (c Host) CompleteRegistrationAnnouncement(ctx context.Context, input passbooking.AnnouncementCompletion) error {
	return c.deliveryCommand(ctx, "/internal/pass-announcements/complete", input)
}

func (c Host) BeginRegistrationAnnouncement(ctx context.Context, input delivery.Attempt) (delivery.Admission, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return delivery.Admission{}, err
	}
	var out delivery.Admission
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-announcements/begin",
		body,
		&out,
	)
	return registrationHTTPResult(out, err)
}

func (c Host) FoodNotificationDeliveryStatus(
	ctx context.Context,
	id int64,
) (legacyfood.NotificationDeliveryStatus, error) {
	var out legacyfood.NotificationDeliveryStatus
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodGet,
		"/internal/food/notifications/"+strconv.FormatInt(id, 10),
		nil,
		&out,
	)
	if err != nil {
		return legacyfood.NotificationDeliveryStatus{}, err
	}
	return out, nil
}

func (c Host) PrepareFoodNotification(ctx context.Context, id int64) (legacyfood.Notification, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return legacyfood.Notification{}, false, err
	}
	var out struct {
		Notice legacyfood.Notification `json:"notice"`
		Found  bool                    `json:"found"`
	}
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/food/notifications/prepare",
		body,
		&out,
	)
	if err != nil {
		return legacyfood.Notification{}, false, err
	}
	return out.Notice, out.Found, nil
}

func (c Host) RecoverFoodNotifications(ctx context.Context) ([]legacyfood.Notification, error) {
	var out []legacyfood.Notification
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/food/notifications/recover",
		nil,
		&out,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}
