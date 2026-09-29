package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestPassDiscoveryMembershipAuthority(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	handler := api.Handler(
		appservices.NewServices(f.db, appservices.Options{}),
		f.b.Host.Signer,
		slog.New(slog.DiscardHandler),
	)
	oversized := make([]string, core.ReadPageItems+1)
	for index := range oversized {
		oversized[index] = "archive"
	}
	for _, test := range []struct {
		name, actor string
		events      []string
		status      int
		owned       bool
	}{
		{"owner", "alice", []string{"archive"}, http.StatusOK, true},
		{"foreign", "bob", []string{"archive"}, http.StatusOK, false},
		{"missing", "alice", []string{"missing"}, http.StatusOK, false},
		{"mixed", "alice", []string{"archive", "missing"}, http.StatusOK, false},
		{"duplicate", "alice", []string{"archive", "archive"}, http.StatusOK, true},
		{"empty", "alice", nil, http.StatusBadRequest, false},
		{"empty-id", "alice", []string{""}, http.StatusBadRequest, false},
		{"maximum", "alice", oversized[:core.ReadPageItems], http.StatusOK, true},
		{"oversized", "alice", oversized, http.StatusBadRequest, false},
		{"anonymous", "", []string{"archive"}, http.StatusUnauthorized, false},
		{"unknown-actor", "missing", []string{"archive"}, http.StatusForbidden, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query := url.Values{"event": test.events}
			query.Set("owner", "alice") // Query input cannot replace authenticated identity.
			r := httptest.NewRequest(http.MethodGet, "/v1/passes/bookings/owned?"+query.Encode(), nil)
			if test.actor != "" {
				r.Header.Set("Authorization", "Bearer "+f.b.Host.Signer.Token(test.actor))
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Equal(t, test.status, w.Code, w.Body.String())
			if test.status == http.StatusOK {
				var owned bool
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &owned))
				require.Equal(t, test.owned, owned)
			}
		})
	}
}
