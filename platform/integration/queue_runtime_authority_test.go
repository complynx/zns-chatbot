package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestQueueRuntimeResourceIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, code, mutation string
		payment              bool
	}{
		{"queue replacement", `return tools.passes.admin.queue({event:"dance"});`, `UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE owner='alice'`, false},
		{"payment replacement", `return tools.passes.payments.review({event:"dance"});`, `UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE owner='alice'`, true},
		{"payment decision", `return tools.passes.payments.review({event:"dance"});`, `UPDATE core.pass_payment_attempts SET decision='accepted',reviewed_by='bob',reviewed_at=now() WHERE event_id='dance'`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			needle := "queue-resource-private-canary"
			_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice'`, needle)
			require.NoError(t, err)
			if test.payment {
				needle = queueRuntimePayment(t, f)
			}
			update := pausePrivatePassTool(t, f, `tools.preferences.setLanguage({language:"ru"}); `+test.code, needle)
			input := retryPassDiscovery(t, f, update)
			require.Contains(
				t,
				string(input.Script.Runs[0].Result),
				needle,
				"authorized retained resource stays usable",
			)
			_, err = f.db.Exec(t.Context(), test.mutation)
			require.NoError(t, err)
			input = retryPassDiscovery(t, f, update)
			data, err := json.Marshal(input)
			require.NoError(t, err)
			require.NotContains(t, string(data), needle)
			require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
			require.Equal(t, 1, input.Script.Remaining)
			var effects int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.language_operations WHERE owner='bob'`).
					Scan(&effects),
			)
			require.Equal(t, 1, effects)
		})
	}
}

func queueRuntimePayment(t *testing.T, f *fixture) string {
	t.Helper()
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "100", Currency: "RUB"},
	}
	handle(t, f.b, photo)
	payment, err := (passbooking.Service{DB: f.db}).Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.NotEmpty(t, payment.ProofID)
	return payment.ProofID
}

func TestQueueRuntimeLegacyDerivedCapture(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	const needle = "legacy-queue-context-canary"
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_bookings SET comment=$1 WHERE owner='alice'`, needle)
	require.NoError(t, err)
	f.b.Scripts = scopeVM{}
	calls := 0
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		switch calls {
		case 1:
			return registrationRead("queue"), nil
		case 2:
			data, marshalErr := json.Marshal(input.Registration.Reads)
			require.NoError(t, marshalErr)
			return agent.Plan{
				View:         "workflow",
				ScriptAction: &agent.ScriptProposal{Code: `return input;`, InputJSON: string(data)},
			}, nil
		default:
			return agent.Plan{}, context.Canceled
		}
	})
	update := message(47111, 202, "Inspect private queue")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.interactions SET content=jsonb_set(content,'{0,pass_context,0}',(content#>'{0,pass_context,0}')-'queue_authorities') WHERE owner='bob' AND update_id=47111 AND kind='script_runs'`,
	)
	require.NoError(t, err)
	input := retryPassDiscovery(t, f, update)
	require.NotContains(t, string(input.Script.Runs[0].Result), needle)
	require.Contains(t, string(input.Script.Runs[0].Result), "pass_access_changed")
	require.Equal(t, 1, input.Script.Remaining)
}
