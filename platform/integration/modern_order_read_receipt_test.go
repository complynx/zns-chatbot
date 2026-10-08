package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestModernReadPublishesOnlyCommittedCurrentReceipt(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"complete", "revision", "retirement", "cancellation", "database"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, sourceID := privateModernDraft(t, f, 52100, "")
			order, err := (orders.Service{DB: f.db}).Execute(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "read-receipt",
				Choice: orderChoice("preparty"),
			})
			require.NoError(t, err)
			const updateID int64 = 52101
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var changed atomic.Bool
			f.b.API.HTTP = &http.Client{
				Transport: retirementTransport{after: func(request *http.Request, response *http.Response) error {
					if response.StatusCode == http.StatusOK &&
						strings.HasSuffix(request.URL.Path, "/orders/"+order.ID) &&
						changed.CompareAndSwap(false, true) {
						mutateModernReadReceipt(t, f, scenario, updateID, sourceID, cancel)
					}
					return nil
				}},
			}
			observed := false
			f.b.Scripts = retirementObservedVM{
				observe: func(call scriptclient.ToolCall, result json.RawMessage, callErr error) {
					if call.Name != "orders.inspect" {
						return
					}
					observed = true
					requireModernReadReceipt(t, f, scenario, updateID, result, callErr)
				},
			}
			planned := false
			f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
				if planned {
					cancel()
					return agent.Plan{}, context.Canceled
				}
				planned = true
				return agent.Plan{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{
					Code: fmt.Sprintf(`return tools.orders.inspect({order_id:%q});`, order.ID), InputJSON: "null",
				}}, nil
			})
			_ = f.b.Handle(ctx, message(updateID, identity.AliceTelegramID, "Inspect private order"))
			require.True(t, changed.Load(), "the authorized domain read occurred")
			require.True(t, observed)
		})
	}
}

func requireModernReadReceipt(t *testing.T, f *fixture, scenario string, updateID int64,
	result json.RawMessage, callErr error) {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, updateID).Scan(&records))
	if scenario == "complete" {
		require.NoError(t, callErr)
		require.NotEmpty(t, result)
		require.Len(t, records, 1)
		require.Len(t, records[0].Calls, 1)
		require.NotEmpty(t, records[0].Calls[0].Outcome.Result, "output follows durable completion")
		return
	}
	require.Error(t, callErr)
	require.Empty(t, result, "an uncommitted or retired page cannot leave the callback")
	for _, record := range records {
		for _, stored := range record.Calls {
			require.Empty(t, stored.Outcome.Result)
		}
	}
}

func mutateModernReadReceipt(
	t *testing.T,
	f *fixture,
	scenario string,
	updateID, sourceID int64,
	cancel context.CancelFunc,
) {
	t.Helper()
	var err error
	switch scenario {
	case "revision":
		_, err = f.db.Exec(t.Context(), `UPDATE bot.interactions
 SET content=jsonb_set(content,'{0,request,input_json}','"changed"'::jsonb)
 WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, updateID)
	case "retirement":
		err = (conversation.Service{DB: f.db}).DeleteContent(t.Context(), "alice", sourceID)
	case "cancellation":
		cancel()
	case "database":
		_, err = f.db.Exec(t.Context(), `CREATE FUNCTION reject_read_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.update_id=52101 AND NEW.kind='script_runs' THEN RAISE EXCEPTION 'read receipt unavailable'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_read_receipt BEFORE INSERT OR UPDATE ON bot.interactions
 FOR EACH ROW EXECUTE FUNCTION reject_read_receipt()`)
	}
	require.NoError(t, err)
}
