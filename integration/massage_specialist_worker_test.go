package integration_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func awaitSpecialistNotice(
	t *testing.T,
	f paymentFixture,
	id uuid.UUID,
	state string,
) db.MassageSpecialistNotification {
	t.Helper()
	var result db.MassageSpecialistNotification
	require.Eventually(t, func() bool {
		var err error
		result, err = f.H.store.MassageSpecialistNotification(
			t.Context(),
			db.EventScope{BotID: 123, Organization: "org"},
			id,
		)
		return err == nil && result.State == state
	}, 10*time.Second, 25*time.Millisecond)
	return result
}

func specialistMessages(p *peers) []telegram.SendMessageRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := []telegram.SendMessageRequest{}
	for _, message := range p.sent {
		if message.ChatID == 11001 {
			result = append(result, message)
		}
	}
	return result
}

func TestMassageSpecialistWorkerDeliversLocalizedContactsAndCancellationWithoutLoom(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "uk"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, staff, schedule, _ := staffFixture(t)
			owner := ownerProfileScope(t, f.H, f.OwnerID)
			_, err := f.H.store.Pool().
				Exec(
					t.Context(),
					"UPDATE shuttle_users SET profile=$2 WHERE id=$1",
					staff.UserID,
					json.RawMessage(`{"language_code":"`+language+`"}`),
				)
			require.NoError(t, err)
			_, err = f.H.store.Pool().
				Exec(
					t.Context(),
					"UPDATE shuttle_users SET profile=$2 WHERE id=$1",
					owner.UserID,
					json.RawMessage(`{"first_name":"Client 🐈 <b>name</b>"}`),
				)
			require.NoError(t, err)
			booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
			id := specialistNotices(t, f, booking.ID)[0].ID
			f.H.start(t)
			delivered := awaitSpecialistNotice(t, f, id, "delivered")
			require.NotEqual(t, uuid.Nil, delivered.AttemptID)
			messages := specialistMessages(f.H.peers)
			require.Len(t, messages, 1)
			message := messages[0]
			heading := "New massage booking"
			cancelHeading := "Massage booking cancelled"
			if language == "uk" {
				heading = "Новая запись на массаж"
				cancelHeading = "Запись на массаж отменена"
			}
			require.Contains(t, message.Text, heading)
			require.Contains(t, message.Text, "Client 🐈 <b>name</b>")
			require.Contains(t, message.Text, booking.Snapshot.EventName)
			require.Contains(t, message.Text, booking.Snapshot.Terms.Timezone)
			require.Contains(t, message.Text, booking.ID.String())
			zone, err := time.LoadLocation(booking.Snapshot.Terms.Timezone)
			require.NoError(t, err)
			require.Contains(t, message.Text, booking.Snapshot.Terms.StartsAt.In(zone).Format(time.RFC3339))
			require.Contains(t, message.Text, booking.Snapshot.Terms.TreatmentEndsAt.In(zone).Format(time.RFC3339))
			specialistName := booking.Snapshot.Terms.SpecialistName.EN
			if language == "uk" {
				specialistName = booking.Snapshot.Terms.SpecialistName.RU
			}
			require.Contains(t, message.Text, specialistName)
			require.Len(t, message.Entities, 1)
			entity := message.Entities[0]
			require.Equal(t, "text_link", entity.Type)
			require.Equal(t, "tg://user?id=11002", entity.URL)
			units := utf16.Encode([]rune(message.Text))
			require.LessOrEqual(t, entity.Offset+entity.Length, len(units))
			label := string(utf16.Decode(units[entity.Offset : entity.Offset+entity.Length]))
			require.NotEmpty(t, label)
			if language == "uk" {
				require.Equal(t, "Связаться с клиентом", label)
			} else {
				require.Equal(t, "Contact client", label)
			}
			cancelMassageForTest(t, f, owner, booking)
			notices := specialistNotices(t, f, booking.ID)
			require.Len(t, notices, 2)
			var cancelID uuid.UUID
			for _, n := range notices {
				if n.Kind == "cancelled" {
					cancelID = n.ID
				}
			}
			require.NotEqual(t, uuid.Nil, cancelID)
			awaitSpecialistNotice(t, f, cancelID, "delivered")
			messages = specialistMessages(f.H.peers)
			require.Len(t, messages, 2)
			require.Contains(t, messages[1].Text, cancelHeading)
			require.Empty(t, messages[1].Entities)
			require.NotContains(t, messages[1].Text, "tg://")
			f.H.peers.mu.Lock()
			require.Empty(t, f.H.peers.createKeys)
			f.H.peers.mu.Unlock()
			f.H.stop(t)
			f.H.start(t)
			require.Never(
				t,
				func() bool { return len(specialistMessages(f.H.peers)) != 2 },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
		})
	}
}

