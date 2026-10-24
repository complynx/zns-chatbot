package loom_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestOutputValidationRequiresExactAdvertisedCapability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		status     int
		accepted   bool
	}{
		{"supported", `{"output_validation":{"version":1,"disallow_custom_emoji":true}}`, 200, true},
		{"missing", `{}`, 200, false},
		{"disabled", `{"output_validation":{"version":1,"disallow_custom_emoji":false}}`, 200, false},
		{"future", `{"output_validation":{"version":2,"disallow_custom_emoji":true}}`, 200, false},
		{"old", `{}`, 404, false},
		{"unauthorized", `{}`, 401, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v1/capabilities", r.URL.Path)
				assert.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
				assert.Equal(t, "session", r.Header.Get("X-Auth-Session-Id"))
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client, err := loom.New(server.URL, server.Client())
			require.NoError(t, err)
			err = client.RequireOutputValidation(t.Context(), loom.Auth{ServiceToken: "fixture", SessionID: "session"})
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
