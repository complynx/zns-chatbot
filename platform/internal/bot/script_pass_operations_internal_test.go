package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func TestPassOperationSummaryRevalidation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"unchanged", "source", "grant", "outage"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			saved := interaction.RegistrationOperationSummary{
				ID:           "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
				Tool:         scriptPassBatchAssign,
				AdmittedAt:   time.Now().UTC(),
				Status:       "pending",
				Continuation: "available",
				Context:      &interaction.RegistrationOperationContext{Event: "dance", Recipients: []int64{101}},
			}
			read := func(context.Context, string, derivedmutation.PassOperationQuery) (interaction.RegistrationOperationRead, error) {
				switch scenario {
				case "grant":
					return interaction.RegistrationOperationRead{}, &core.ProblemError{
						Status: http.StatusNotFound,
						Code:   "pass_operation_unavailable",
					}
				case "outage":
					return interaction.RegistrationOperationRead{}, &core.ProblemError{
						Status: http.StatusServiceUnavailable,
						Code:   "unavailable",
					}
				}
				current := saved
				if scenario == "source" {
					current.Context = nil
					current.Continuation = "unavailable"
				}
				return interaction.RegistrationOperationRead{
					Summaries: []interaction.RegistrationOperationSummary{current},
				}, nil
			}
			b := &Bot{}
			raw, err := json.Marshal([]interaction.RegistrationOperationSummary{saved})
			require.NoError(t, err)
			changed, err := passOperationSummaryChanged(
				t.Context(),
				"alice",
				agent.ScriptToolResult{Name: scriptPassOperations, Result: raw},
				read,
			)
			if scenario == "outage" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario != "unchanged", changed)
			if changed {
				record := agenthost.ScriptRecord{
					Calls: []agenthost.ScriptToolRecord{
						{Outcome: agent.ScriptToolResult{Name: scriptPassOperations, Result: raw}},
					},
				}
				agenthost.RedactPassScript(&record)
				_, err = (agenthost.ScriptAuthorization{ScriptDomainAuthority: botScriptAuthority{bot: b}}).AccessChanged(
					t.Context(),
					"alice",
					record,
				)
				require.NoError(t, err)
				require.True(t, record.PassRedacted)
				require.Empty(t, record.Calls[0].Outcome.Result)
			}
		})
	}
}
