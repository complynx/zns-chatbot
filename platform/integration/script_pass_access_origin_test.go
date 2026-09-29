package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestScriptPassArchivedGetReopenedAfterRevocation(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment='private-reopened-canary' WHERE event_id='archive'`,
	)
	require.NoError(t, err)
	update := pausePassDiscovery(t, f, `return tools.passes.get({event:"archive"});`)
	removeArchivedBooking(t, f)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'`,
	)
	require.NoError(t, err)
	input := retryPassDiscovery(t, f, update)
	raw, err := json.Marshal(input.Script)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-reopened-canary")
}

func TestScriptPassBookingOriginAuthority(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, code                string
		current, recreate, legacy bool
	}{
		{name: "current get", code: `return tools.passes.get({event:"archive"});`, current: true},
		{name: "current registration", code: `return tools.passes.registration.read({event:"archive",view:"home"});`, current: true},
		{name: "historical registration reopened", code: `return tools.passes.registration.read({event:"archive",view:"home"});`},
		{name: "same version get replacement", code: `return tools.passes.get({event:"archive"});`, recreate: true},
		{name: "same version registration replacement", code: `return tools.passes.registration.read({event:"archive",view:"home"});`, recreate: true},
		{name: "legacy registration without historical flag", code: `return tools.passes.registration.read({event:"archive",view:"home"});`, legacy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := archivedPassFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET comment='private-origin-canary' WHERE event_id='archive'`,
			)
			require.NoError(t, err)
			if test.current {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'`,
				)
				require.NoError(t, err)
			}
			update := pausePassDiscovery(t, f, `tools.preferences.setLanguage({language:"ru"}); `+test.code)
			if test.legacy {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE bot.interactions SET content=content #- '{0,calls,1,outcome,result,historical}' WHERE owner='alice' AND update_id=29991 AND kind='script_runs';
UPDATE bot.interactions SET content=content #- '{0,historical}' WHERE owner='alice' AND update_id=29991 AND kind='registration_reads'`,
				)
				require.NoError(t, err)
			}
			removeArchivedBooking(t, f)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'`,
			)
			require.NoError(t, err)
			if test.recreate {
				_, err = f.db.Exec(
					t.Context(),
					`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,comment)
VALUES('archive','alice',1,'assigned','leader','guest','bob',now(),now(),100,'replacement')`,
				)
				require.NoError(t, err)
			}
			input := retryPassDiscovery(t, f, update)
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "private-origin-canary")
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
			require.Equal(t, 1, input.Script.Remaining)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).
					Scan(&count),
			)
			require.Equal(t, 1, count)
		})
	}
}

func TestScriptPassBookingLegacyIdentityFailsClosed(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	update := pausePassDiscovery(t, f, `return tools.passes.get({event:"archive"});`)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE bot.interactions SET content=content #- '{0,calls,0,outcome,result,created_at}' WHERE owner='alice' AND update_id=29991 AND kind='script_runs'`,
	)
	require.NoError(t, err)
	removeLegacyPassToolAuthority(t, f, "alice", 29991, 0)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
}

type passBookingAuthorityTransport struct {
	fail atomic.Bool
	hits atomic.Int64
}

func (transport *passBookingAuthorityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if (request.URL.Path == "/v1/passes/events/archive/me" || request.Method == http.MethodPost && request.URL.Path == "/internal/history/authority") &&
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

func TestScriptPassBookingOriginOutage(t *testing.T) {
	t.Parallel()
	f := archivedPassFixture(t)
	update := pausePassDiscovery(t, f, `return tools.passes.get({event:"archive"});`)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'`,
	)
	require.NoError(t, err)
	transport := &passBookingAuthorityTransport{}
	transport.fail.Store(true)
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Positive(t, transport.hits.Load())
	require.Empty(t, model.inputs)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=29991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, `"pass_redacted": true`)
	transport.fail.Store(false)
	input := retryPassDiscovery(t, f, update)
	require.NotContains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
	var booking passbooking.Booking
	require.NoError(t, json.Unmarshal(input.Script.Runs[0].Result, &booking))
	require.Equal(t, "alice", booking.Owner)
}
