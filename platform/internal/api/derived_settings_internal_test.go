package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestDerivedSettingsRoutesRequireHostEvidence(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	authorizer := applicationauth.Authorizer{
		DB:     authOwnerRow{exists: true},
		Verify: func(context.Context, string) (string, error) { return "alice", nil },
	}
	mux := http.NewServeMux()
	derivedSettingsRoutes(mux, derivedmutation.Service{}, authorizer, signer, slog.New(slog.DiscardHandler))
	for _, path := range []string{"/internal/derived/preferences/language", "/internal/derived/model-settings/alice", "/internal/derived/model-grants", "/internal/derived/credit-policy/alice"} {
		for _, host := range []bool{false, true} {
			request := httptest.NewRequest(
				http.MethodPost,
				path,
				strings.NewReader(`{"actor":"bob","command":{},"source":{"generation":0,"authorities":[]}}`),
			)
			request.Header.Set("Authorization", "Bearer live-user")
			want := http.StatusUnauthorized
			if host {
				request.Header.Set(derivedHostHeader, signer.DerivedMutationToken("alice"))
				want = http.StatusBadRequest
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			require.Equal(t, want, response.Code, path)
		}
	}
}
