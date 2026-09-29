package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
)

const latePlanSecret = "late private registration canary"

func latePlanFixture(t *testing.T) *fixture {
	t.Helper()
	f := archivedPassFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET comment=$1 WHERE event_id='archive'`,
		latePlanSecret,
	)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	return f
}

func latePlanModel(t *testing.T, f *fixture, route string, finish func(agent.Input) (agent.Plan, error)) *int {
	t.Helper()
	calls := new(int)
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		*calls++
		if *calls == 1 {
			if route == "script" {
				return agent.Plan{
					View: "workflow",
					ScriptAction: &agent.ScriptProposal{
						Code:      `tools.preferences.setLanguage({language:"en"}); return tools.passes.get({event:"archive"});`,
						InputJSON: "null",
					},
				}, nil
			}
			return agent.Plan{
				View: agent.RegistrationView,
				RegistrationAction: &agent.RegistrationProposal{
					Name:  agent.RegistrationRead,
					Event: "archive",
					View:  "home",
				},
			}, nil
		}
		if *calls == 2 && route == "derived" {
			data, err := json.Marshal(input.Registration.Reads)
			require.NoError(t, err)
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `tools.preferences.setLanguage({language:"en"}); return input;`,
					InputJSON: string(data),
				},
			}, nil
		}
		data, err := json.Marshal(input)
		require.NoError(t, err)
		require.Contains(t, string(data), latePlanSecret)
		return finish(input)
	})
	return calls
}

func TestPassPlanLateExposureRoutes(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"direct", "derived", "script retry", "direct retry", "command"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			f := latePlanFixture(t)
			modelRoute := route
			if route == "script retry" || route == "command" {
				modelRoute = "script"
			}
			calls := latePlanModel(t, f, modelRoute, func(input agent.Input) (agent.Plan, error) {
				removeArchivedBooking(t, f)
				if strings.Contains(route, "retry") {
					require.NoError(t, input.BeforeProvider(t.Context(), &input))
				}
				plan := agent.Plan{View: "workflow", Text: latePlanSecret}
				if route == "command" {
					plan.View = agent.OrdersView
					plan.OrderAction = &agent.OrderProposal{Name: "create"}
				}
				return plan, nil
			})
			update := message(46992, 101, "Read my private registration")
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
			before := *calls
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
			require.Equal(t, before, *calls)
			assertPassPlanTerminal(t, f, 46992)
			var orders int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&orders),
			)
			require.Zero(t, orders)
		})
	}
}

func assertPassPlanTerminal(t *testing.T, f *fixture, id int64) {
	t.Helper()
	var raw string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT payload::text FROM interaction.saved_turns WHERE owner='alice' AND update_id=$1`, id).
			Scan(&raw),
	)
	var terminal bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT kind='terminal' AND state='privacy_terminal' AND reason='source_revoked' FROM interaction.saved_turns WHERE owner='alice' AND update_id=$1`, id).
			Scan(&terminal),
	)
	require.True(t, terminal)
	require.NotContains(t, raw, latePlanSecret)

	var archived int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND text LIKE '%'||$1||'%'`, latePlanSecret).
			Scan(&archived),
	)
	require.Zero(t, archived)
}

func TestPassPlanDeliveryRetryAndRender(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"revoked", "missing_authority", "outage"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := latePlanFixture(t)
			calls := latePlanModel(
				t,
				f,
				"script",
				func(agent.Input) (agent.Plan, error) { return agent.Plan{View: "workflow", Text: latePlanSecret}, nil },
			)
			post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
			update := message(46993, 101, "Read my private registration")
			require.Error(t, f.b.Handle(t.Context(), update))
			require.Equal(t, 2, *calls)
			switch scenario {
			case "outage":
				transport := &passToolAuthorityTransport{}
				transport.fail.Store(true)
				f.b.API.HTTP = &http.Client{Transport: transport}
				f.b.Host.HTTP = f.b.API.HTTP
				require.ErrorContains(t, f.b.Handle(t.Context(), update), "registration authority unavailable")
				require.Positive(t, transport.hits.Load(), "authority outage must be injected")
				transport.fail.Store(false)
				handle(t, f.b, update)
				require.Contains(t, workflowCard(t, f).Text, latePlanSecret)
			case "missing_authority":
				_, err := f.db.Exec(
					t.Context(),
					`UPDATE interaction.saved_turns SET payload=payload-'pass_authority' WHERE owner='alice' AND update_id=46993`,
				)
				require.NoError(t, err)
				for range 2 {
					require.ErrorContains(t, f.b.Handle(t.Context(), update), "invalid saved turn")
				}
				require.NoError(t, f.b.Render(t.Context(), "alice", 101))
				cards, err := json.Marshal(chatMessages(t, f, 101))
				require.NoError(t, err)
				require.NotContains(t, string(cards), latePlanSecret)
			default:
				removeArchivedBooking(t, f)
				require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
				require.NoError(t, f.b.Render(t.Context(), "alice", 101))
				require.NotContains(t, workflowCard(t, f).Text, latePlanSecret)
			}
			require.Equal(t, 2, *calls, "delivery retry does not repeat model work")
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

func TestPassPlanPostModelOutageRecovery(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	transport := &passToolAuthorityTransport{}
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	calls := latePlanModel(t, f, "script", func(agent.Input) (agent.Plan, error) {
		if !transport.fail.Load() {
			transport.fail.Store(true)
		}
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	update := message(46994, 101, "Read my private registration")
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "registration authority unavailable")
	require.Positive(t, transport.hits.Load(), "authority outage must be injected")
	var plans int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=46994`).
			Scan(&plans),
	)
	require.Zero(t, plans)
	transport.fail.Store(false)
	f.b = &bot.Bot{
		DB:      f.db,
		API:     f.b.API,
		Host:    f.b.Host,
		TG:      f.b.TG,
		Scripts: scopeVM{},
		Model: avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
			require.Equal(t, 1, input.Script.Remaining)
			require.Contains(t, string(input.Script.Runs[0].Result), latePlanSecret)
			return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
		}),
	}
	handle(t, f.b, update)
	require.Equal(t, 2, *calls)
	require.Contains(t, workflowCard(t, f).Text, latePlanSecret)
}

func TestPassPlanCommittedStatusRetry(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='alice'`)
	require.NoError(t, err)
	calls := latePlanModel(t, f, "direct", func(agent.Input) (agent.Plan, error) {
		post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
		return agent.Plan{
			View:        agent.OrdersView,
			Text:        latePlanSecret,
			OrderAction: &agent.OrderProposal{Name: "create"},
		}, nil
	})
	update := message(46999, 101, "Create an order after checking my registration")
	err = f.b.Handle(t.Context(), update)
	require.Error(t, err)
	var orders int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&orders))
	require.Equal(t, 1, orders, "the domain operation committed before delivery failed")
	removeArchivedBooking(t, f)
	handle(t, f.b, update)
	require.Equal(t, 2, *calls)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&orders))
	require.Equal(t, 1, orders)
	data, err := json.Marshal(chatMessages(t, f, 101))
	require.NoError(t, err)
	require.NotContains(t, string(data), latePlanSecret)
}
