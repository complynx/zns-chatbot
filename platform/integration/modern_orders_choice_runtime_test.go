package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestModernChoiceFullRuntimeAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, stem := range []string{"a", "Ж", "<&\\\""} {
		t.Run(fmt.Sprintf("%x", stem), func(t *testing.T) {
			t.Parallel()
			worker := startModernRuntimeWorker(t)
			f, original := modernSingleKeyOrder(t, stem)
			f.b.Model = modernRuntimeDiagnosticModel(t, f.fake.URL)
			f.b.Scripts = worker
			f.b.WebAppURL = "https://sandbox.invalid/orders"
			more := true
			for turn := 0; more && turn < 24; turn++ {
				// Reconstruct the host each turn. Only durable receipts can continue reads.
				f.b = &bot.Bot{
					DB:        f.db,
					Delivery:  f.b.Delivery,
					API:       f.b.API,
					Host:      f.b.Host,
					TG:        f.b.TG,
					Model:     f.b.Model,
					Scripts:   worker,
					WebAppURL: f.b.WebAppURL,
				}
				t.Logf("phase=inspect turn=%d resume=%t", turn, turn > 0)
				result := modernRuntimeScript(t, f, "Inspect order "+original.ID, fmt.Sprintf(
					`let p=tools.orders.inspect({order_id:%q,resume:%t});for(let i=1;i<8&&p.more;i++)p=tools.orders.inspect({order_id:%q,cursor:p.next_cursor});return {more:p.more};`,
					original.ID,
					turn > 0,
					original.ID,
				))
				var state struct {
					More bool `json:"more"`
				}
				require.NoError(t, json.Unmarshal(result, &state))
				more = state.More
			}
			require.False(t, more)
			result := modernRuntimeScript(t, f, "Change only the name of order "+original.ID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q,resume:true});let d=tools.orders.choice({operation:"begin",order_id:%q});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:"New name"});return {ref:d.choice_ref};`,
				original.ID,
				original.ID,
			))
			var draft struct {
				Ref string `json:"ref"`
			}
			require.NoError(t, json.Unmarshal(result, &draft))
			f.b = &bot.Bot{
				DB:        f.db,
				Delivery:  f.b.Delivery,
				API:       f.b.API,
				Host:      f.b.Host,
				TG:        f.b.TG,
				Model:     f.b.Model,
				Scripts:   worker,
				WebAppURL: f.b.WebAppURL,
			}
			result = modernRuntimeScript(t, f, "Apply the name edit to order "+original.ID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q,resume:true});return tools.orders.update({name:"edit",order_id:%q,choice_ref:%q});`,
				original.ID,
				original.ID,
				draft.Ref,
			))
			t.Logf("name edit through external worker: %s", result)
			service := orders.Service{DB: f.db}
			edited, err := service.Get(t.Context(), "alice", original.EventID, original.ID)
			require.NoError(t, err)
			assert.Equal(t, "New name", edited.Choice.Customer)
			assert.Equal(t, original.Choice.Extras, edited.Choice.Extras)
			restartModernRuntimeStand(t, f)
			result = modernRuntimeScript(t, f, "Create the complete selected order", fmt.Sprintf(
				`let d=tools.orders.choice({operation:"begin",empty:true});const refs=tools.orders.choice({operation:"read",choice_ref:d.choice_ref,part:"catalog"});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:%q,extras:[{ref:0,selected:true}]});return {ref:d.choice_ref,bytes:d.choice_bytes};`,
				original.Choice.Customer,
			))
			var create struct {
				Ref   string `json:"ref"`
				Bytes int    `json:"bytes"`
			}
			require.NoError(t, json.Unmarshal(result, &create))
			assert.Equal(t, 256<<10, create.Bytes)
			quoteCursor := ""
			more = true
			for turn := 0; more && turn < 24; turn++ {
				result = modernRuntimeScript(t, f, "Quote the complete selected choice", fmt.Sprintf(
					`let p=tools.orders.quote({choice_ref:%q,cursor:%q});for(let i=1;i<8&&p.more;i++)p=tools.orders.quote({choice_ref:%q,cursor:p.next_cursor});return {more:p.more,cursor:p.next_cursor||""};`,
					create.Ref,
					quoteCursor,
					create.Ref,
				))
				var page struct {
					More   bool   `json:"more"`
					Cursor string `json:"cursor"`
				}
				require.NoError(t, json.Unmarshal(result, &page))
				more, quoteCursor = page.More, page.Cursor
			}
			require.False(t, more)
			result = modernRuntimeScript(
				t,
				f,
				"Create that exact full order",
				fmt.Sprintf(`return tools.orders.update({name:"create",choice_ref:%q});`, create.Ref),
			)
			var created struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(result, &created))
			full, err := service.Get(t.Context(), "alice", original.EventID, created.ID)
			require.NoError(t, err)
			assert.Equal(t, original.Choice, full.Choice)
			restartModernRuntimeStand(t, f)
			// A full replacement starts empty and uses compact refs for even a huge key.
			// The updated original requires a new complete observation first.
			more = true
			for turn := 0; more && turn < 24; turn++ {
				result = modernRuntimeScript(t, f, "Inspect order "+original.ID, fmt.Sprintf(
					`let p=tools.orders.inspect({order_id:%q,resume:%t});for(let i=1;i<8&&p.more;i++)p=tools.orders.inspect({order_id:%q,cursor:p.next_cursor});return {more:p.more};`,
					original.ID,
					turn > 0,
					original.ID,
				))
				var state struct {
					More bool `json:"more"`
				}
				require.NoError(t, json.Unmarshal(result, &state))
				more = state.More
			}
			require.False(t, more)
			result = modernRuntimeScript(t, f, "Replace the complete choice of order "+original.ID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q,resume:true});let d=tools.orders.choice({operation:"begin",order_id:%q,empty:true});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:%q,extras:[{ref:0,selected:true}]});return tools.orders.update({name:"edit",order_id:%q,choice_ref:d.choice_ref});`,
				original.ID,
				original.ID,
				original.Choice.Customer,
				original.ID,
			))
			t.Logf("full replacement through external worker: %s", result)
			restored, err := service.Get(t.Context(), "alice", original.EventID, original.ID)
			require.NoError(t, err)
			assert.Equal(t, original.Choice, restored.Choice)
		})
	}
}

func TestModernRuntimeDurableInboxCompletion(t *testing.T) {
	t.Parallel()
	worker := startModernRuntimeWorker(t)
	f, order := modernSingleKeyOrder(t, "a")
	f.b.Scripts = worker
	f.b.WebAppURL = "https://sandbox.invalid/orders"
	result := modernRuntimeScript(t, f, "Inspect order "+order.ID, fmt.Sprintf(
		`let p=tools.orders.inspect({order_id:%q});return {more:p.more};`, order.ID,
	))
	var page struct {
		More bool `json:"more"`
	}
	require.NoError(t, json.Unmarshal(result, &page))
	require.True(t, page.More)
	current, err := (orders.Service{DB: f.db}).Get(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, order.Choice, current.Choice)
}

func restartModernRuntimeStand(t *testing.T, f *fixture) {
	t.Helper()
	stand, err := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, err)
	server := httptest.NewServer(stand.Handler())
	t.Cleanup(server.Close)
	f.fake = server
	f.b.Model = modernRuntimeDiagnosticModel(t, server.URL)
	f.b.TG.Base = server.URL
}

func modernRuntimeDiagnosticModel(t *testing.T, address string) sandbox.FixtureRemote {
	t.Helper()
	return sandbox.FixtureRemote{
		URL:  address + "/lab/model",
		HTTP: &http.Client{Timeout: 10 * time.Second, Transport: modernRuntimeModelTransport{t: t}},
	}
}

type modernRuntimeModelTransport struct{ t *testing.T }

func (transport modernRuntimeModelTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	response, err := http.DefaultTransport.RoundTrip(request)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	transport.t.Logf("runtime model HTTP update=%s turn=%s path=%s bytes=%d status=%d transport_error=%t elapsed=%s",
		request.Header.Get("X-Sandbox-Update"), request.Header.Get("X-Sandbox-Turn"), request.URL.Path,
		request.ContentLength, status, err != nil, time.Since(started))
	return response, err
}

func modernRuntimeScript(t *testing.T, f *fixture, text, code string) json.RawMessage {
	t.Helper()
	// The long scenario exceeds the provider's retained 32-step capacity. Each
	// completed interaction owns a fresh bounded model fixture, not a new Telegram stand.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	model, err := sandbox.New(ctx, nil, "sandbox")
	require.NoError(t, err)
	server := httptest.NewServer(model.Handler())
	defer server.Close()
	input, err := json.Marshal(map[string]any{"user": 101, "language_code": "en", "text": text})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, f.fake.URL+"/lab/input", bytes.NewReader(input),
	)
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	var queued struct {
		ID int64 `json:"update_id"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&queued)
	closeErr := response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, decodeErr)
	require.NoError(t, closeErr)
	require.Positive(t, queued.ID)
	update := queued.ID
	fixtureBody, err := json.Marshal(map[string]any{
		"owner": "alice", "update_id": update,
		"steps": []map[string]any{
			{"expect": map[string]any{"text": text}, "plan": agent.Plan{
				View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"},
			}},
			{"expect": map[string]any{"text": text}, "plan": agent.Plan{
				View: agent.OrdersView, Text: "Order operation recorded.",
			}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, update, installModernRuntimeFixture(t, server.URL, fixtureBody))
	f.b.Model = modernRuntimeDiagnosticModel(t, server.URL)
	modelFixture := *f
	modelFixture.fake = server
	completeModernRuntimeInbox(t, &modelFixture, update)
	assertModernRuntimeFixtureConsumed(t, &modelFixture, update, 2)
	var records []struct {
		Run agent.ScriptRun `json:"run"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	require.Len(t, records, 1)
	require.Empty(t, records[0].Run.Error)
	return records[0].Run.Result
}

func modernSingleKeyOrder(t *testing.T, stem string) (*fixture, orders.Order) {
	t.Helper()
	f := setup(t)
	menu, err := orders.Menu()
	require.NoError(t, err)
	low, high := 1, 256<<10
	for low < high {
		middle := (low + high + 1) / 2
		key := strings.Repeat(stem, middle)
		value, quoteErr := orders.Canonicalize(
			orders.ChoiceInput{Extras: map[string]json.RawMessage{key: json.RawMessage(`0`)}},
			menu,
			map[string]orders.Extra{key: {Price: 100}},
		)
		encoded, _ := json.Marshal(value)
		if quoteErr == nil && len(encoded) <= (256<<10)-512 {
			low = middle
		} else {
			high = middle - 1
		}
	}
	key := strings.Repeat(stem, low)
	extras := map[string]orders.Extra{key: {Price: 100}}
	choice := orders.ChoiceInput{Extras: map[string]json.RawMessage{key: json.RawMessage(`0`)}}
	canonical, err := orders.Canonicalize(choice, menu, extras)
	require.NoError(t, err)
	encoded, err := json.Marshal(canonical)
	require.NoError(t, err)
	choice.Customer = strings.Repeat("x", (256<<10)-len(encoded)-len(`"customer":"",`))
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET extras=$1 WHERE id='sandbox-festival'`, extras)
	require.NoError(t, err)
	order, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "boundary-create",
			Choice:  &choice,
		},
	)
	require.NoError(t, err)
	encoded, err = json.Marshal(order.Choice)
	require.NoError(t, err)
	require.Len(t, encoded, 256<<10)
	return f, order
}

