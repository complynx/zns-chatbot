package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
)

func (c Host) BrowserAuthRecipient(ctx context.Context, username string) (browserauth.Recipient, error) {
	body, err := json.Marshal(map[string]string{"username": username})
	if err != nil {
		return browserauth.Recipient{}, err
	}
	var recipient browserauth.Recipient
	err = c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/browser-auth/recipient",
		body,
		&recipient,
	)
	return recipient, err
}
