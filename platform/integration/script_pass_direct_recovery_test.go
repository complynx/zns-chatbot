package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const directPassCanary = "private-direct-context-canary"

func directPassModel(t *testing.T, code string, finish func(agent.Input) (agent.Plan, error)) agent.Model {
	t.Helper()
	calls := 0
	return avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return agent.Plan{
				View: agent.RegistrationView,
				RegistrationAction: &agent.RegistrationProposal{
					Name:  agent.RegistrationRead,
					Event: "archive",
					View:  "home",
				},
			}, nil
		}
		if calls == 2 {
			require.Equal(t, directPassCanary, input.Registration.Reads[0].Booking.Comment)
			data, err := json.Marshal(input.Registration.Reads[0].Booking.Comment)
			require.NoError(t, err)
			return agent.Plan{
				View:         "workflow",
				ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: string(data)},
			}, nil
		}
		return finish(input)
	})
}

func pauseDirectPassScript(t *testing.T, f *fixture) telegram.Update {
	t.Helper()
	f.b.Scripts = scopeVM{}
	f.b.Model = directPassModel(
		t,
		`tools.preferences.setLanguage({language:"ru"}); return {derived:input};`,
		func(input agent.Input) (agent.Plan, error) {
			require.Contains(t, string(input.Script.Runs[0].Result), directPassCanary)
			return agent.Plan{}, context.Canceled
		},
	)
	update := message(31991, 101, "Read my registration")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	return update
}

func directPassFixture(t *testing.T) *fixture {
	t.Helper()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment=$1 WHERE event_id='archive'`,
		directPassCanary,
	)
	require.NoError(t, err)
	return f
}

func TestScriptPassDirectContextRecovery(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"removed", "reopened", "current expires", "replacement", "legacy", "partial read"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := directPassFixture(t)
			if scenario == "partial read" {
				_, err := f.db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name)
SELECT 'extra-admin-'||n,80000+n,'Admin '||n FROM generate_series(1,25) n;
INSERT INTO core.pass_payment_admins(event_id,owner) SELECT 'archive','extra-admin-'||n FROM generate_series(1,25) n`)
				require.NoError(t, err)
			}
			if scenario == "current expires" {
				_, err := f.db.Exec(
					t.Context(),
					`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'`,
				)
				require.NoError(t, err)
			}
			update := pauseDirectPassScript(t, f)
			if scenario == "partial read" {
				var omitted bool
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT (content#>>'{0,omitted}')::bool FROM bot.interactions WHERE owner='alice' AND update_id=31991 AND kind='registration_reads'`).
						Scan(&omitted),
				)
				require.True(t, omitted)
			}
			invalidateDirectPassContext(t, f, scenario)
			// Resume with a fresh bot instance using only the durable database/API.
			f.b = &bot.Bot{
				Delivery: f.b.Delivery,
				DB:       f.db,
				API:      f.b.API,
				Host:     f.b.Host,
				TG:       f.b.TG,
				Scripts:  scopeVM{},
			}
			input := retryPassDiscovery(t, f, update)
			raw, err := json.Marshal(input.Script)
			require.NoError(t, err)
			require.NotContains(t, string(raw), directPassCanary)
			require.Contains(t, string(raw), "pass_access_changed")
			require.Equal(t, 1, input.Script.Remaining)
			var operations int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).
					Scan(&operations),
			)
			require.Equal(t, 1, operations)
		})
	}
}

func TestScriptPassDirectContextOutage(t *testing.T) {
	t.Parallel()
	f := directPassFixture(t)
	update := pauseDirectPassScript(t, f)
	transport := &passBookingAuthorityTransport{}
	transport.fail.Store(true)
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "Checked"}}}
	f.b.Model = model
	require.Error(t, f.b.Handle(t.Context(), update))
	require.Empty(t, model.inputs)
	var stored string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=31991 AND kind='script_runs'`).
			Scan(&stored),
	)
	require.NotContains(t, stored, `"pass_redacted": true`)
	transport.fail.Store(false)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), directPassCanary)
}

func TestScriptPassDirectContextLateCompletion(t *testing.T) {
	t.Parallel()
	f := directPassFixture(t)
	f.b.Scripts = archivedPassLateVM{after: func() { removeArchivedBooking(t, f) }}
	f.b.Model = directPassModel(t, `return {derived:input};`, func(input agent.Input) (agent.Plan, error) {
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		require.NotContains(t, string(raw), directPassCanary)
		require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
		return agent.Plan{View: "workflow", Text: "Checked"}, nil
	})
	handle(t, f.b, message(31992, 101, "Read my registration"))
}

func TestScriptPassNoPrivateDirectContext(t *testing.T) {
	t.Parallel()
	f := directPassFixture(t)
	update := pausePassDiscovery(t, f, `return {public:"public-script-result"};`)
	removeArchivedBooking(t, f)
	input := retryPassDiscovery(t, f, update)
	require.Contains(t, string(input.Script.Runs[0].Result), "public-script-result")
}

func invalidateDirectPassContext(t *testing.T, f *fixture, scenario string) {
	t.Helper()
	if scenario == "legacy" {
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE bot.interactions SET content=content #- '{0,pass_context}' WHERE owner='alice' AND update_id=31991 AND kind='script_runs'`,
		)
		require.NoError(t, err)
	} else {
		removeArchivedBooking(t, f)
	}
	if scenario == "reopened" {
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.pass_events SET finishes_at=now()+interval '1 year' WHERE id='archive'`,
		)
		require.NoError(t, err)
	}
	if scenario == "current expires" {
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.pass_events SET finishes_at=now()-interval '1 year' WHERE id='archive'`,
		)
		require.NoError(t, err)
	}
	if scenario == "replacement" {
		_, err := f.db.Exec(
			t.Context(),
			`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,comment)
VALUES('archive','alice',1,'assigned','leader','guest','bob',now(),now(),100,'replacement')`,
		)
		require.NoError(t, err)
	}
}
