package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type authOwnerRow struct {
	exists bool
	err    error
}

func (row authOwnerRow) QueryRow(context.Context, string, ...any) pgx.Row { return row }
func (row authOwnerRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*(dest[0].(*bool)) = row.exists
	return nil
}

func TestSharedAuthenticationHTTPStatuses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, header       string
		known              bool
		providerErr, dbErr error
		status             int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "invalid", header: "Bearer token", providerErr: identity.ErrZitadelIdentity, status: http.StatusUnauthorized},
		{name: "provider outage", header: "Bearer token", providerErr: errors.New("provider secret"), status: http.StatusInternalServerError},
		{name: "unknown owner", header: "Bearer token", status: http.StatusForbidden},
		{name: "database outage", header: "Bearer token", dbErr: errors.New("database secret"), status: http.StatusInternalServerError},
		{name: "verified", header: "Bearer token", known: true, status: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			called := false
			verifiedOwner := ""
			authorizer := applicationauth.Authorizer{
				DB:     authOwnerRow{exists: test.known, err: test.dbErr},
				Verify: func(context.Context, string) (string, error) { return "alice", test.providerErr },
			}
			handler := authenticated(authorizer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				verifiedOwner = requestOwner(r)
				w.WriteHeader(http.StatusNoContent)
			}), slog.New(slog.DiscardHandler))
			request := httptest.NewRequest(http.MethodGet, "/v1/orders", nil)
			request.Header.Set("Authorization", test.header)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code)
			require.Equal(t, test.status == http.StatusNoContent, called)
			if called {
				require.Equal(t, "alice", verifiedOwner)
			}
			require.NotContains(t, response.Body.String(), "secret")
		})
	}
}
