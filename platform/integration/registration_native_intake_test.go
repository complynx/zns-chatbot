package integration_test

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// pauseNativeRegistration uses the real polling/saveBatch path. The observer
// pauses before business handling, authentication, Host admission or model work.
func pauseNativeRegistration(t *testing.T, f *fixture) func() {
	t.Helper()
	handleNotificationUpdate(t, f, message(83001, 101, "/passes"))
	handleNotificationUpdate(t, f, passMenuClick(t, f, 101, 83002, "Dance"))
	click := passMenuClick(t, f, 101, 83003, "Register solo")
	post(
		t,
		f.fake.URL+"/lab/input",
		map[string]any{"user": 101, "data": click.Callback.Data, "message_id": click.Callback.Message.ID},
	)
	barrier := &retainedIngressBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	f.b.Observer = barrier
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan error, 1)
	go func() { ended <- f.b.Run(ctx) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-ended:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("native poller did not join")
			}
		})
	}
	t.Cleanup(stop)
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("native input did not reach business barrier")
	}
	var native, inbox, intents int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.registration_ingress WHERE native_event='dance' AND native_owner='alice' AND native_payload IS NOT NULL AND native_outcome=''),
 (SELECT count(*) FROM bot.telegram_inbox),
 (SELECT count(*) FROM core.registration_intents WHERE owner='alice')`).Scan(&native, &inbox, &intents))
	require.Equal(t, 1, native, "event-specific envelope must be committed before business handling")
	require.Equal(t, 1, inbox)
	require.Zero(t, intents, "paused native request must not already have a canonical admission")
	return stop
}

func TestRegistrationRetentionNativeBeforeHandleBlocksHTTP(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"active", "expired", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			stop := pauseNativeRegistration(t, f)
			ctx := t.Context()
			if scenario == "expired" {
				_, err := f.db.Exec(
					ctx,
					"UPDATE core.registration_ingress SET received_at=clock_timestamp()-interval '11 minutes' WHERE native_event='dance'",
				)
				require.NoError(t, err)
			}
			if scenario == "stale" {
				_, err := f.db.Exec(ctx, "UPDATE bot.pass_views SET revision=revision+1 WHERE owner='alice'")
				require.NoError(t, err)
			}
			bob, err := f.b.API.ExecutePassBooking(
				ctx,
				"bob",
				bookingCommand("solo", "http-after-native", passbooking.Booking{}),
			)
			require.NoError(t, err)
			var outcome string
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT native_outcome FROM core.registration_ingress WHERE native_event='dance'").
					Scan(&outcome),
			)
			if scenario == "stale" {
				require.Equal(t, "rejected", outcome)
				require.Equal(t, "assigned", bob.State)
				var count int
				require.NoError(
					t,
					f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_intents WHERE owner='alice'").
						Scan(&count),
				)
				require.Zero(t, count, "invalid native evidence must not create or cancel another application")
				stop()
				return
			}
			require.Equal(t, "admitted", outcome)
			first := readRegistrationTurn(t, f.db, "alice")
			later := readRegistrationTurn(t, f.db, "bob")
			require.Equal(t, "captured", first.State)
			if scenario == "active" {
				require.Less(t, first.Position, later.Position)
				require.Zero(t, first.Count)
				require.Equal(t, "waitlist", bob.State)
			} else {
				require.Greater(t, first.Position, later.Position)
				require.EqualValues(t, 1, first.Count)
				require.Equal(t, "assigned", bob.State)
			}
			var role string
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT role FROM core.pass_profiles WHERE owner='alice'").Scan(&role),
			)
			require.Equal(t, "leader", role, "expiry keeps the draft profile")
			// Reconstruct application services against the same persisted evidence.
			restarted := notificationFixtureServices(f.db, appservices.Options{})
			require.NoError(t, restarted.Registration.ResolveRegistrationIntake(ctx, "dance"))
			require.Equal(t, first, readRegistrationTurn(t, f.db, "alice"), "retry must neither renew nor reprioritize")
			var hype int
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT count(*) FROM core.pass_registration_announcements WHERE owner='bob'").
					Scan(&hype),
			)
			if scenario == "active" {
				require.Zero(t, hype)
			} else {
				require.Equal(t, 1, hype)
			}
			stop()
			// A new poller consumes the original durable callback, not a fabricated retry.
			clone := *f.b
			clone.Observer = nil
			f.b = &clone
			var received int64
			require.NoError(
				t,
				f.db.QueryRow(ctx, "SELECT value FROM bot.cursors WHERE name='telegram_received'").Scan(&received),
			)
			completeInbox(t, f, received)
			after := readRegistrationTurn(t, f.db, "alice")
			require.Equal(t, first.ID, after.ID)
			require.Equal(t, first.Position, after.Position)
			require.Equal(t, first.Deadline, after.Deadline)
		})
	}
}

func TestRegistrationRetentionNativeImmutableAndMetadataRole(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	stop := pauseNativeRegistration(t, f)
	defer stop()
	ctx := t.Context()
	var original []byte
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT native_payload FROM core.registration_ingress WHERE native_event='dance'").
			Scan(&original),
	)
	var envelope registrationnative.Envelope
	require.NoError(t, json.Unmarshal(original, &envelope))
	roleTx, err := f.db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = roleTx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = roleTx.Exec(ctx, "SET LOCAL ROLE zns_api")
	require.NoError(t, err)
	_, unbound, err := registrationnative.Classify(ctx, roleTx, envelope.Chat, "not-a-saved-token")
	require.NoError(t, err)
	require.False(t, unbound)
	_, foreign, err := registrationnative.Classify(ctx, roleTx, 202, envelope.Token)
	require.NoError(t, err)
	require.False(t, foreign, "another sender cannot classify an owner-bound button")
	valid, err := registrationnative.Check(ctx, roleTx, envelope)
	require.NoError(t, err, "the real API role must hold the exact transport binding through commit")
	require.True(t, valid)
	for _, statement := range []string{
		"UPDATE bot.pass_views SET state=state WHERE owner='alice'",
		"UPDATE bot.pass_buttons SET action=action WHERE owner='alice'",
		"INSERT INTO bot.pass_views(owner,chat_id,revision,state) VALUES('visitor',303,1,'{}')",
		"DELETE FROM bot.pass_buttons WHERE owner='alice'",
	} {
		attempt, saveErr := roleTx.Begin(ctx)
		require.NoError(t, saveErr)
		_, writeErr := attempt.Exec(ctx, statement)
		require.Error(t, writeErr, "metadata lock privilege must not grant %s", statement)
		var denied *pgconn.PgError
		require.ErrorAs(t, writeErr, &denied)
		require.Equal(t, "42501", denied.Code)
		require.NoError(t, attempt.Rollback(ctx))
	}
	require.NoError(t, roleTx.Rollback(ctx))
	// The duplicate transport update cannot replace its first classified command.
	var updateID int64
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT request_key::bigint FROM core.registration_ingress WHERE native_event='dance'").
			Scan(&updateID),
	)
	envelope.Command.Key = "different-command"
	binding, err := envelope.Binding()
	require.NoError(t, err)
	duplicate, err := f.db.Begin(ctx)
	require.NoError(t, err)
	_, err = duplicate.Exec(ctx, "SET LOCAL ROLE zns_bot")
	require.NoError(t, err)
	require.NoError(t, registrationingress.SaveClassifiedTelegram(ctx, duplicate,
		registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: updateID}, 101, binding))
	require.NoError(t, duplicate.Commit(ctx))
	var after []byte
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT native_payload FROM core.registration_ingress WHERE native_event='dance'").
			Scan(&after),
	)
	require.JSONEq(t, string(original), string(after))
	var count int
	require.NoError(
		t,
		f.db.QueryRow(ctx, "SELECT count(*) FROM core.registration_ingress WHERE native_event='dance'").Scan(&count),
	)
	require.Equal(t, 1, count, "duplicate must not create a second classified request")
	assertNativeBotIngressPrivileges(t, f, updateID, binding)
	_, err = f.db.Exec(
		ctx,
		"UPDATE core.registration_ingress SET native_payload=$1 WHERE native_event='dance'",
		binding.Payload,
	)
	require.ErrorContains(t, err, "native registration evidence is immutable")
}

func assertNativeBotIngressPrivileges(
	t *testing.T,
	f *fixture,
	updateID int64,
	binding *registrationingress.NativeBinding,
) {
	t.Helper()
	ctx := t.Context()
	tx, err := f.db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, "SET LOCAL ROLE zns_bot")
	require.NoError(t, err)
	classified := registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: updateID + 1000000}
	generic := registrationingress.Reference{BotID: f.b.Delivery.BotID, UpdateID: updateID + 1000001}
	require.NoError(t, registrationingress.SaveClassifiedTelegram(ctx, tx, classified, 101, binding))
	require.NoError(t, registrationingress.SaveTelegram(ctx, tx, generic, 101))
	var payload []byte
	require.NoError(
		t,
		tx.QueryRow(ctx, "SELECT native_payload FROM core.registration_ingress WHERE bot_id=$1 AND request_key=$2", classified.BotID, strconv.FormatInt(classified.UpdateID, 10)).
			Scan(&payload),
	)
	require.JSONEq(t, string(binding.Payload), string(payload))
	var plain bool
	require.NoError(
		t,
		tx.QueryRow(ctx, "SELECT native_payload IS NULL FROM core.registration_ingress WHERE bot_id=$1 AND request_key=$2", generic.BotID, strconv.FormatInt(generic.UpdateID, 10)).
			Scan(&plain),
	)
	require.True(t, plain)
	attempt, err := tx.Begin(ctx)
	require.NoError(t, err)
	_, err = attempt.Exec(
		ctx,
		"UPDATE core.registration_ingress SET native_payload=native_payload WHERE bot_id=$1 AND request_key=$2",
		classified.BotID,
		strconv.FormatInt(classified.UpdateID, 10),
	)
	var denied *pgconn.PgError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "42501", denied.Code)
	require.NoError(t, attempt.Rollback(ctx))
	require.NoError(t, tx.Rollback(ctx))
}