// Wait for durable completion within the enclosing scenario deadline and retain
// progress before cancellation erases a slow operation, retry loop or lock wait.
func completeModernRuntimeInbox(t *testing.T, f *fixture, update int64) {
	t.Helper()
	timings := &modernRuntimeTimings{}
	previousLogger, previousHTTP, previousHostHTTP := f.b.Logger, f.b.API.HTTP, f.b.Host.HTTP
	f.b.API.HTTP = modernRuntimeTimedClient(previousHTTP, timings, "user")
	f.b.Host.HTTP = modernRuntimeTimedClient(previousHostHTTP, timings, "host")
	f.b.Logger = observability.NewLogger(timings, observability.LogConfig{})
	started := time.Now()
	timings.started = started
	deadline, bounded := t.Deadline()
	require.True(t, bounded, "runtime completion requires the enclosing test deadline")
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	defer func() {
		cancel()
		runErr := <-done
		f.b.Logger, f.b.API.HTTP = previousLogger, previousHTTP
		f.b.Host.HTTP = previousHostHTTP
		timings.report(t)
		require.NoError(t, runErr)
	}()
	ready := func() bool {
		var cursor int64
		var count int
		err := f.db.QueryRow(t.Context(), `SELECT value,(SELECT count(*) FROM bot.telegram_inbox) FROM bot.cursors WHERE name='telegram'`).
			Scan(&cursor, &count)
		return err == nil && cursor == update+1 && count == 0
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ctx.Done():
			modernRuntimeDiagnostics(t, f, update)
			require.FailNow(t, "durable runtime completion did not finish", "%v", ctx.Err())
		case <-ticker.C:
		}
	}
	t.Logf("runtime update=%d complete elapsed=%s", update, time.Since(started))
}

