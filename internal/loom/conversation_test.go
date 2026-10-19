package loom_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestAgentModeRequiresEchoedNegotiation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{loom.ReplyModeAgent, loom.ReplyModeProtocol, ""} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input loom.CreateRunRequest
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Interaction.ReplyMode != loom.ReplyModeAgent {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).
					Encode(loom.Run{ID: "fixture-run", Status: "waiting_input", Interaction: loom.Interaction{ReplyMode: mode}})
			}))
			defer server.Close()
			client, err := loom.New(server.URL, server.Client())
			require.NoError(t, err)
			_, err = client.CreateRun(
				t.Context(),
				loom.Auth{ServiceToken: "fixture", SessionID: "fixture"},
				"operation",
				loom.CreateRunRequest{
					Input:       loom.Input{Text: "hello"},
					Interaction: loom.Interaction{ReplyMode: loom.ReplyModeAgent},
				},
			)
			if mode == loom.ReplyModeAgent {
				require.NoError(t, err)
			} else {
				var failure *loom.Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, "unsupported_reply_mode", failure.Code)
				require.Equal(t, loom.Rejected, failure.Outcome)
			}
		})
	}
}
