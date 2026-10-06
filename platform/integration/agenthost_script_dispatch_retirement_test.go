package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
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

type completionRetirementScenario struct {
	t               *testing.T
	f               *fixture
	scenario        string
	updateID        int64
	cancel          context.CancelFunc
	calls           atomic.Int32
	secondRead      atomic.Bool
	completionStage atomic.Bool
	retired         atomic.Bool
	denied          atomic.Bool
	faulted         atomic.Bool
	outage          error
	first           []agenthost.ScriptRecord
	live            json.RawMessage
	completionErr   error
	modelCalls      int
}

func (s *completionRetirementScenario) after(request *http.Request, response *http.Response) error {
	if s.calls.Load() == 2 && request.URL.Path == "/v1/passes/events/dance/admin/targets/101" &&
		response.StatusCode == http.StatusOK {
		s.secondRead.Store(true)
	}
	if request.URL.Path != "/internal/history/authority" || !s.retired.Load() {
		return nil
	}
	if s.denied.Load() && s.faulted.CompareAndSwap(false, true) {
		if s.scenario == "cancellation" {
			s.cancel()
			return context.Canceled
		}
		return s.outage
	}
	if response.StatusCode == http.StatusConflict {
		s.denied.Store(true)
	}
	return nil
}

func (s *completionRetirementScenario) beforeCompletion(query pgx.TraceQueryStartData) {
	if !s.completionStage.Load() || !s.secondRead.Load() ||
		!strings.Contains(query.SQL, "SELECT content FROM bot.interactions") {
		return
	}
	for _, argument := range query.Args {
		if kind, ok := argument.(string); ok && kind == "script_runs" && s.retired.CompareAndSwap(false, true) {
			_, err := s.f.db.Exec(s.t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
			require.NoError(s.t, err)
		}
	}
}

// Arm retirement only inside the real second call's durable completion span.
func (s *completionRetirementScenario) Write(data []byte) (int, error) {
	var logRecord struct {
		Event string `json:"agent_event"`
	}
	if err := json.Unmarshal(data, &logRecord); err != nil {
		return 0, fmt.Errorf("decode diagnostic log record: %w", err)
	}
	if logRecord.Event == "" {
		return len(data), nil
	}
	var event struct {
		Operation string `json:"operation"`
		Outcome   string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(logRecord.Event), &event); err != nil {
		return 0, fmt.Errorf("decode completion event: %w", err)
	}
	if s.t != nil && s.calls.Load() == 2 {
		s.t.Logf("second callback phase operation=%s outcome=%s", event.Operation, event.Outcome)
	}
	if s.calls.Load() == 2 &&
		event.Operation == "script.call.complete" && event.Outcome == "started" {
		s.completionStage.Store(true)
	}
	return len(data), nil
}

func TestCompletionRetirementObserverRequiresSecondCompletion(t *testing.T) {
	t.Parallel()
	s := &completionRetirementScenario{}
	n, err := s.Write([]byte(`{"msg":"unrelated"}`))
	require.NoError(t, err)
	require.Positive(t, n)
	_, err = s.Write([]byte(`{"agent_event":`))
	require.Error(t, err)
	_, err = s.Write([]byte(`{"agent_event":"not-json"}`))
	require.Error(t, err)
	recorder, err := observability.NewAgentEvents(slog.New(slog.NewJSONHandler(s, nil)),
		"8d17ab59-dc35-42ac-a1cb-dcb9f09bfeee", 1)
	require.NoError(t, err)
	ctx := observability.WithAgentEvents(t.Context(), recorder)
	s.calls.Store(1)
	_, span := observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: "script", Operation: "script.call.complete"})
	span.Finish(nil)
	require.False(t, s.completionStage.Load())
	s.calls.Store(2)
	observability.EmitAgentEvent(ctx,
		observability.AgentEvent{Phase: "script", Operation: "script.call.complete", Outcome: "ok"})
	_, span = observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: "script", Operation: "script.call.admit"})
	span.Finish(nil)
	require.False(t, s.completionStage.Load())
	_, span = observability.StartAgentEvent(ctx,
		observability.AgentEvent{Phase: "script", Operation: "script.call.complete"})
	require.True(t, s.completionStage.Load())
	span.Finish(nil)
}

func (s *completionRetirementScenario) before(call scriptclient.ToolCall) {
	if call.Name == "passes.admin.queue" || call.Name == "passes.admin.target" {
		s.calls.Add(1)
	}
}

