package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func (c Host) CurrentAdminInputExpiry(ctx context.Context, id int64) (adminmessage.InputExpiry, bool, error) {
	body, err := json.Marshal(struct {
		ID int64 `json:"id"`
	}{id})
	if err != nil {
		return adminmessage.InputExpiry{}, false, err
	}
	var out struct {
		Expiry adminmessage.InputExpiry `json:"expiry"`
		Found  bool                     `json:"found"`
	}
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/input-expiry/current",
		body,
		&out,
	)
	return out.Expiry, out.Found, err
}
