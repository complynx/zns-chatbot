package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type ingressProvisioner struct {
	calls int
	user  identityprovision.Telegram
	err   error
}

func (p *ingressProvisioner) EnsureTelegram(
	_ context.Context,
	u identityprovision.Telegram,
) (identityprovision.Binding, error) {
	p.calls++
	p.user = u
	return identityprovision.Binding{Owner: "opaque", Subject: "reserved"}, p.err
}

func TestProvisioningEndpointAuthenticatesExactRequest(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	valid := identity.TelegramProvisioningRequest{
		BotID: 77,
		User:  telegram.User{ID: 101, FirstName: "Synthetic", LanguageCode: "ru"},
	}
	for _, name := range []string{"valid", "ordinary", "memory", "absent", "wrongbot", "tamper", "bodybot", "botuser", "zero", "oversized", "malformed"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := provisioningBody(t, name, valid)
			token := signer.TelegramProvisioningToken(77, body)
			want := http.StatusOK
			switch name {
			case "ordinary":
				token = signer.Token("101")
				want = http.StatusUnauthorized
			case "memory":
				token = signer.MemoryProvenanceToken("101")
				want = http.StatusUnauthorized
			case "absent":
				token = ""
				want = http.StatusUnauthorized
			case "wrongbot":
				token = signer.TelegramProvisioningToken(88, body)
				want = http.StatusUnauthorized
			case "tamper":
				body = bytes.ReplaceAll(body, []byte("101"), []byte("202"))
				want = http.StatusUnauthorized
			case "bodybot", "botuser", "zero", "malformed":
				want = http.StatusBadRequest
			case "oversized":
				want = http.StatusRequestEntityTooLarge
			}
			provider := &ingressProvisioner{}
			handler := api.WithTelegramProvisioning(http.NotFoundHandler(), provider, signer, 77)
			r := httptest.NewRequest(http.MethodPost, "/internal/identity/telegram", bytes.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Equal(t, want, w.Code)
			if name == "valid" {
				require.Equal(t, 1, provider.calls)
				require.Equal(t, int64(101), provider.user.ID)
				require.Equal(t, "ru", provider.user.Language)
			} else {
				require.Zero(t, provider.calls)
			}
		})
	}
}

func provisioningBody(t *testing.T, name string, input identity.TelegramProvisioningRequest) []byte {
	t.Helper()
	switch name {
	case "bodybot":
		input.BotID = 88
	case "botuser":
		input.User.IsBot = true
	case "zero":
		input.User.ID = 0
	case "oversized":
		return []byte(strings.Repeat(" ", identity.MaxProvisioningBytes+1))
	case "malformed":
		return []byte(`{"invalid":true}`)
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	return body
}
