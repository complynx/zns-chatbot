package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassBatchRuntimeGroundedReplay(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	tier := 1
	command := passbooking.RuntimeBatch{
		Key:        "runtime",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{999999, 101, 202},
		Options:    passbooking.AdminAssignment{AppendTier: &tier},
	}
	items, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "pass_recipient_unknown", items[0].Outcome.Code)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[2].Outcome.Status)
	service = passbooking.Service{DB: db}
	replay, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	assertRuntimeBatchReplayEqual(t, items, replay)
	command.Recipients = []int64{101}
	_, err = service.RunBatch(t.Context(), "bob", command)
	requireCode(t, err, "idempotency_conflict")
	var amount int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	assert.Equal(t, 23, amount, "the pair adds two places; its assigned recipient adds one")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	command.Recipients = []int64{999999, 101, 202}
	_, err = service.RunBatch(t.Context(), "bob", command)
	requireCode(t, err, "forbidden")
}

func TestPassBatchRuntimePaymentAdminCancel(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	command := passbooking.RuntimeBatch{Key: "cancel", Event: "dance", Action: "admin_cancel", Recipients: []int64{101}}
	items, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	_, err = service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.RunBatch(t.Context(), "bob", command)
	requireCode(t, err, "forbidden")
	_, err = service.TierStatus(t.Context(), "bob", "dance")
	requireCode(t, err, "forbidden")
}

func TestPassBatchRuntimeInterruptedMarkerResumes(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`CREATE FUNCTION core.fail_batch_marker() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic marker interruption'; END $$;
 CREATE TRIGGER fail_batch_marker BEFORE UPDATE ON core.pass_admin_batches FOR EACH ROW EXECUTE FUNCTION core.fail_batch_marker()`,
	)
	require.NoError(t, err)
	tier := 1
	command := passbooking.RuntimeBatch{
		Key:        "interrupted",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101, 202},
		Options:    passbooking.AdminAssignment{AppendTier: &tier},
	}
	_, err = service.RunBatch(t.Context(), "bob", command)
	require.Error(t, err)
	var state string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM core.pass_bookings WHERE owner='alice' AND event_id='dance'`).
			Scan(&state),
	)
	assert.Equal(t, "waitlist", state, "domain mutation and outcome marker roll back together")
	_, err = db.Exec(t.Context(), `DROP TRIGGER fail_batch_marker ON core.pass_admin_batches`)
	require.NoError(t, err)
	service = passbooking.Service{DB: db}
	items, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
	var amount int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	assert.Equal(t, 23, amount, "the pair adds two places; its assigned recipient adds one")
}

func TestPassBatchRuntimeConcurrentSingleConnection(t *testing.T) {
	t.Parallel()
	db, _ := adminPairFixture(t)
	config := db.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	defer pool.Close()
	service := passbooking.Service{DB: pool}
	command := passbooking.RuntimeBatch{
		Key:        "concurrent",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101},
	}
	const workers = 3
	const timeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	results := make(chan error, workers)
	for range workers {
		go func() { _, runErr := service.RunBatch(ctx, "bob", command); results <- runErr }()
	}
	for range workers {
		require.NoError(t, <-results)
	}
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestPassBatchRuntimeRejectedItemRollsBackProfile(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	name := "Synthetic Name"
	invalidTier := 99
	command := passbooking.RuntimeBatch{
		Key:        "rejected",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101},
		Options: passbooking.AdminAssignment{
			AppendTier: &invalidTier,
			Create:     &passbooking.AdminCreate{Role: passallocation.Leader, LegalName: &name},
		},
	}
	items, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.Equal(t, "pass_tier_invalid", items[0].Outcome.Code)
	var stored string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT legal_name FROM core.pass_profiles WHERE owner='alice'`).Scan(&stored),
	)
	assert.Empty(t, stored)
	current, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Zero(t, current.Version)
	replay, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.Equal(t, items, replay)
}

func TestPassBatchRuntimeUncouplePreservesPrices(t *testing.T) {
	t.Parallel()
	_, service := adminPairFixture(t)
	price := 101
	_, err := service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "pair",
			Version:       1,
			Target:        "alice",
			TargetVersion: 1,
			TotalPrice:    &price,
		},
	)
	require.NoError(t, err)
	command := passbooking.RuntimeBatch{
		Key:        "uncouple",
		Event:      "dance",
		Action:     "admin_uncouple",
		Recipients: []int64{101},
	}
	items, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Empty(t, alice.Partner)
	assert.Empty(t, bob.Partner)
	assert.Equal(t, 51, *alice.Price)
	assert.Equal(t, 50, *bob.Price)
}

func TestPassBatchRuntimeTelegramCommands(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
			require.NoError(t, err)
			handleVisible(
				t,
				f.b,
				message(9901, 202, `/passes_assign --pass_key dance --price 0 --comment "Guest pass" 101`),
			)
			messages := chatMessages(t, f, 202)
			require.NotEmpty(t, messages)
			if language == "ru" {
				assert.Contains(t, messages[len(messages)-1].Text, "выполнено")
			} else {
				assert.Contains(t, messages[len(messages)-1].Text, "completed")
			}
			service := passbooking.Service{DB: f.db}
			current, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, "paid", current.State)
			assert.Equal(t, "Guest pass", current.Comment)
			handleVisible(
				t,
				f.b,
				message(9901, 202, `/passes_assign --pass_key dance --price 0 --comment "Guest pass" 101`),
			)
			replay, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, current.Version, replay.Version)
			handleVisible(t, f.b, message(9902, 202, `/passes_tier --pass_key dance`))
			handleVisible(t, f.b, message(9903, 202, `/passes_cancel --pass_key dance 101`))
			final, err := service.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Equal(t, "cancelled", final.State)
		})
	}
}
