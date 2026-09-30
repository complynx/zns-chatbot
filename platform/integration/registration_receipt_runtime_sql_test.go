package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type r97AdmissionQueryKey struct{}

type r97RuntimeAdmissionTrace struct {
	started    atomic.Int32
	failed     atomic.Bool
	parentLive atomic.Bool
	failedAt   atomic.Int64
}

func (trace *r97RuntimeAdmissionTrace) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if !strings.HasPrefix(data.SQL, "SELECT request,source,created_at,retired FROM (") {
		return ctx
	}
	trace.started.Add(1)
	trace.parentLive.Store(ctx.Err() == nil)
	local, cancel := context.WithDeadline(ctx, time.Time{})
	cancel()
	return context.WithValue(local, r97AdmissionQueryKey{}, true)
}

func (trace *r97RuntimeAdmissionTrace) TraceQueryEnd(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData,
) {
	if target, _ := ctx.Value(r97AdmissionQueryKey{}).(bool); target && data.Err != nil {
		trace.failedAt.Store(time.Now().UnixNano())
		trace.failed.Store(true)
	}
}

type r97RuntimeVM struct {
	scopeVM

	trace         *r97RuntimeAdmissionTrace
	starts        atomic.Int32
	startsAfter   atomic.Int32
	operationRead atomic.Int32
	groundedRead  atomic.Int32
	laterSuccess  atomic.Int32
	lateProbe     atomic.Int32
	lateFenced    atomic.Bool
}

func (vm *r97RuntimeVM) Execute(
	ctx context.Context, request scriptclient.Request, tools []scriptclient.Tool, callback scriptclient.Callback,
) (json.RawMessage, error) {
	vm.starts.Add(1)
	if vm.trace.failed.Load() {
		vm.startsAfter.Add(1)
	}
	result, err := vm.scopeVM.Execute(
		ctx,
		request,
		tools,
		func(callCtx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			if call.Name == "passes.operations" {
				vm.operationRead.Add(1)
			}
			later := vm.trace.failed.Load()
			result, err := callback(callCtx, call)
			if call.Name == "passes.registration.read" && err == nil {
				vm.groundedRead.Add(1)
			}
			if later && err == nil {
				vm.laterSuccess.Add(1)
			}
			return result, err
		},
	)
	if vm.trace.failed.Load() {
		// Probe a late executor callback; Sobek itself stops on cancellation.
		vm.lateProbe.Add(1)
		late, lateErr := callback(context.WithoutCancel(ctx), scriptclient.ToolCall{
			Name: "passes.registration.cancel", Arguments: json.RawMessage(`{"event":"dance"}`),
		})
		vm.lateFenced.Store(core.IsDatabaseFailure(lateErr) && len(late) == 0)
	}
	return result, err
}

type r97RuntimePassTransport struct {
	trace *r97RuntimeAdmissionTrace
	total atomic.Int32
	after atomic.Int32
}

