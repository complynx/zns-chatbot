package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestPassBatchSelfRecipientContinuation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"direct", "derived"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			for _, scenario := range []string{"resume", "external_version", "external_target_version", "new_identity", "telegram_identity", "missing_receipt", "revoked"} {
				t.Run(scenario, func(t *testing.T) {
					t.Parallel()
					runSelfRecipientContinuation(t, mode, scenario)
				})
			}
		})
	}
}

func runSelfRecipientContinuation(t *testing.T, mode, scenario string) {
	t.Helper()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`CREATE FUNCTION core.interrupt_second_assignment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.target='bob' THEN RAISE EXCEPTION 'synthetic dependency failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER interrupt_second_assignment BEFORE INSERT ON core.pass_admin_assignments
FOR EACH ROW EXECUTE FUNCTION core.interrupt_second_assignment()`,
	)
	require.NoError(t, err)
	price := 100
	command := passbooking.RuntimeBatch{
		Key:        "self-continuation",
		Event:      "dance",
		Action:     "admin_assign",
		Recipients: []int64{101, 202},
		Options:    passbooking.AdminAssignment{TotalPrice: &price},
	}
	var source readsource.Derivation
	if mode == "derived" {
		read, readErr := service.AdminTarget(t.Context(), "bob", "dance", 101)
		require.NoError(t, readErr)
		generation := int64(0)
		source = readsource.Derivation{
			Generation:     &generation,
			PrivateHistory: true,
			Authorities: readsource.Registration(
				[]passbooking.ReadAuthority{{Kind: passbooking.ReadPrivileged,
					Event: "dance", Owner: read.Booking.Owner, Version: read.Booking.Version,
					CreatedAt: read.Booking.CreatedAt, Action: "admin_assign", TargetTelegramID: 101}},
			),
		}
	}
	run := func() ([]passbooking.RuntimeBatchItem, error) {
		registration := passbooking.Service{DB: db}
		if mode == "derived" {
			return (derivedmutation.Service{DB: db, Registration: registration}).RunPassBatch(
				t.Context(),
				"bob",
				command,
				source,
			)
		}
		return registration.RunBatch(t.Context(), "bob", command)
	}
	_, err = run()
	require.Error(t, err, "second recipient fails after the first commits")
	_, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	var receipts int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&receipts),
	)
	require.Equal(t, 1, receipts)
	_, err = db.Exec(
		t.Context(),
		`DROP TRIGGER interrupt_second_assignment ON core.pass_admin_assignments`,
	)
	require.NoError(t, err)
	switch scenario {
	case "external_version":
		_, err = db.Exec(
			t.Context(),
			`UPDATE core.pass_bookings SET version=version+1 WHERE owner='bob' AND event_id='dance'`,
		)
	case "external_target_version":
		_, err = db.Exec(
			t.Context(),
			`UPDATE core.pass_bookings SET version=version+1 WHERE owner='alice' AND event_id='dance'`,
		)
	case "telegram_identity":
		_, err = db.Exec(t.Context(), `UPDATE core.users SET telegram_id=203 WHERE id='bob'`)
	case "new_identity":
		_, err = db.Exec(
			t.Context(),
			`UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE owner='bob' AND event_id='dance'`,
		)
	case "missing_receipt":
		_, err = db.Exec(t.Context(), `DELETE FROM core.pass_admin_assignments`)
	case "revoked":
		_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	}
	require.NoError(t, err)
	protected, protectedErr := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, protectedErr)
	items, err := run()
	if scenario == "revoked" {
		requireCode(t, err, "forbidden")
		return
	}
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	if scenario == "resume" {
		assert.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
		replay, replayErr := run()
		require.NoError(t, replayErr)
		assertRuntimeBatchReplayEqual(t, items, replay)
	} else {
		assert.Equal(t, passbooking.AdminBatchRejected, items[1].Outcome.Status)
		wantCode := "pass_booking_stale"
		if scenario == "missing_receipt" {
			wantCode = "source_stale"
		}
		assert.Equal(t, wantCode, items[1].Outcome.Code)
		current, readErr := service.Get(t.Context(), "alice", "dance")
		require.NoError(t, readErr)
		assert.Equal(t, protected, current, "replay and rejected continuation add no effects")
	}
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&receipts),
	)
	switch scenario {
	case "resume":
		assert.Equal(t, 2, receipts)
	case "missing_receipt":
		assert.Zero(t, receipts)
	default:
		assert.Equal(t, 1, receipts)
	}
}

func TestPassBatchSelfRecipientFirst(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	price := 100
	command := passbooking.RuntimeBatch{Key: "self-first", Event: "dance", Action: "admin_assign",
		Recipients: []int64{202, 101}, Options: passbooking.AdminAssignment{TotalPrice: &price}}
	items, err := service.RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[0].Outcome.Status)
	require.Equal(t, passbooking.AdminBatchSucceeded, items[1].Outcome.Status)
	original, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	replay, err := (passbooking.Service{DB: db}).RunBatch(t.Context(), "bob", command)
	require.NoError(t, err)
	assertRuntimeBatchReplayEqual(t, items, replay)
	after, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.Equal(t, original, after)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&count))
	require.Equal(t, 2, count)
}

// Compare every public field without freezing [time.Time]'s private Location value.
func assertRuntimeBatchReplayEqual(t *testing.T, expected, actual []passbooking.RuntimeBatchItem) {
	t.Helper()
	want, err := json.Marshal(expected)
	require.NoError(t, err)
	got, err := json.Marshal(actual)
	require.NoError(t, err)
	// Typed encoding preserves exact integer values and ordered results.
	assert.Equal(t, string(want), string(got))
}
