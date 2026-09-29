package api_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestBotDeliveryRequiresWorkerAndLiveUserCredentials(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.AuthenticatedHandler(
		appservices.Services{},
		signer,
		slog.New(slog.DiscardHandler),
		func(context.Context, string) (string, error) { return "", applicationauth.ErrUnauthorized },
	)
	for _, tc := range []struct{ name, host, user string }{{"user cannot call worker route", "", signer.Token("alice")}, {"worker cannot impersonate user", signer.DeliveryToken(), "expired"}, {"ordinary token is not worker credential", signer.Token("alice"), signer.Token("alice")}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(
				http.MethodPost,
				"/internal/bot-delivery/result",
				bytes.NewBufferString(`{"Owner":"alice","Chat":101}`),
			)
			request.Header.Set("Authorization", "Bearer "+tc.user)
			request.Header.Set("X-Zns-Derivation", tc.host)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusUnauthorized, response.Code)
		})
	}
}