func (transport *r97RuntimePassTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost && request.URL.Path == "/internal/derived/pass-actions" {
		transport.total.Add(1)
		if transport.trace.failed.Load() {
			transport.after.Add(1)
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}

type r97RuntimeTelegram struct {
	trace          *r97RuntimeAdmissionTrace
	mutations      atomic.Int32
	mutationsAfter atomic.Int32
	allAfter       atomic.Int32
	mu             sync.Mutex
	observed       []r97TelegramObservation
}

type r97TelegramObservation struct {
	Method string
	At     time.Time
	After  bool
}

func (wire *r97RuntimeTelegram) RoundTrip(request *http.Request) (*http.Response, error) {
	observation := r97TelegramObservation{
		Method: path.Base(request.URL.Path), At: time.Now(), After: wire.trace.failed.Load(),
	}
	wire.mu.Lock()
	wire.observed = append(wire.observed, observation)
	wire.mu.Unlock()
	if observation.After {
		wire.allAfter.Add(1)
	}
	// Long polling belongs to Run's intake lifecycle, not a displayed tool result.
	if !strings.HasSuffix(request.URL.Path, "/getUpdates") {
		wire.mutations.Add(1)
		if observation.After {
			wire.mutationsAfter.Add(1)
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}

func (wire *r97RuntimeTelegram) requireStartupOnly(t *testing.T) {
	t.Helper()
	wire.mu.Lock()
	observed := append([]r97TelegramObservation(nil), wire.observed...)
	wire.mu.Unlock()
	failedAt := time.Unix(0, wire.trace.failedAt.Load())
	methods := make([]string, 0, len(observed))
	for _, item := range observed {
		t.Logf("Telegram method=%s relative_to_failed_SQL=%s after=%t", item.Method, item.At.Sub(failedAt), item.After)
		require.True(t, item.At.Before(failedAt), "all observed requests must start before the SQL failure")
		if item.Method != "getUpdates" {
			methods = append(methods, item.Method)
		}
	}
	// startTelegramPolling registers exactly these controls before fetching input.
	require.Equal(t, []string{"setChatMenuButton", "setMyCommands", "setMyCommands"}, methods)
	require.Zero(t, wire.allAfter.Load(), "no new Telegram request starts after failed SQL")
}

func TestRegistrationAdmissionsSQLFailureStopsOwningRun(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	trace := &r97RuntimeAdmissionTrace{}
	passTransport := &r97RuntimePassTransport{trace: trace}
	require.Nil(t, f.b.Host.LocalDerived, "this fixture executes derived registration through HTTP")
	f.b.Host.HTTP = &http.Client{Transport: passTransport}
	seed := runPassVM(t, f, 97900, 101, "Invite Telegram ID 202 to dance", `
await tools.passes.registration.read({event:"dance",view:"home"});
return tools.passes.registration.invite({event:"dance",invite_telegram_id:202});`)
	require.Empty(t, seed.Error)
	var committed struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(seed.Result, &committed))
	require.NotEmpty(t, committed.ID)
	require.True(t, committed.Complete)
	require.EqualValues(t, 1, passTransport.total.Load(), "the real seed calibrates the effect transport spy")
	drainPassNotices(t, f)
	stored := r97ReceiptSnapshot(t, f.db)
	seedAlice, seedBob := chatMessages(t, f, 101), chatMessages(t, f, 202)
	config := f.db.Config().Copy()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f.b.DB = pool
	vm := &r97RuntimeVM{trace: trace}
	f.b.Scripts = vm
	wire := &r97RuntimeTelegram{trace: trace}
	f.b.TG.HTTP = &http.Client{Transport: wire}
	var modelCalls, modelAfter atomic.Int32
	f.b.Model = avModel(func(context.Context, agent.Input) (agent.Plan, error) {
		modelCalls.Add(1)
		if trace.failed.Load() {
			modelAfter.Add(1)
		}
		return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{InputJSON: "null", Code: `
await tools.passes.registration.read({event:"dance",view:"home"});
try { await tools.passes.operations({}); } catch (_) {}
return await tools.passes.registration.cancel({event:"dance"});`}}, nil
	})
	failed := r97RuntimeInput(t, f, "Read my pass operation receipts")
	later := r97RuntimeInput(t, f, "Later same-chat request must wait")
	require.Greater(t, later.ID, failed.ID)
	beforeAlice, beforeBob := chatMessages(t, f, 101), chatMessages(t, f, 202)
	require.Len(t, beforeAlice, len(seedAlice)+2)
	require.Equal(t, seedAlice, beforeAlice[:len(seedAlice)], "posting input preserves all prior messages")
	require.Equal(t, seedBob, beforeBob)
	for index, update := range []telegram.Update{failed, later} {
		added := beforeAlice[len(seedAlice)+index]
		require.Equal(t, *update.Message, added)
		require.EqualValues(t, 101, added.From.ID)
		require.False(t, added.From.IsBot)
		require.EqualValues(t, 101, added.Chat.ID)
		require.Equal(t, update.Message.Text, added.Text)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = f.b.Run(ctx)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NoError(t, ctx.Err(), "the fatal owner must return with its caller still live")
	require.NotContains(t, err.Error(), "deadline")
	require.EqualValues(t, 1, trace.started.Load())
	require.True(t, trace.failed.Load(), "actual admissions SQL completed with a driver failure")
	require.True(t, trace.parentLive.Load())
	require.EqualValues(t, 1, modelCalls.Load())
	require.Zero(t, modelAfter.Load())
	require.EqualValues(t, 1, vm.starts.Load())
	require.Zero(t, vm.startsAfter.Load())
	require.EqualValues(t, 1, vm.operationRead.Load())
	require.EqualValues(t, 1, vm.groundedRead.Load(), "the later cancellation has a real completed registration read")
	require.Zero(t, vm.laterSuccess.Load(), "catching SQL cannot admit a later successful callback")
	require.EqualValues(t, 1, vm.lateProbe.Load(), "one explicit adversarial late callback reached the host fence")
	require.True(t, vm.lateFenced.Load(), "the late callback returns ErrDatabase without a result")
	require.Zero(t, passTransport.after.Load(), "no later registration effect request, including one that would fail")
	require.EqualValues(t, 1, passTransport.total.Load(), "only the seeded invitation reached the effect transport")
	wire.requireStartupOnly(t)
	require.Zero(t, wire.mutationsAfter.Load())
	require.EqualValues(
		t,
		3,
		wire.mutations.Load(),
		"only the exact startup controls are allowed; no status send or edit",
	)
	require.Equal(t, beforeAlice, chatMessages(t, f, 101))
	require.Equal(t, beforeBob, chatMessages(t, f, 202))
	require.Equal(t, stored, r97ReceiptSnapshot(t, f.db), "the real committed invitation remains unchanged")
	require.Equal(t, []telegram.Update{failed, later}, retryInboxPayloads(t, f),
		"failed head and same-chat successor remain durable with their full original payloads in FIFO order")
	for _, update := range []telegram.Update{failed, later} {
		var state string
		var failures int
		var chat int64
		require.NoError(t, f.db.QueryRow(t.Context(),
			`SELECT state,failures,chat_key FROM bot.telegram_inbox WHERE update_id=$1`, update.ID,
		).Scan(&state, &failures, &chat))
		t.Logf("durable update_id=%d chat=%d state=%s failures=%d", update.ID, chat, state, failures)
		require.Equal(t, "pending", state)
		require.Zero(t, failures, "SQL failure and the unprocessed successor consume no poison budget")
		require.EqualValues(t, 101, chat)
	}
}

func r97RuntimeInput(t *testing.T, f *fixture, text string) telegram.Update {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"user": 101, "text": text})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		f.fake.URL+"/lab/input", bytes.NewReader(raw))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Sandbox", "1")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	require.Positive(t, update.ID)
	require.NotNil(t, update.Message)
	require.Equal(t, text, update.Message.Text)
	t.Logf("lab input update_id=%d message_id=%d text=%q", update.ID, update.Message.ID, text)
	return update
}
