package appclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestOrderChoiceSnapshotResponseValidation(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, body, id string
		status         int
		valid          bool
	}{
		{"catalog", `{"catalog":"` + digest + `"}`, "", 200, true},
		{"order", `{"catalog":"` + digest + `","order":"` + digest + `"}`, "order", 200, true},
		{"private_unknown", `{"catalog":"` + digest + `","customer":"PRIVATE"}`, "", 200, true},
		{"empty", `{}`, "", 200, false},
		{"bad_digest", `{"catalog":"no"}`, "", 200, false},
		{"missing_order", `{"catalog":"` + digest + `"}`, "order", 200, false},
		{"unexpected_order", `{"catalog":"` + digest + `","order":"` + digest + `"}`, "", 200, false},
		{"partial_json", `{"catalog":"` + digest + `",`, "", 200, false},
		{"denied_partial", `{"catalog":"` + digest + `"}`, "", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/order-events/event/choice-snapshot", r.URL.Path)
				assert.Equal(t, tc.id, r.URL.Query().Get("order_id"))
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			client := appclient.Client{
				Base:         server.URL,
				HTTP:         server.Client(),
				SandboxToken: func(string) string { return "synthetic" },
			}
			result, err := client.OrderChoiceSnapshot(t.Context(), "alice", "event", tc.id)
			if !tc.valid {
				require.Error(t, err)
				require.Equal(t, orders.ChoiceSnapshot{}, result)
				return
			}
			require.NoError(t, err)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "PRIVATE")
			require.Equal(t, digest, result.Catalog)
		})
	}
}
