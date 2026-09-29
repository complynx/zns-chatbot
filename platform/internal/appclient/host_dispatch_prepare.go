package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Host) PrepareAdminMessage(ctx context.Context, id int64) (adminmessage.Delivery, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return adminmessage.Delivery{}, false, err
	}
	var out struct {
		Delivery adminmessage.Delivery `json:"delivery"`
		Found    bool                  `json:"found"`
	}
	err = c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost, "/internal/admin-messages/prepare", body, &out)
	return out.Delivery, out.Found, err
}

func (c Host) PrepareRegistrationAnnouncement(
	ctx context.Context,
	id int64,
) (passbooking.RegistrationAnnouncement, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return passbooking.RegistrationAnnouncement{}, false, err
	}
	var out struct {
		Announcement passbooking.RegistrationAnnouncement `json:"announcement"`
		Found        bool                                 `json:"found"`
	}
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-announcements/prepare",
		body,
		&out,
	)
	out, err = registrationHTTPResult(out, err)
	return out.Announcement, out.Found, err
}
func (c Host) RecoverAdminMessages(ctx context.Context) error {
	return c.deliveryCommand(ctx, "/internal/admin-messages/recover", struct{}{})
}
func (c Host) RecoverRegistrationAnnouncements(ctx context.Context) error {
	return c.deliveryCommand(ctx, "/internal/pass-announcements/recover", struct{}{})
}