func modernRuntimeDiagnostics(t *testing.T, f *fixture, update int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
	defer cancel()
	queries := map[string]string{
		"stages":  `SELECT COALESCE(jsonb_agg(jsonb_build_object('kind',kind,'bytes',octet_length(content::text)) ORDER BY id),'[]'::jsonb) FROM bot.interactions WHERE owner='alice' AND update_id=$1`,
		"durable": `SELECT jsonb_build_object('cursors',(SELECT jsonb_object_agg(name,value) FROM bot.cursors),'inbox',(SELECT count(*) FROM bot.telegram_inbox),'turn',(SELECT jsonb_build_object('kind',kind,'state',state,'reason',reason,'generation',history_generation,'system_notice',payload->>'system_notice') FROM interaction.saved_turns WHERE owner='alice' AND update_id=$1),'quota',(SELECT jsonb_build_object('allowed',allowed,'remaining',remaining) FROM bot.agent_quota WHERE owner='alice' AND update_id=$1))`,
		"calls":   `SELECT COALESCE(jsonb_agg(item),'[]'::jsonb) FROM (SELECT jsonb_build_object('run',r.position,'call',c.position,'run_error',r.value->'run'->>'error','name',c.value->'outcome'->>'name','error',c.value->'outcome'->>'error','result_bytes',octet_length((c.value->'outcome'->'result')::text),'offset',c.value->'outcome'->'result'->'offset','more',c.value->'outcome'->'result'->'more','source_generation',c.value->'source'->'generation') AS item FROM bot.interactions i CROSS JOIN LATERAL jsonb_array_elements(i.content) WITH ORDINALITY r(value,position) CROSS JOIN LATERAL jsonb_array_elements(COALESCE(r.value->'calls','[]'::jsonb)) WITH ORDINALITY c(value,position) WHERE i.owner='alice' AND i.update_id=$1 AND i.kind='script_runs' ORDER BY r.position,c.position LIMIT 16) bounded`,
		"waits":   `SELECT COALESCE(jsonb_agg(jsonb_build_object('pid',pid,'state',state,'wait_type',wait_event_type,'wait_event',wait_event,'blockers',pg_blocking_pids(pid))),'[]'::jsonb) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND $1::bigint>0`,
	}
	for name, query := range queries {
		var body json.RawMessage
		err := f.db.QueryRow(ctx, query, update).Scan(&body)
		t.Logf("runtime diagnostic update=%d %s=%s error=%v", update, name, body, err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/lab/model/state?owner=alice&update_id=%d", f.fake.URL, update),
		http.NoBody,
	)
	if err == nil {
		request.Header.Set("X-Sandbox", "1")
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr == nil {
			defer response.Body.Close()
			var wire struct {
				Accepted int `json:"accepted"`
				Rejected int `json:"rejected"`
				NextTurn int `json:"next_turn"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&wire)
			t.Logf("runtime fixture update=%d state=%+v error=%v", update, wire, decodeErr)
		} else {
			t.Logf("runtime fixture diagnostic: %v", requestErr)
		}
	}
	stack := make([]byte, 4<<20)
	size := runtime.Stack(stack, true)
	for trace := range strings.SplitSeq(string(stack[:size]), "\n\n") {
		if strings.Contains(trace, "/internal/bot.") || strings.Contains(trace, "/internal/interaction.") ||
			strings.Contains(trace, "/internal/telegram.") {
			t.Logf("runtime targeted stack (capture=%d/%d):\n%s", size, len(stack), trace)
		}
	}
}
