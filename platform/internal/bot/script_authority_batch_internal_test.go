package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestScriptAuthorityBatchKeepsEveryDependency(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"duplicate", "unique_revoked", "oversized", "oversized_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			var leaves atomic.Int32
			server := httptest.NewServer(batchAuthorityHandler(&requests, &leaves))
			t.Cleanup(server.Close)
			b := Bot{
				Host: appclient.Host{
					Base:      server.URL,
					UserToken: func(context.Context, string) (string, error) { return "live-user", nil },
				},
			}
			record := batchAuthorityRecord(scenario)
			changed, err := (agenthost.ScriptAuthorization{ScriptDomainAuthority: botScriptAuthority{bot: &b}}).AccessChanged(
				t.Context(),
				"alice",
				record,
			)
			require.NoError(t, err)
			require.Equal(t, scenario == "unique_revoked" || scenario == "oversized_revoked", changed)
			expected := int32(1)
			if scenario == "oversized" || scenario == "oversized_revoked" {
				expected = 2
			}
			require.Equal(t, expected, requests.Load())
			if scenario == "oversized" {
				require.EqualValues(t, 400, leaves.Load(), "fallback validates every leaf")
			}
			if scenario == "duplicate" {
				require.EqualValues(t, 1, leaves.Load())
				_, err = (agenthost.ScriptAuthorization{ScriptDomainAuthority: botScriptAuthority{bot: &b}}).AccessChanged(
					t.Context(),
					"alice",
					record,
				)
				require.NoError(t, err)
				require.EqualValues(t, 2, requests.Load(), "next boundary must check again")
			}
		})
	}
}

func batchAuthorityCall(refs []readsource.Authority) agenthost.ScriptToolRecord {
	generation := int64(0)
	return agenthost.ScriptToolRecord{
		Source:            &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
		ResultAuthorities: refs,
		Outcome:           agent.ScriptToolResult{Name: "orders.inspect", Result: json.RawMessage(`{"value":"body"}`)},
	}
}

func batchAuthorityRecord(scenario string) agenthost.ScriptRecord {
	record := agenthost.ScriptRecord{
		PassContext:     []interaction.PassContextDependency{},
		ReadAuthorities: []readsource.Authority{},
	}
	switch scenario {
	case "duplicate", "unique_revoked":
		for range 8 {
			record.Calls = append(
				record.Calls,
				batchAuthorityCall(
					[]readsource.Authority{
						{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "same"}},
					},
				),
			)
		}
		if scenario == "unique_revoked" {
			record.Calls = append(
				record.Calls,
				batchAuthorityCall(
					[]readsource.Authority{
						{
							Knowledge: knowledgeauthority.ReadAuthority{
								Kind:  knowledgeauthority.Review,
								Scope: "revoked",
							},
						},
					},
				),
			)
		}
	default:
		for group := range 2 {
			refs := []readsource.Authority{}
			for n := range 200 {
				refs = append(
					refs,
					readsource.Authority{
						Knowledge: knowledgeauthority.ReadAuthority{
							Kind:  knowledgeauthority.Review,
							Scope: strconv.Itoa(group*200 + n),
						},
					},
				)
			}
			if scenario == "oversized_revoked" && group == 1 {
				refs[0].Knowledge.Scope = "revoked"
			}
			record.Calls = append(record.Calls, batchAuthorityCall(refs))
		}
	}
	return record
}

func batchAuthorityHandler(requests, leaves *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ReadAuthorities []readsource.Authority `json:"read_authorities"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !readsource.Valid(input.ReadAuthorities) {
			http.Error(w, "invalid evidence", http.StatusBadRequest)
			return
		}
		requests.Add(1)
		for _, a := range input.ReadAuthorities {
			if a.Knowledge.Kind == knowledgeauthority.Review {
				leaves.Add(1)
				if a.Knowledge.Scope == "revoked" {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]string{"code": "history_stale"})
					return
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}
