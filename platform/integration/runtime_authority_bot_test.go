package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestRuntimeAuthorityProviderOutageRecovery(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"direct", "script", "derived"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			f := latePlanFixture(t)
			transport := &passToolAuthorityTransport{}
			f.b.API.HTTP = &http.Client{Transport: transport}
			f.b.Host.HTTP = f.b.API.HTTP
			latePlanModel(t, f, route, func(input agent.Input) (agent.Plan, error) {
				transport.fail.Store(true)
				err := input.BeforeProvider(t.Context(), &input)
				transport.fail.Store(false)
				require.Error(t, err)
				return agent.Plan{}, err
			})
			update := message(47101, 101, "Read my private registration")
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "registration authority unavailable")
			var saved int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=47101`).
					Scan(&saved),
			)
			require.Zero(t, saved)
			f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
				data, err := json.Marshal(input)
				require.NoError(t, err)
				require.Contains(t, string(data), latePlanSecret)
				if route != "direct" {
					require.Equal(t, 1, input.Script.Remaining)
				}
				return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
			})
			handle(t, f.b, update)
			if route != "direct" {
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).
						Scan(&saved),
				)
				require.Equal(t, 1, saved)
			}
		})
	}
}

func TestRuntimeAuthorityHistoryDerivedLateRevocation(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	latePlanModel(t, f, "direct", func(agent.Input) (agent.Plan, error) {
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	handle(t, f.b, message(47102, 101, "Read private registration"))
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			data, err := json.Marshal(input.History)
			require.NoError(t, err)
			require.Contains(t, string(data), latePlanSecret)
			return agent.Plan{
				View:         "workflow",
				ScriptAction: &agent.ScriptProposal{Code: `return input;`, InputJSON: string(data)},
			}, nil
		}
		require.NotEmpty(t, input.Script.Runs)
		require.Contains(t, string(input.Script.Runs[0].Result), latePlanSecret)
		removeArchivedBooking(t, f)
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	update := message(47103, 101, "Summarize our prior answer")
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
	require.Equal(t, 2, calls)
	var archived int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key='tg-assistant-47103' AND text=$1`, latePlanSecret).
			Scan(&archived),
	)
	require.Zero(t, archived)
}

func TestRuntimeAuthorityUpgradeRecognizedArchive(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	latePlanModel(
		t,
		f,
		"direct",
		func(agent.Input) (agent.Plan, error) { return agent.Plan{View: "workflow", Text: latePlanSecret}, nil },
	)
	handle(t, f.b, message(47104, 101, "Read private registration"))
	_, err := f.db.Exec(
		t.Context(),
		`DELETE FROM core.conversation_read_authorities WHERE event_id IN (SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='tg-assistant-47104')`,
	)
	require.NoError(t, err)
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		data, marshalErr := json.Marshal(input.History)
		require.NoError(t, marshalErr)
		require.NotContains(t, string(data), latePlanSecret)
		return agent.Plan{View: "workflow", Text: "ok"}, nil
	})
	handle(t, f.b, message(47105, 101, "Read prior history"))
}

