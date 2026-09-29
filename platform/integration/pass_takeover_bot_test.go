package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassTakeoverAgentAndManualOwnerBoundControls(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.pass_events SET finishes_at=now()-interval '1 day'`)
			require.NoError(t, err)
			calls := 0
			f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				p := &agent.RegistrationProposal{
					Name:   agent.RegistrationRead,
					Event:  "dance",
					View:   agent.RegistrationTakeoverTarget,
					Target: "101",
				}
				if calls > 1 {
					require.NotNil(t, input.Registration.Reads[0].TakeoverTarget)
					p.Name, p.Target = agent.RegistrationShow, "alice"
				}
				return agent.Plan{View: agent.RegistrationView, RegistrationAction: p}, nil
			})
			handle(t, f.b, message(1100, 101, "Show payment-contact controls for event dance, Telegram ID 101"))
			missing, err := i18n.Translate(language, i18n.RegistrationReceiverMissing, nil)
			require.NoError(t, err)
			assert.Contains(t, passMenuCard(t, f, 101).Text, missing)
			label, err := i18n.Translate(language, i18n.RegistrationTakeoverApply, nil)
			require.NoError(t, err)
			apply := passMenuClick(t, f, 101, 1101, label)
			foreign := apply
			callback := *apply.Callback
			callback.From.ID = 202
			foreign.Callback, foreign.ID = &callback, 1102
			handle(t, f.b, foreign)
			service := passbooking.Service{DB: f.db}
			before, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, "bob", before.PaymentAdmin)
			handle(t, f.b, apply)
			handle(t, f.b, apply)
			after, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, "alice", after.PaymentAdmin)
			assert.Equal(t, before.Version+1, after.Version)
			assert.NotContains(t, passMenuCard(t, f, 101).Text, "RegistrationTakeover")
			drainPassNotices(t, f)
			require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
			var deliveries, history int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_notification_deliveries d
 JOIN core.pass_notifications n ON n.id=d.notice_id WHERE n.kind='payment_contact_changed'`).Scan(&deliveries))
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events e
 JOIN core.pass_notifications n ON e.source_key='pass-notification-'||n.id::text WHERE n.kind='payment_contact_changed'`).Scan(&history))
			assert.Equal(t, 1, deliveries)
			assert.Equal(t, 1, history)
		})
	}
}

func TestPassTakeoverAgentBindsVersionAndRechecksRights(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"allowed", "revoked", "stale", "guessed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.pass_bookings SET payment_admin='alice' WHERE owner='alice'; DELETE FROM core.pass_booking_admins WHERE owner='bob'`,
			)
			require.NoError(t, err)
			calls := 0
			f.b.Model = avModel(func(ctx context.Context, _ agent.Input) (agent.Plan, error) {
				calls++
				if calls == 1 {
					return agent.Plan{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
						Name:   agent.RegistrationRead,
						Event:  "dance",
						View:   agent.RegistrationTakeoverTarget,
						Target: "101",
					}}, nil
				}
				switch scenario {
				case "revoked":
					_, err = f.db.Exec(ctx, `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
				case "stale":
					_, err = f.db.Exec(ctx, `UPDATE core.pass_bookings SET version=version+1 WHERE owner='alice'`)
				}
				require.NoError(t, err)
				return agent.Plan{View: agent.RegistrationView, RegistrationAction: &agent.RegistrationProposal{
					Name: passbooking.CommandTakeover, Event: "dance", Target: "alice",
				}}, nil
			})
			text := "Make me the payment contact for 101"
			if scenario == "guessed" {
				text = "Make me the payment contact for someone"
			}
			update := message(1110, 202, text)
			switch scenario {
			case "revoked", "stale":
				require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
				require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
				require.Equal(t, 2, calls)
			default:
				handle(t, f.b, update)
			}
			booking, err := f.b.API.PassBooking(t.Context(), "alice", "dance")
			require.NoError(t, err)
			if scenario == "allowed" {
				assert.Equal(t, "bob", booking.PaymentAdmin)
			} else {
				assert.Equal(t, "alice", booking.PaymentAdmin)
			}
		})
	}
}

func TestPassTakeoverTargetReadScope(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`DELETE FROM core.pass_booking_admins; INSERT INTO core.pass_events(id,finishes_at) VALUES('foreign',now()+interval '1 day'); UPDATE core.users SET name='Display Contact' WHERE id='bob'`,
	)
	require.NoError(t, err)
	service := passbooking.Service{DB: f.db}
	for _, read := range []func(context.Context, string, string, int64) (passbooking.TakeoverTarget, error){service.TakeoverTarget, f.b.API.PassTakeoverTarget} {
		_, err = read(t.Context(), "alice", "dance", 101)
		requireCode(t, err, "forbidden")
		_, err = read(t.Context(), "bob", "foreign", 101)
		requireCode(t, err, "forbidden")
		target, readErr := read(t.Context(), "bob", "dance", 101)
		require.NoError(t, readErr)
		require.NotNil(t, target.PaymentContact)
		assert.Equal(t, "Display Contact", target.PaymentContact.Name)
		assert.EqualValues(t, 202, target.PaymentContact.TelegramID)
		assert.Nil(t, target.ReceiverContact)
	}
}

func TestPassTakeoverManualDivergentPairContacts(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			s := passbooking.Service{DB: f.db}
			invite := bookingCommand("invite", "pair", passbooking.Booking{})
			invite.InviteTelegramID = 202
			alice, err := s.Execute(t.Context(), "alice", invite)
			require.NoError(t, err)
			accept := bookingCommand("accept", "pair", passbooking.Booking{})
			accept.Target, accept.TargetVersion = "alice", alice.Version
			bob, err := s.Execute(t.Context(), "bob", accept)
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','alice')`,
			)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
			require.NoError(t, err)
			alice, err = s.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			change := bookingCommand("payment_admin", "own-contact", alice)
			change.PaymentAdmin = "alice"
			alice, err = s.Execute(t.Context(), "alice", change)
			require.NoError(t, err)
			assert.Equal(t, "alice", alice.PaymentAdmin)
			assert.Equal(t, "bob", bob.PaymentAdmin)
			calls := 0
			f.b.Model = avModel(func(_ context.Context, _ agent.Input) (agent.Plan, error) {
				calls++
				p := &agent.RegistrationProposal{
					Name:   agent.RegistrationRead,
					Event:  "dance",
					View:   agent.RegistrationTakeoverTarget,
					Target: "202",
				}
				if calls > 1 {
					p.Name, p.Target = agent.RegistrationShow, "bob"
				}
				return agent.Plan{View: agent.RegistrationView, RegistrationAction: p}, nil
			})
			handle(t, f.b, message(1900, 202, "Show payment-contact controls for Telegram ID 202"))
			label, err := i18n.Translate(language, i18n.RegistrationTakeoverApply, nil)
			require.NoError(t, err)
			handle(t, f.b, passMenuClick(t, f, 202, 1901, label))
			after, err := s.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, "bob", after.PaymentAdmin)
			assert.Equal(t, alice.Version+1, after.Version)
			unchanged, err := s.Get(t.Context(), "bob", "dance")
			require.NoError(t, err)
			assert.Equal(t, bob, unchanged)
			// A fresh paired-target button is safe when both contacts already match.
			handle(t, f.b, passMenuClick(t, f, 202, 1902, label))
			repeated, err := s.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, after, repeated)
		})
	}
}