func (s *completionRetirementScenario) observe(call scriptclient.ToolCall, result json.RawMessage, callbackErr error) {
	if call.Name != "passes.admin.queue" && call.Name != "passes.admin.target" {
		return
	}
	if s.calls.Load() == 1 {
		require.NoError(s.t, s.f.db.QueryRow(s.t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, s.updateID).Scan(&s.first))
		return
	}
	s.live = append(json.RawMessage(nil), result...)
	s.completionErr = callbackErr
	s.t.Logf("second callback failed=%t canceled=%t outage=%t result_present=%t",
		callbackErr != nil, errors.Is(callbackErr, context.Canceled), errors.Is(callbackErr, s.outage),
		len(result) != 0)
}

func (s *completionRetirementScenario) plan(_ context.Context, _ agent.Input) (agent.Plan, error) {
	s.modelCalls++
	if s.modelCalls <= 2 {
		code := `return tools.passes.admin.queue({event:"dance"});`
		if s.modelCalls == 2 {
			code = `return tools.passes.admin.target({event:"dance",target:"101"});`
		}
		return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{
			Code: code, InputJSON: "null",
		}}, nil
	}
	return agent.Plan{}, context.Canceled
}

func TestAgentHostCompletionRetirementSurvivesLaterAuthorityFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"outage", "cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			const updateID int64 = 44992
			const canary = "mixed-completion-private-comment"
			_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice'`, canary)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := &completionRetirementScenario{t: t, f: f, scenario: scenario, updateID: updateID,
				cancel: cancel, outage: core.ErrDatabase}
			recorder, err := observability.NewAgentEvents(slog.New(slog.NewJSONHandler(s, nil)),
				"8d17ab59-dc35-42ac-a1cb-dcb9f09bfeee", 1)
			require.NoError(t, err)
			// Keep this required observer independent of best-effort SQL diagnostics.
			f.b.Logger = nil
			ctx = observability.WithAgentEvents(ctx, recorder)
			client := &http.Client{
				Transport: retirementTransport{after: s.after},
			}
			f.b.API.HTTP, f.b.Host.HTTP = client, client
			config := f.db.Config()
			config.ConnConfig.Tracer = retirementCompletionTracer{before: s.beforeCompletion}
			pool, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			f.b.DB = pool
			f.b.Scripts = retirementObservedVM{before: s.before, observe: s.observe}
			f.b.Model = avModel(s.plan)
			_ = f.b.Handle(
				ctx,
				message(updateID, 202, "Read the registration queue for dance, then inspect Telegram ID 101 for dance"),
			)
			require.EqualValues(t, 2, s.calls.Load())
			require.True(t, s.secondRead.Load(), "the second dispatcher call must finish a real domain read")
			require.True(t, s.completionStage.Load(), "retirement must follow the real second call completion span")
			require.True(t, s.retired.Load())
			require.True(t, s.denied.Load(), "real domain retirement must precede the injected fault")
			require.True(t, s.faulted.Load(), "the later authority request must fail")
			require.Error(t, s.completionErr)
			if scenario == "cancellation" {
				require.ErrorIs(t, s.completionErr, context.Canceled)
			} else {
				require.ErrorIs(t, s.completionErr, s.outage)
			}
			require.NotContains(t, string(s.live), canary)
			var records []agenthost.ScriptRecord
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, updateID).Scan(&records))
			require.Len(t, records, 2)
			for _, record := range records {
				require.True(t, record.PassRedacted)
			}
			durable, err := json.Marshal(records)
			require.NoError(t, err)
			require.NotContains(t, string(durable), canary, "scrub must commit before grant restoration")
			_, err = f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
			require.NoError(t, err)
			f.b.Host.HTTP = http.DefaultClient
			refs, err := agenthost.ScriptReadAuthorities("bob", s.first)
			require.NoError(t, err)
			require.NotEmpty(t, refs)
			changed, err := agenthost.ReadAuthoritiesChanged(t.Context(), f.b.Host, "bob", refs)
			require.NoError(t, err)
			require.False(t, changed, "the restored real grant must authorize the original sources")
			store := agenthost.ScriptStore{DB: f.db, Policy: liveLedgerAuthority{fixture: f}}
			retried, err := store.LoadAuthorized(t.Context(), "bob", updateID)
			require.NoError(t, err)
			require.Len(t, retried, 2)
			for _, record := range retried {
				require.True(t, record.PassRedacted)
			}
			retained, err := json.Marshal(retried)
			require.NoError(t, err)
			require.NotContains(t, string(retained), canary)
		})
	}
}