func TestRuntimeAuthorityPrivilegedTargetIncarnation(t *testing.T) {
	t.Parallel()
	for _, view := range []string{agent.RegistrationAdminTarget, agent.RegistrationTakeoverTarget} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET comment=$1 WHERE event_id='dance' AND owner='alice'`,
				latePlanSecret,
			)
			require.NoError(t, err)
			calls := 0
			f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				if calls == 1 {
					plan := registrationRead(view)
					plan.RegistrationAction.Target = "101"
					return plan, nil
				}
				data, marshalErr := json.Marshal(input.Registration.Reads)
				require.NoError(t, marshalErr)
				require.Contains(t, string(data), latePlanSecret)
				_, replaceErr := f.db.Exec(
					t.Context(),
					`UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE event_id='dance' AND owner='alice'`,
				)
				require.NoError(t, replaceErr)
				return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
			})
			update := message(47106, 202, "Inspect registration for Telegram ID 101")
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
			require.Equal(t, 2, calls)
		})
	}
}
func TestRuntimeAuthorityPureScriptInheritsPriorScript(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		switch calls {
		case 1:
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `return tools.passes.get({event:"archive"});`,
					InputJSON: "null",
				},
			}, nil
		case 2:
			require.Len(t, input.Script.Runs, 1)
			require.Contains(t, string(input.Script.Runs[0].Result), latePlanSecret)
			return agent.Plan{
				View: "workflow",
				ScriptAction: &agent.ScriptProposal{
					Code:      `return {derived:input};`,
					InputJSON: string(input.Script.Runs[0].Result),
				},
			}, nil
		default:
			require.Len(t, input.Script.Runs, 2)
			require.Contains(t, string(input.Script.Runs[1].Result), latePlanSecret)
			return agent.Plan{}, context.Canceled
		}
	})
	update := message(47107, 101, "Read and transform my registration")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	removeArchivedBooking(t, f)
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		data, err := json.Marshal(input.Script)
		require.NoError(t, err)
		require.NotContains(t, string(data), latePlanSecret)
		require.Zero(t, input.Script.Remaining)
		return agent.Plan{View: "workflow", Text: "Source unavailable."}, nil
	})
	handle(t, f.b, update)
}

func TestRuntimeAuthorityTerminalInboxNotice(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	calls := latePlanModel(t, f, "direct", func(agent.Input) (agent.Plan, error) {
		removeArchivedBooking(t, f)
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "Read private registration"})
	completeInbox(t, f, 2)
	require.Equal(t, 2, *calls)
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&pending))
	require.Zero(t, pending)
	var effects int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='alice'`).Scan(&effects),
	)
	require.Zero(t, effects)
	cards := chatMessages(t, f, 101)
	require.NotEmpty(t, cards, "terminal denial must give the user a safe refusal")
	require.Contains(t, cards[len(cards)-1].Text, "no longer available", "a generic workflow card is not a refusal")
	data, err := json.Marshal(cards)
	require.NoError(t, err)
	require.NotContains(t, string(data), latePlanSecret)
	completeInbox(t, f, 2)
	require.Equal(t, 2, *calls)
}

func TestRuntimeAuthorityTerminalNoticeDeliveryRecovery(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := latePlanFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			calls := latePlanModel(t, f, "direct", func(agent.Input) (agent.Plan, error) {
				removeArchivedBooking(t, f)
				post(t, f.fake.URL+"/lab/fault", map[string]string{"mode": "transient"})
				return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
			})
			update := message(1, 101, "Read private registration")
			err = f.b.Handle(t.Context(), update)
			require.Error(t, err)
			require.NotContains(
				t,
				err.Error(),
				"terminal registration plan",
				"delivery failure must keep inbox retryable",
			)
			assertPassPlanTerminal(t, f, 1)
			payload, err := json.Marshal(update)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `INSERT INTO bot.telegram_inbox(update_id,payload) VALUES(1,$1)`, payload)
			require.NoError(t, err)
			restarted := *f.b
			f.b = &restarted
			completeInbox(t, f, 2)
			require.Equal(t, 2, *calls)
			notice, err := i18n.Translate(language, i18n.AgentSourceUnavailable, nil)
			require.NoError(t, err)
			cards := chatMessages(t, f, 101)
			require.NotEmpty(t, cards)
			require.Contains(t, cards[len(cards)-1].Text, notice)
			require.NoError(t, f.b.Render(t.Context(), "alice", 101))
			require.Equal(t, cards, chatMessages(t, f, 101), "normal reconciliation must retain the refusal")
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
			require.Equal(t, cards, chatMessages(t, f, 101), "a replay must not duplicate the delivered refusal")
			completeInbox(t, f, 2)
			require.Equal(t, 2, *calls)
			var quota int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.agent_quota WHERE owner='alice' AND update_id=1`).
					Scan(&quota),
			)
			require.Equal(t, 1, quota)
		})
	}
}

func TestRuntimeAuthorityTerminalNoticeDoesNotAuthorizeLegacyArchive(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	latePlanModel(
		t,
		f,
		"direct",
		func(agent.Input) (agent.Plan, error) { return agent.Plan{View: "workflow", Text: latePlanSecret}, nil },
	)
	handle(t, f.b, message(47104, 101, "Read private registration"))
	_, err := f.db.Exec(
		t.Context(),
		`DELETE FROM core.conversation_read_authorities WHERE event_id IN (SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='tg-assistant-47104')`,
	)
	require.NoError(t, err)
	removeArchivedBooking(t, f)
	require.ErrorContains(
		t,
		f.b.Handle(t.Context(), message(47104, 101, "Read private registration")),
		"terminal registration plan",
	)
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		data, marshalErr := json.Marshal(input.History)
		require.NoError(t, marshalErr)
		require.NotContains(t, string(data), latePlanSecret)
		return agent.Plan{View: "workflow", Text: "ok"}, nil
	})
	handle(t, f.b, message(47105, 101, "Read prior history"))
}
