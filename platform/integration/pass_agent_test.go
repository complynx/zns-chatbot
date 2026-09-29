package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func registrationRead(view string) agent.Plan {
	return agent.Plan{
		View:               agent.RegistrationView,
		RegistrationAction: &agent.RegistrationProposal{Name: agent.RegistrationRead, Event: "dance", View: view},
	}
}

func TestRegistrationAgentTrustedContactReadMutationReplay(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	model := &knowledgeModel{plans: []agent.Plan{
		registrationRead("home"),
		{
			View:               agent.RegistrationView,
			Text:               "I propose inviting this person.",
			RegistrationAction: &agent.RegistrationProposal{Name: "invite", Event: "dance", InviteTelegramID: 202},
		},
	}}
	f.b.Model = model
	update := message(800, 101, "Invite this contact to Dance")
	update.Message.Contact = &telegram.Contact{UserID: 202, FirstName: "Boris"}
	handle(t, f.b, update)
	require.Len(t, model.inputs, 2)
	assert.Equal(t, []int64{202}, model.inputs[0].Registration.TrustedPartnerIDs)
	require.Len(t, model.inputs[1].Registration.Reads, 1)
	require.NotNil(t, model.inputs[1].Registration.Reads[0].Booking)
	service := passbooking.Service{DB: f.db}
	booking, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.EqualValues(t, 202, booking.InvitationTarget)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "Waiting for partner")
	handle(t, f.b, update)
	assert.Len(t, model.inputs, 2)
	var operations int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).
			Scan(&operations),
	)
	assert.Equal(t, 1, operations)
}

func TestRegistrationAgentRejectsGuessedPartnerAndStaleVersion(t *testing.T) {
	t.Parallel()
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "guessed", true: "stale"}[stale], func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			calls := 0
			service := passbooking.Service{DB: f.db}
			f.b.Model = avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
				calls++
				if calls == 1 {
					return registrationRead("home"), nil
				}
				if stale {
					_, err := service.Execute(ctx, "alice", bookingCommand("solo", "concurrent", passbooking.Booking{}))
					require.NoError(t, err)
				}
				return agent.Plan{
					View: agent.RegistrationView,
					RegistrationAction: &agent.RegistrationProposal{
						Name:             "invite",
						Event:            "dance",
						InviteTelegramID: 202,
					},
				}, nil
			})
			text := "Invite Boris"
			if stale {
				text = "Invite Telegram ID 202"
			}
			handle(t, f.b, message(810, 101, text))
			booking, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Zero(t, booking.InvitationTarget)
			if stale {
				assert.Contains(t, passMenuCard(t, f, 101).Text, "outdated")
			} else {
				assert.Zero(t, booking.Version)
			}
		})
	}
}

func TestRegistrationAgentReadBudgetAndDuplicateRead(t *testing.T) {
	t.Parallel()
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "budget", true: "duplicate"}[duplicate], func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			plans := []agent.Plan{
				registrationRead("home"),
				registrationRead("invitations"),
				registrationRead("admins"),
				registrationRead("queue"),
			}
			if duplicate {
				plans = []agent.Plan{registrationRead("home"), registrationRead("home")}
			}
			model := &knowledgeModel{plans: plans}
			f.b.Model = model
			handle(t, f.b, message(820, 101, "Read my registration"))
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT jsonb_array_length(content) FROM bot.interactions WHERE owner='alice' AND update_id=820 AND kind='registration_reads'`).
					Scan(&count),
			)
			if duplicate {
				assert.Equal(t, 1, count)
				assert.Len(t, model.inputs, 2)
			} else {
				assert.Equal(t, 3, count)
				assert.Len(t, model.inputs, 4)
			}
		})
	}
}

func TestRegistrationAgentPendingPartnerDoesNotCaptureUnrelatedText(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	handle(t, f.b, message(830, 101, "/passes"))
	handle(t, f.b, passMenuClick(t, f, 101, 831, "Dance"))
	handle(t, f.b, passMenuClick(t, f, 101, 832, "Invite a partner"))
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "I can help with that separate question."}}}
	f.b.Model = model
	handle(t, f.b, message(833, 101, "What does this word mean?"))
	require.Len(t, model.inputs, 1)
	assert.True(t, model.inputs[0].Registration.PendingPartner)
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings`).Scan(&count))
	assert.Zero(t, count)
}

func TestRegistrationAgentRevokedQueueReadIsHiddenOnRetry(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := passbooking.Service{DB: f.db}
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "read-target", passbooking.Booking{}))
	require.NoError(t, err)
	calls := 0
	f.b.Model = avModel(func(_ context.Context, _ agent.Input) (agent.Plan, error) {
		calls++
		if calls == 1 {
			return registrationRead("queue"), nil
		}
		return agent.Plan{}, context.Canceled
	})
	update := message(840, 202, "Show the registration queue")
	require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	model := &knowledgeModel{plans: []agent.Plan{{View: "workflow", Text: "The queue is unavailable."}}}
	f.b.Model = model
	handle(t, f.b, update)
	require.Len(t, model.inputs, 1)
	require.Len(t, model.inputs[0].Registration.Reads, 1)
	assert.Empty(t, model.inputs[0].Registration.Reads[0].Queue)
	assert.Equal(t, "forbidden", model.inputs[0].Registration.Reads[0].Error)
}
