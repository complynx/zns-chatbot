package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type retirementTransport struct {
	after func(*http.Request, *http.Response) error
}

func (transport retirementTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil {
		if err = transport.after(request, response); err != nil {
			_ = response.Body.Close()
			return nil, err
		}
	}
	return response, err
}

type retirementObservedVM struct {
	scopeVM

	before  func(scriptclient.ToolCall)
	observe func(scriptclient.ToolCall, json.RawMessage, error)
}

type retirementCompletionTracer struct {
	before func(pgx.TraceQueryStartData)
}

func (tracer retirementCompletionTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	tracer.before(data)
	return ctx
}

func (retirementCompletionTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (vm retirementObservedVM) Execute(ctx context.Context, request scriptclient.Request,
	tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
	return vm.scopeVM.Execute(
		ctx,
		request,
		tools,
		func(callContext context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			if vm.before != nil {
				vm.before(call)
			}
			result, err := callback(callContext, call)
			vm.observe(call, result, err)
			return result, err
		},
	)
}

func TestAgentHostLateAdmissionDenialDoesNotClaimChoice(t *testing.T) {
	t.Parallel()
	f := setup(t)
	parentRef, sourceID := privateModernDraft(t, f, 48900, "")
	const updateID int64 = 48901
	var inChoice, prepared, admission, retired atomic.Bool
	transport := &http.Client{
		Transport: retirementTransport{after: func(request *http.Request, response *http.Response) error {
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return nil
			}
			if inChoice.Load() && strings.HasPrefix(request.URL.Path, "/v1/order-events/") {
				prepared.Store(true)
			}
			if admission.Load() && request.URL.Path == "/internal/history/authority" &&
				retired.CompareAndSwap(false, true) {
				// The admission's first real authority request succeeded. Retire its
				// canonical source before the detached candidate is reauthorized.
				return (conversation.Service{DB: f.db}).DeleteContent(request.Context(), "alice", sourceID)
			}
			return nil
		}},
	}
	f.b.API.HTTP, f.b.Host.HTTP = transport, transport
	config := f.db.Config()
	config.ConnConfig.Tracer = lateAdmissionTracer(&prepared, &admission, updateID)
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f.b.DB = pool
	observed := false
	f.b.Scripts = retirementObservedVM{
		before: func(call scriptclient.ToolCall) { inChoice.Store(call.Name == "orders.choice") },
		observe: func(call scriptclient.ToolCall, _ json.RawMessage, _ error) {
			observed = observed || call.Name == "orders.choice"
		},
	}
	modelCalls := 0
	f.b.Model = avModel(func(_ context.Context, _ agent.Input) (agent.Plan, error) {
		modelCalls++
		if modelCalls == 1 {
			return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{
				Code: fmt.Sprintf(
					`return tools.orders.choice({operation:"patch",choice_ref:%q,customer_first_name:"fresh"});`,
					parentRef,
				),
				InputJSON: "null",
			}}, nil
		}
		return agent.Plan{}, context.Canceled
	})
	_ = f.b.Handle(
		t.Context(),
		message(updateID, identity.AliceTelegramID, "Change the prepared draft first name to fresh"),
	)
	require.True(t, prepared.Load(), "the actual choice preparer must finish its domain read")
	require.True(t, retired.Load(), "canonical source retirement must follow a successful admission authority request")
	require.True(t, observed, "the real dispatcher callback must finish")
	parentUpdate, parentRun, parentCall, err := agenthost.ParseModernChoiceRef(parentRef)
	require.NoError(t, err)
	var child string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE(content->$3::int->'calls'->$4::int->'modern_choice'->>'child','')
 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='script_runs'`, "alice", parentUpdate, parentRun, parentCall).Scan(&child),
	)
	require.Empty(t, child, "denied admission must not consume the existing parent")
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, updateID).
			Scan(&records),
	)
	require.Len(t, records, 1)
	require.Empty(t, records[0].Calls, "unaccepted intent must not enter the durable ledger")
}

func TestAgentHostCompletionRetirementSuppressesLiveResult(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	const canary = "completion-private-comment"
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice'`, canary)
	require.NoError(t, err)
	var readReady, retired atomic.Bool
	f.b.API.HTTP = &http.Client{
		Transport: retirementTransport{after: func(request *http.Request, response *http.Response) error {
			if request.URL.Path == "/v1/passes/events/dance/queue" && response.StatusCode == http.StatusOK {
				readReady.Store(true)
			}
			return nil
		}},
	}
	config := f.db.Config()
	config.ConnConfig.Tracer = retirementCompletionTracer{before: func(query pgx.TraceQueryStartData) {
		if !readReady.Load() || !strings.Contains(query.SQL, "SELECT content FROM bot.interactions") {
			return
		}
		for _, argument := range query.Args {
			if kind, ok := argument.(string); ok && kind == "script_runs" && retired.CompareAndSwap(false, true) {
				// The real tool has finished its domain read. Withdraw its grant
				// at the completion transaction, before its authorization check.
				_, revokeErr := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
				require.NoError(t, revokeErr)
			}
		}
	}}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f.b.DB = pool
	var observed bool
	var visible json.RawMessage
	f.b.Scripts = retirementObservedVM{observe: func(call scriptclient.ToolCall, result json.RawMessage, _ error) {
		if call.Name == "passes.admin.queue" {
			observed = true
			visible = append(json.RawMessage(nil), result...)
		}
	}}
	modelCalls := 0
	f.b.Model = avModel(func(_ context.Context, _ agent.Input) (agent.Plan, error) {
		modelCalls++
		if modelCalls == 1 {
			return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{
				Code: `return tools.passes.admin.queue({event:"dance"});`, InputJSON: "null",
			}}, nil
		}
		return agent.Plan{}, context.Canceled
	})
	_ = f.b.Handle(t.Context(), message(44991, 202, "Read the registration queue for dance"))
	require.True(t, retired.Load(), "the successful real domain read must precede grant withdrawal")
	require.True(t, observed, "the real worker callback must have completed")
	require.NotContains(t, string(visible), canary, "completion denial must suppress the pre-encoded live result")
	var durable json.RawMessage
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=44991 AND kind='script_runs'`).
			Scan(&durable),
	)
	require.NotContains(t, string(durable), canary)
}

func lateAdmissionTracer(prepared, admission *atomic.Bool, updateID int64) retirementCompletionTracer {
	return retirementCompletionTracer{before: func(query pgx.TraceQueryStartData) {
		if !prepared.Load() || !strings.Contains(query.SQL, "SELECT content FROM bot.interactions") {
			return
		}
		for _, argument := range query.Args {
			if value, ok := argument.(int64); ok && value == updateID {
				admission.Store(true)
			}
		}
	}}
}