func TestPassTakeoverDelayedContactCycleDeliversLatestNotice(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_booking_admins(owner) VALUES('alice'); UPDATE core.pass_notifications SET delivered_at=now()`,
	)
	require.NoError(t, err)
	s := passbooking.Service{DB: f.db}
	for index, actor := range []string{"alice", "bob", "alice"} {
		target, readErr := s.TakeoverTarget(t.Context(), actor, "dance", 101)
		require.NoError(t, readErr)
		_, err = s.Execute(t.Context(), actor, passbooking.Command{
			Name: passbooking.CommandTakeover, Event: "dance", Key: fmt.Sprintf("cycle-%d", index),
			Version: target.ActorVersion, Target: "alice", TargetVersion: target.Booking.Version,
		})
		require.NoError(t, err)
	}
	var latest int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT max(id) FROM core.pass_notifications WHERE kind='payment_contact_changed'`).
			Scan(&latest),
	)
	for _, expectedCurrent := range []bool{false, false, true} {
		notices, readErr := s.PendingNotifications(t.Context())
		require.NoError(t, readErr)
		require.Len(t, notices, 1)
		assert.Equal(t, expectedCurrent, notices[0].Current)
		if expectedCurrent {
			assert.Equal(t, latest, notices[0].ID)
		}
		require.NoError(t, f.b.DeliverPassNotifications(t.Context()))
	}
	pending, err := s.PendingNotifications(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pending)
	require.Len(t, chatMessages(t, f, 101), 1, "only the latest contact notification reaches Telegram")
	var deliveries, history int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.pass_notification_deliveries`).Scan(&deliveries),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE source_key LIKE 'pass-notification-%'`).
			Scan(&history),
	)
	assert.Equal(t, 1, deliveries)
	assert.Equal(t, 1, history)
	var delivered int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT notice_id FROM bot.pass_notification_deliveries`).Scan(&delivered),
	)
	assert.Equal(t, latest, delivered)
}