func TestMassageSpecialistWorkerRechecksChangesAfterIAMWait(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"mute_reenable", "regrant", "module", "revoked", "cancel"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, staff, schedule, binding := staffFixture(t)
			owner := ownerProfileScope(t, f.H, f.OwnerID)
			booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
			id := specialistNotices(t, f, booking.ID)[0].ID
			p := f.H.peers
			entered, release := make(chan struct{}), make(chan struct{})
			var signal, unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(release) }) })
			p.tokenHook = func(subject string) {
				if subject == staff.Subject {
					signal.Do(func() { close(entered); <-release })
				}
			}
			f.H.start(t)
			select {
			case <-entered:
			case <-time.After(8 * time.Second):
				t.Fatal("specialist notice did not reach IAM wait")
			}
			switch change {
			case "mute_reenable":
				_, err := f.H.store.SetMassageSpecialistPreferences(
					t.Context(),
					staff,
					uuid.New(),
					noticePreference(f.Event.ID, 0, false),
				)
				require.NoError(t, err)
				_, err = f.H.store.SetMassageSpecialistPreferences(
					t.Context(),
					staff,
					uuid.New(),
					noticePreference(f.Event.ID, 1, true),
				)
				require.NoError(t, err)
			case "regrant":
				binding.Active = false
				revoked := bindingChange(t, f, staff, binding)
				binding.Active = true
				binding.ExpectedVersion = revoked.Version
				bindingChange(t, f, staff, binding)
			case "module":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
				require.NoError(t, err)
			case "revoked":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
				require.NoError(t, err)
			case "cancel":
				cancelMassageForTest(t, f, owner, booking)
			}
			unblock.Do(func() { close(release) })
			blocked := awaitSpecialistNotice(t, f, id, "delivery_blocked")
			require.Equal(t, uuid.Nil, blocked.AttemptID)
			require.Empty(t, blocked.Payload)
			for _, message := range specialistMessages(p) {
				require.NotContains(t, message.Text, "New massage booking")
				require.Empty(t, message.Entities)
			}
		})
	}
}

func TestMassageSpecialistWorkerWaitsForIAMAndRetriesKnownTelegramRejection(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	id := specialistNotices(t, f, booking.ID)[0].ID
	p := f.H.peers
	entered := make(chan struct{}, 1)
	p.unavailableTokenSubjects = map[string]bool{staff.Subject: true}
	p.tokenHook = func(subject string) {
		if subject == staff.Subject {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
	}
	f.H.start(t)
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("specialist notice did not attempt IAM")
	}
	pending := awaitSpecialistNotice(t, f, id, "pending")
	require.Equal(t, uuid.Nil, pending.AttemptID)
	require.Empty(t, specialistMessages(p))
	f.H.stop(t)
	p.mu.Lock()
	p.unavailableTokenSubjects = nil
	p.rejectSendOnce = true
	p.loomDown = true
	p.mu.Unlock()
	f.H.start(t)
	awaitSpecialistNotice(t, f, id, "retryable")
	delivered := awaitSpecialistNotice(t, f, id, "delivered")
	require.NotEqual(t, uuid.Nil, delivered.AttemptID)
	require.Len(t, specialistMessages(p), 2)
	p.mu.Lock()
	require.Empty(t, p.createKeys)
	p.mu.Unlock()
}

func TestMassageSpecialistWorkerPreservesUnknownWithoutAutomaticResend(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"response_lost", "startup_inflight", "receipt_write"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			f, staff, schedule, _ := staffFixture(t)
			owner := ownerProfileScope(t, f.H, f.OwnerID)
			booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
			id := specialistNotices(t, f, booking.ID)[0].ID
			expectedAttempts := 1
			switch failure {
			case "response_lost":
				f.H.peers.mu.Lock()
				f.H.peers.loseSend = true
				f.H.peers.mu.Unlock()
			case "startup_inflight":
				_, err := f.H.store.ReserveMassageSpecialistNotification(
					t.Context(),
					paymentScope(t, f.H, staff.UserID, false),
					id,
					1,
					json.RawMessage(`{"text":"uncertain startup attempt"}`),
				)
				require.NoError(t, err)
				expectedAttempts = 0
			case "receipt_write":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "CREATE FUNCTION reject_specialist_receipt() RETURNS trigger LANGUAGE plpgsql AS "+
						"$$ BEGIN RAISE EXCEPTION 'synthetic specialist receipt failure'; END $$;\n"+
						" CREATE TRIGGER reject_specialist_receipt BEFORE UPDATE ON "+
						"shuttle_massage_specialist_notifications FOR EACH ROW WHEN "+
						"(NEW.state='delivered') EXECUTE FUNCTION reject_specialist_receipt()")
				require.NoError(t, err)
			}
			f.H.start(t)
			unknown := awaitSpecialistNotice(t, f, id, "delivery_unknown")
			require.NotEqual(t, uuid.Nil, unknown.AttemptID)
			require.Len(t, specialistMessages(f.H.peers), expectedAttempts)
			f.H.stop(t)
			f.H.start(t)
			require.Never(
				t,
				func() bool { return len(specialistMessages(f.H.peers)) != expectedAttempts },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
			require.Equal(t, unknown, awaitSpecialistNotice(t, f, id, "delivery_unknown"))
		})
	}
}
