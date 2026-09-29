package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestRegistrationRevokedReadsStayHiddenAfterAllowedRead(t *testing.T) {
	t.Parallel()
	for _, view := range []string{"queue", agent.RegistrationAdminTarget} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			calls := 0
			f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				if calls == 1 {
					plan := registrationRead(view)
					if view == agent.RegistrationAdminTarget {
						plan.RegistrationAction.Target = "101"
					}
					return plan, nil
				}
				require.Len(t, input.Registration.Reads, 1)
				read := input.Registration.Reads[0]
				if view == agent.RegistrationAdminTarget {
					require.NotNil(t, read.AdminTarget)
				} else {
					require.NotEmpty(t, read.Queue)
				}
				return agent.Plan{}, context.Canceled
			})
			update := message(980, 202, "Inspect registration for Telegram ID 101")
			require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
			_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
			require.NoError(t, err)
			calls = 0
			f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				require.NotEmpty(t, input.Registration.Reads)
				read := input.Registration.Reads[0]
				assert.Equal(t, "forbidden", read.Error)
				assert.Nil(t, read.AdminTarget)
				assert.Nil(t, read.Booking)
				assert.Empty(t, read.Queue)
				assert.Empty(t, read.PaymentQueue)
				if calls == 1 {
					return registrationRead("events"), nil
				}
				require.Len(t, input.Registration.Reads, 2)
				assert.NotEmpty(t, input.Registration.Reads[1].Events)
				assert.Equal(t, 1, input.Registration.Remaining)
				return agent.Plan{View: "workflow", Text: "The event list is available."}, nil
			})
			handle(t, f.b, update)
			assert.Equal(t, 2, calls)
		})
	}
}

func TestRegistrationAuthorizationBeforeModelAfterHistoryRead(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"queue", agent.RegistrationAdminTarget, "database_failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			calls := 0
			f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				switch calls {
				case 1:
					plan := registrationRead("queue")
					if scenario == agent.RegistrationAdminTarget {
						plan.RegistrationAction.View, plan.RegistrationAction.Target = scenario, "101"
					}
					return plan, nil
				case 2:
					require.Len(t, input.Registration.Reads, 1)
					assert.Empty(t, input.Registration.Reads[0].Error)
					query := `DELETE FROM core.pass_booking_admins WHERE owner='bob'`
					if scenario == "database_failure" {
						query = `ALTER TABLE core.pass_booking_admins RENAME TO unavailable_registration_admins`
					}
					_, err := f.db.Exec(ctx, query)
					require.NoError(t, err)
					return agent.Plan{View: "workflow", HistoryAction: &agent.HistoryProposal{}}, nil
				default:
					require.NotEqual(
						t,
						"database_failure",
						scenario,
						"failed authorization checks must prevent another model invocation",
					)
					read := input.Registration.Reads[0]
					assert.Equal(t, "forbidden", read.Error)
					assert.Nil(t, read.AdminTarget)
					assert.Empty(t, read.Queue)
					assert.Equal(t, 2, input.Registration.Remaining)
					require.Len(t, input.Conversation.Reads, 1)
					return agent.Plan{View: "workflow", Text: "The history read completed."}, nil
				}
			})
			update := message(990, 202, "Inspect registration for 101 and recent history")
			err := f.b.Handle(t.Context(), update)
			if scenario == "database_failure" {
				assert.Equal(t, 2, calls)
				require.ErrorContains(t, err, "registration authority unavailable")
				var plans int
				require.NoError(
					t,
					f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='alice' AND update_id=990`).
						Scan(&plans),
				)
				assert.Zero(t, plans)
				_, restoreErr := f.db.Exec(
					t.Context(),
					`ALTER TABLE core.unavailable_registration_admins RENAME TO pass_booking_admins`,
				)
				require.NoError(t, restoreErr)
				f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
					require.Len(t, input.Registration.Reads, 1)
					assert.Empty(t, input.Registration.Reads[0].Error)
					assert.Equal(t, 2, input.Registration.Remaining)
					require.Len(t, input.Conversation.Reads, 1)
					return agent.Plan{View: "workflow", Text: "The recovered history read completed."}, nil
				})
				handle(t, f.b, update)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 3, calls)
			}
			var completed int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT jsonb_array_length(content) FROM bot.interactions
WHERE owner='bob' AND update_id=990 AND kind='history_reads'`).Scan(&completed))
			assert.Equal(t, 1, completed, "the non-registration read must complete before the boundary check")
		})
	}
}
