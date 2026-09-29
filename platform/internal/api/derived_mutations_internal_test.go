package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedMutationRevalidatesAcrossDenialAndRecovery(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	var providerErr error
	calls, mutations := 0, 0
	authorizer := applicationauth.Authorizer{DB: authOwnerRow{exists: true},
		Verify: func(context.Context, string) (string, error) { calls++; return "alice", providerErr }}
	handler := authenticatedDerivation(
		authorizer,
		signer,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mutations++
			w.WriteHeader(http.StatusNoContent)
		}),
		slog.New(slog.DiscardHandler),
	)
	for _, step := range []struct {
		err    error
		status int
	}{
		{nil, http.StatusNoContent},
		{identity.ErrZitadelUserInactive, http.StatusUnauthorized},
		{errors.New("provider unavailable"), http.StatusInternalServerError},
		{nil, http.StatusNoContent},
	} {
		providerErr = step.err
		request := httptest.NewRequest(http.MethodPost, "/internal/derived/actions", nil)
		request.Header.Set("Authorization", "Bearer same-user-credential")
		request.Header.Set(derivedHostHeader, signer.DerivedMutationToken("alice"))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, step.status, response.Code)
	}
	require.Equal(t, 4, calls)
	require.Equal(t, 2, mutations)
}

func TestDerivedMutationRequiresHostAndLiveMatchingUser(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	for _, test := range []struct {
		name, host, user, owner string
		providerErr             error
		status, calls           int
	}{
		{name: "ordinary user", user: "Bearer user", status: http.StatusUnauthorized},
		{name: "user as host", host: signer.Token("alice"), user: "Bearer user", status: http.StatusUnauthorized},
		{name: "memory as host", host: signer.MemoryProvenanceToken("alice"), user: "Bearer user", status: http.StatusUnauthorized},
		{name: "delivery as host", host: signer.DeliveryToken(), user: "Bearer user", status: http.StatusUnauthorized},
		{name: "host only", host: signer.DerivedMutationToken("alice"), status: http.StatusUnauthorized},
		{name: "malformed user", host: signer.DerivedMutationToken("alice"), user: "user", status: http.StatusUnauthorized},
		{name: "mismatch", host: signer.DerivedMutationToken("bob"), user: "Bearer user", owner: "alice", status: http.StatusUnauthorized, calls: 1},
		{name: "inactive", host: signer.DerivedMutationToken("alice"), user: "Bearer user", providerErr: identity.ErrZitadelUserInactive, status: http.StatusUnauthorized, calls: 1},
		{name: "outage", host: signer.DerivedMutationToken("alice"), user: "Bearer user", providerErr: errors.New("provider unavailable"), status: http.StatusInternalServerError, calls: 1},
		{name: "verified", host: signer.DerivedMutationToken("alice"), user: "Bearer user", owner: "alice", status: http.StatusNoContent, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls, mutations := 0, 0
			var logs bytes.Buffer
			authorizer := applicationauth.Authorizer{DB: authOwnerRow{exists: true},
				Verify: func(context.Context, string) (string, error) {
					calls++
					return test.owner, test.providerErr
				}}
			handler := authenticatedDerivation(
				authorizer,
				signer,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					mutations++
					w.WriteHeader(http.StatusNoContent)
				}),
				slog.New(slog.NewJSONHandler(&logs, nil)),
			)
			for range 2 {
				request := httptest.NewRequest(http.MethodPost, "/internal/derived/actions", nil)
				request.Header.Set("Authorization", test.user)
				request.Header.Set(derivedHostHeader, test.host)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				require.Equal(t, test.status, response.Code)
			}
			require.Equal(t, 2*test.calls, calls, "each attempt revalidates the user")
			if test.status == http.StatusNoContent {
				require.Equal(t, 2, mutations)
			} else {
				require.Zero(t, mutations)
			}
			if test.host != "" {
				require.NotContains(t, logs.String(), test.host)
			}
			if test.user != "" {
				require.NotContains(t, logs.String(), test.user)
			}
		})
	}
}

