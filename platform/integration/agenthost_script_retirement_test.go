package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type retirementThenOutage struct {
	agenthost.ScriptAuthority

	checks   int
	restored bool
	cancel   context.CancelFunc
}

func (p *retirementThenOutage) AccessChanged(context.Context, string, agenthost.ScriptRecord) (bool, error) {
	if p.restored {
		return false, nil
	}
	p.checks++
	if p.checks == 1 {
		return true, nil
	}
	if p.cancel != nil {
		p.cancel()
		return false, context.Canceled
	}
	return false, errors.New("authority unavailable")
}

func (p *retirementThenOutage) Generation(context.Context, string) (int64, error) { return 0, nil }
func (p *retirementThenOutage) MemoryState(context.Context, string) (knowledge.MemoryDeletionState, error) {
	return knowledge.MemoryDeletionState{}, nil
}
func TestAgentHostScriptRetirementPersistsBeforeLaterFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"load/outage", "load/cancellation", "complete/outage", "complete/cancellation", "change/outage", "change/cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			const updateID int64 = 94004
			const privateText = "retired dependent model payload"
			records := make([]agenthost.ScriptRecord, 2)
			for i := range records {
				records[i] = agenthost.ScriptRecord{
					Run: agent.ScriptRun{
						Code:   privateText,
						Result: json.RawMessage(`{"private":"retired dependent model payload"}`),
					},
					Calls: []agenthost.ScriptToolRecord{
						{
							Pass: &agenthost.ScriptPassRequest{
								ID:   "kept-operation",
								Name: "passes.admin.cancel",
								Command: &passbooking.Command{
									Name:  "admin_cancel",
									Key:   "kept-domain-key",
									Event: "sandbox",
								},
							},
							Outcome: agent.ScriptToolResult{
								Name:   "passes.operations",
								Result: json.RawMessage(`[{"private":"retired dependent model payload"}]`),
							},
						},
					},
				}
			}
			_, err := f.db.Exec(
				t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',$1,'script_runs',$2)`,
				updateID,
				records,
			)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			policy := &retirementThenOutage{}
			if strings.HasSuffix(scenario, "/cancellation") {
				policy.cancel = cancel
			}
			store := agenthost.ScriptStore{DB: f.db, Policy: policy}
			switch {
			case strings.HasPrefix(scenario, "complete/"):
				_, err = store.CompleteRun(ctx, "alice", updateID, 0, records[0].Run)
			case strings.HasPrefix(scenario, "change/"):
				err = store.MarkPrivateProfile(ctx, "alice", updateID, 0)
			default:
				_, err = store.LoadAuthorized(ctx, "alice", updateID)
			}
			require.Error(t, err)
			require.Equal(t, 2, policy.checks)
			err = f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, updateID).
				Scan(&records)
			require.NoError(t, err)
			for _, record := range records {
				assert.True(t, record.PassRedacted)
				assert.False(t, record.PrivateProfile, "failed bookkeeping must not commit alongside retirement")
				assert.Empty(t, record.Run.Code)
				assert.Empty(t, record.Calls[0].Outcome.Result)
				require.NotNil(t, record.Calls[0].Pass)
				require.NotNil(t, record.Calls[0].Pass.Command)
				assert.Equal(t, "kept-domain-key", record.Calls[0].Pass.Command.Key)
			}
			policy.restored = true
			// A new store simulates retry after both the grant and service recover.
			store = agenthost.ScriptStore{DB: f.db, Policy: policy}
			retried, err := store.LoadAuthorized(t.Context(), "alice", updateID)
			require.NoError(t, err)
			encoded, err := json.Marshal(retried)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), privateText)
			for _, record := range retried {
				assert.True(t, record.PassRedacted)
				assert.False(t, record.PrivateProfile, "failed bookkeeping must not commit alongside retirement")
				assert.Equal(t, "kept-domain-key", record.Calls[0].Pass.Command.Key)
			}
		})
	}
}
