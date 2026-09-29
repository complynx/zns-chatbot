package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type rejectedVerifier struct{}

func (rejectedVerifier) Verify(context.Context, string) (string, error) {
	return "", identity.ErrZitadelIdentity
}

type unexpectedLinks struct{ t *testing.T }

func (l unexpectedLinks) Subject(context.Context, string) (string, error) {
	l.t.Error("invalid token reached identity links")
	return "alice", nil
}

func TestAuthenticationFailsBeforeDatabase(t *testing.T) {
	t.Parallel()
	verify := ZitadelOwner(rejectedVerifier{}, unexpectedLinks{t: t})
	_, err := verify(t.Context(), "invalid")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	handler := AuthenticatedHandler(
		appservices.Services{},
		identity.Signer{},
		slog.New(slog.DiscardHandler),
		verify,
	)
	for _, header := range []string{"", "invalid", "Basic invalid", "Bearer ", "Bearer invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/workflow", nil)
		request.Header.Set("Authorization", header)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
		assert.NotContains(t, response.Body.String(), "Zitadel")
	}
}
