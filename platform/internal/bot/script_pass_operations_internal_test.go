package bot

import (
	"context"
	"encoding/json"
	"errors"
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
	for _, scenario := range []string{"unchanged", "source", "grant", "outage", "cancellation", "database"} {
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
				case "cancellation":
					return interaction.RegistrationOperationRead{}, context.Canceled
				case "database":
					return interaction.RegistrationOperationRead{}, core.ErrDatabase
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
			if scenario == "outage" || scenario == "cancellation" || scenario == "database" {
				assertPassSummaryUnavailable(t, scenario, changed, err)
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

func assertPassSummaryUnavailable(t *testing.T, scenario string, changed bool, err error) {
	t.Helper()
	require.Error(t, err)
	require.False(t, changed, "an unavailable check is not evidence of retirement")
	if expected := map[string]error{"cancellation": context.Canceled, "database": core.ErrDatabase}[scenario]; expected != nil {
		require.ErrorIs(t, err, expected)
	}
}

func TestPassPrivacyFailureDistinguishesDenial(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		err     error
		retired bool
	}{
		{name: "denial", err: &core.ProblemError{Status: http.StatusForbidden}, retired: true},
		{name: "absent", err: &core.ProblemError{Status: http.StatusNotFound}, retired: true},
		{name: "outage", err: &core.ProblemError{Status: http.StatusServiceUnavailable}},
		{name: "canceled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "database", err: core.ErrDatabase},
		{name: "unknown", err: errors.New("authority unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed, err := passPrivacyFailure(test.err)
			require.Equal(t, test.retired, changed)
			if test.retired {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.err)
			}
		})
	}
}