func TestDerivedMutationRoutesRejectForgedAndMalformedBodies(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	authorizer := applicationauth.Authorizer{DB: authOwnerRow{exists: true},
		Verify: func(context.Context, string) (string, error) { return "alice", nil }}
	mux := http.NewServeMux()
	derivedMutationRoutes(mux, derivedmutation.Service{}, authorizer, signer, slog.New(slog.DiscardHandler))
	derivedReceiptRoutes(
		mux,
		derivedmutation.Service{},
		knowledge.Service{},
		authorizer,
		signer,
		slog.New(slog.DiscardHandler),
	)
	routes := append(receiptTestRoutes(), "/internal/derived/actions", "/internal/derived/order-actions")
	for _, route := range routes {
		for _, body := range []string{
			`{"actor":"bob","command":{},"source":{"generation":0,"authorities":[]}}`,
			`{"command":{"actor":"bob"},"source":{"generation":0,"authorities":[]}}`,
			`{"command":{},"source":{"generation":0,"authorities":[],"forged":true}}`,
			`{"command":{},"source":{"authorities":[]}}`,
			`{"command":{},"source":{"generation":0}}`,
			`{"command":{},"source":null}`, `{"source":{"generation":0,"authorities":[]}}`,
			`{"command":null,"source":{"generation":0,"authorities":[]}}`,
			`{"command":{},"source":{"generation":0,"authorities":[]}} {}`, `null`,
		} {
			request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer live-user")
			request.Header.Set(derivedHostHeader, signer.DerivedMutationToken("alice"))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code, "%s %s", route, body)
		}
	}
}

func receiptTestRoutes() []string {
	return []string{
		"/internal/derived/actions/receipt", "/internal/derived/order-actions/receipt",
		"/internal/derived/pass-actions/receipt", "/internal/derived/pass-assignments/receipt",
		"/internal/derived/pass-profiles/receipt", "/internal/knowledge/derived/receipt",
	}
}

func TestReceiptRoutesRequireLiveMatchingCredentials(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	for _, step := range []struct {
		name, owner, host, user string
		providerErr             error
		status                  int
	}{
		{name: "ordinary user", user: "Bearer user", status: http.StatusUnauthorized},
		{name: "host only", host: signer.DerivedMutationToken("alice"), status: http.StatusUnauthorized},
		{name: "foreign host", owner: "alice", host: signer.DerivedMutationToken("bob"), user: "Bearer user", status: http.StatusUnauthorized},
		{name: "inactive", host: signer.DerivedMutationToken("alice"), user: "Bearer user", providerErr: identity.ErrZitadelUserInactive, status: http.StatusUnauthorized},
		{name: "outage", host: signer.DerivedMutationToken("alice"), user: "Bearer user", providerErr: errors.New("provider unavailable"), status: http.StatusInternalServerError},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()
			authorizer := applicationauth.Authorizer{
				DB: authOwnerRow{exists: true},
				Verify: func(context.Context, string) (string, error) {
					return step.owner, step.providerErr
				},
			}
			mux := http.NewServeMux()
			// Empty services make accidental domain access fail this boundary test.
			derivedReceiptRoutes(
				mux,
				derivedmutation.Service{},
				knowledge.Service{},
				authorizer,
				signer,
				slog.New(slog.DiscardHandler),
			)
			for _, route := range receiptTestRoutes() {
				request := httptest.NewRequest(
					http.MethodPost,
					route,
					strings.NewReader(`{"command":{},"source":{"generation":0,"authorities":[]}}`),
				)
				request.Header.Set("Authorization", step.user)
				request.Header.Set(derivedHostHeader, step.host)
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)
				require.Equal(t, step.status, response.Code, route)
			}
		})
	}
}

func TestDerivedMutationCommandEnvelopeLimits(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	authorizer := applicationauth.Authorizer{DB: authOwnerRow{exists: true},
		Verify: func(context.Context, string) (string, error) { return "alice", nil }}
	mux := http.NewServeMux()
	derivedMutationRoutes(mux, derivedmutation.Service{}, authorizer, signer, slog.New(slog.DiscardHandler))
	for _, test := range []struct {
		route string
		limit int
	}{
		{"/internal/derived/actions", maxRequestBytes},
		{"/internal/derived/order-actions", maxDerivedOrderCommandBytes},
	} {
		for _, extra := range []int{0, 1} {
			command := `{"name":"` + strings.Repeat("x", test.limit-len(`{"name":""}`)+extra) + `"}`
			body := `{"command":` + command + `,"source":{"generation":0,"authorities":[]}}`
			request := httptest.NewRequest(http.MethodPost, test.route, strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer live-user")
			request.Header.Set(derivedHostHeader, signer.DerivedMutationToken("alice"))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
			if extra == 0 {
				// The full command reached the service; missing agent origin is a
				// domain refusal before any database access.
				require.JSONEq(t, `{"code":"invalid_derivation"}`, response.Body.String())
			} else {
				require.JSONEq(t, `{"code":"invalid_json"}`, response.Body.String())
			}
		}
		body := `{"command":{},"source":{"generation":0,"authorities":[]}}` +
			strings.Repeat(" ", readsource.MaxAuthorityBytes+test.limit+4096)
		request := httptest.NewRequest(http.MethodPost, test.route, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer live-user")
		request.Header.Set(derivedHostHeader, signer.DerivedMutationToken("alice"))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.JSONEq(t, `{"code":"invalid_json"}`, response.Body.String())
	}
}
