package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func pausePassDiscovery(t *testing.T, f *fixture, code string) telegram.Update {
	t.Helper()
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}}, nil
		}
		require.Empty(t, input.Script.Runs[0].Error)
		return agent.Plan{}, context.Canceled
	})
	update := message(29991, 101, "Read my event information")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	return update
}

func retryPassDiscovery(t *testing.T, f *fixture, update telegram.Update) agent.Input {
	t.Helper()
	var input agent.Input
	f.b.Model = avModel(func(_ context.Context, current agent.Input) (agent.Plan, error) {
		input = current
		return agent.Plan{}, context.Canceled
	})
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	require.NotNil(t, input.Script)
	return input
}

func removeArchivedBooking(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `DELETE FROM core.legacy_pass_payment_metadata WHERE event_id='archive';
 DELETE FROM core.pass_bookings WHERE event_id='archive' AND owner='alice'`)
	require.NoError(t, err)
}

func TestScriptPassDiscoveryPublicMetadataIndependent(t *testing.T) {
	t.Parallel()
	t.Run("explicit historical detail", func(t *testing.T) {
		t.Parallel()
		f := archivedPassFixture(t)
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.pass_events SET titles='{"en":"public-detail-canary"}' WHERE id='archive'`,
		)
		require.NoError(t, err)
		update := pausePassDiscovery(t, f, `return (await tools.passes.event.read({event:"archive"})).json;`)
		removeArchivedBooking(t, f)
		input := retryPassDiscovery(t, f, update)
		require.Contains(t, string(input.Script.Runs[0].Result), "public-detail-canary")
	})
	t.Run("public discovery stays public after expiry", func(t *testing.T) {
		t.Parallel()
		f := archivedPassFixture(t)
		removeArchivedBooking(t, f)
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.pass_events SET titles='{"en":"public-page-canary"}' WHERE id='dance'`,
		)
		require.NoError(t, err)
		update := pausePassDiscovery(t, f, `return tools.passes.events({});`)
		_, err = f.db.Exec(t.Context(), `UPDATE core.pass_events SET finishes_at='2025-12-01' WHERE id='dance'`)
		require.NoError(t, err)
		input := retryPassDiscovery(t, f, update)
		require.Contains(t, string(input.Script.Runs[0].Result), "public-page-canary")
		require.NotContains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
	})
}

func TestScriptPassDiscoveryLegacyReceiptFailsClosedWithoutReplay(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	update := pausePassDiscovery(
		t,
		f,
		`tools.preferences.setLanguage({language:"ru"}); return tools.passes.events({});`,
	)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE bot.interactions SET content=jsonb_set(content,'{0,calls,1,outcome,result,items}',
 (SELECT jsonb_agg(item-'access') FROM jsonb_array_elements(content#>'{0,calls,1,outcome,result,items}') item))
 WHERE owner='alice' AND update_id=29991 AND kind='script_runs'`,
	)
	require.NoError(t, err)
	removeLegacyPassToolAuthority(t, f, "alice", 29991, 1)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
	require.Equal(t, 1, input.Script.Remaining)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).Scan(&count),
	)
	require.Equal(t, 1, count)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=29991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.Contains(t, stored, `"language": {`)
	require.Contains(t, stored, `"pass_redacted": true`)
}

type passMembershipTransport struct {
	fail atomic.Bool
	hits atomic.Int64
}

func (transport *passMembershipTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if (request.URL.Path == "/v1/passes/bookings/owned" || request.Method == http.MethodPost && request.URL.Path == "/internal/history/authority") &&
		transport.fail.Load() {
		transport.hits.Add(1)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":"synthetic_unavailable"}`)),
			Request:    request,
		}, nil
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestScriptPassDiscoveryOutageDoesNotRevoke(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	update := pausePassDiscovery(t, f, `return tools.passes.events({});`)
	transport := &passMembershipTransport{}
	transport.fail.Store(true)
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Positive(t, transport.hits.Load())
	require.Empty(t, model.inputs, "outage must not expose unvalidated cached results")
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=29991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, `"pass_redacted": true`)
	transport.fail.Store(false)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "Past dance")
	removeArchivedBooking(t, f)
	input = retryPassDiscovery(t, f, update)
	require.NotContains(t, string(input.Script.Runs[0].Result), "Past dance")
	require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
}

func TestScriptPassDiscoveryLaterPageLateRevocation(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at,titles)
 SELECT 'history-'||lpad(n::text,2,'0'),'2025-12-01',jsonb_build_object('en','later-page-'||n) FROM generate_series(1,45) n;
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 SELECT 'history-'||lpad(n::text,2,'0'),'alice',1,'cancelled','leader','solo','bob','2025-09-01' FROM generate_series(1,45) n;`)
	require.NoError(t, err)
	f.b.Scripts = archivedPassLateVM{after: func() {
		var pages int
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions,
 jsonb_array_elements(content->0->'calls') call WHERE owner='alice' AND update_id=29992 AND kind='script_runs'
 AND call->'outcome'->>'name'='passes.events'`).Scan(&pages))
		require.Equal(t, 3, pages)
		_, removeErr := f.db.Exec(
			t.Context(),
			`DELETE FROM core.pass_bookings WHERE event_id='history-45' AND owner='alice'`,
		)
		require.NoError(t, removeErr)
	}}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: `let cursor=""; const titles=[];
 do {const page=await tools.passes.events({cursor}); titles.push(...page.items.map(e=>e.titles)); cursor=page.next_cursor;} while(cursor);
 return titles;`, InputJSON: "null"}},
		{View: "workflow", Text: "Checked"},
	}}
	f.b.Model = model
	handle(t, f.b, message(29992, 101, "List my historical events"))
	require.Len(t, model.inputs, 2)
	raw, err := json.Marshal(model.inputs[1].Script)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "later-page-")
	require.Contains(t, string(raw), "pass_access_changed")
}
