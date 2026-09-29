package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassAdminBatchPartialAssignmentReplay(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	tier := 1
	commands := []passbooking.AdminAssignment{
		{Event: "dance", Key: "missing", Version: 1, Target: "visitor"},
		{Event: "dance", Key: "pair", Version: 1, Target: "alice", TargetVersion: 1, AppendTier: &tier},
		{Event: "dance", Key: "stale-partner", Version: 1, Target: "bob", TargetVersion: 1},
	}
	result, err := service.AdminAssignBatch(t.Context(), "bob", commands)
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, passbooking.AdminBatchRejected, result[0].Status)
	assert.NotEmpty(t, result[0].Code)
	assert.Equal(t, passbooking.AdminBatchSucceeded, result[1].Status)
	require.NotNil(t, result[1].Assignment)
	assert.Equal(t, 2, result[1].Assignment.AssignedCount)
	assert.Equal(t, "pass_booking_stale", result[2].Code)
	service = passbooking.Service{DB: db}
	replay, err := service.AdminAssignBatch(t.Context(), "bob", commands)
	require.NoError(t, err)
	assert.Equal(t, passbooking.AdminBatchSucceeded, replay[1].Status)
	var amount, ledger int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&ledger))
	assert.Equal(t, 22, amount)
	assert.Equal(t, 1, ledger)
	commands[1].AppendTier = nil
	result, err = service.AdminAssignBatch(t.Context(), "bob", commands)
	require.NoError(t, err)
	assert.Equal(t, "idempotency_conflict", result[1].Code)
}

func TestPassAdminBatchCancelAndCurrentACL(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	commands := []passbooking.AdminCancellation{
		{Event: "dance", Key: "missing", Version: 1, Target: "visitor"},
		{Event: "dance", Key: "cancel", Version: 1, Target: "alice", TargetVersion: 1},
	}
	result, err := service.AdminCancelBatch(t.Context(), "bob", commands)
	require.NoError(t, err)
	assert.Equal(t, passbooking.AdminBatchRejected, result[0].Status)
	assert.Equal(t, passbooking.AdminBatchSucceeded, result[1].Status)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	assert.Empty(t, bob.Partner)
	result, err = service.AdminCancelBatch(t.Context(), "bob", commands)
	require.NoError(t, err)
	assert.Equal(t, passbooking.AdminBatchSucceeded, result[1].Status)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	result, err = service.AdminCancelBatch(t.Context(), "bob", commands)
	require.NoError(t, err)
	assert.Equal(t, "pass_booking_stale", result[0].Code)
	assert.Equal(
		t,
		passbooking.AdminBatchSucceeded,
		result[1].Status,
		"event payment administrator retains cancellation permission",
	)
}

func TestPassAdminBatchValidationPrecedesWrites(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	commands := []passbooking.AdminAssignment{
		{Event: "dance", Key: "valid", Version: 1, Target: "alice", TargetVersion: 1},
		{Event: "dance", Key: "invalid", Version: 1, Target: "bob", TargetVersion: -1},
	}
	_, err := service.AdminAssignBatch(t.Context(), "bob", commands)
	requireCode(t, err, "pass_booking_invalid")
	commands[1] = commands[0]
	_, err = service.AdminAssignBatch(t.Context(), "bob", commands)
	requireCode(t, err, "pass_booking_invalid")
	_, err = service.AdminCancelBatch(
		t.Context(),
		"bob",
		make([]passbooking.AdminCancellation, passbooking.MaxAdminBatchRecipients+1),
	)
	requireCode(t, err, "pass_booking_invalid")
	var operations int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&operations))
	assert.Zero(t, operations)
}

func TestPassAdminBatchCanceledContextDoesNotAttemptRecipients(t *testing.T) {
	t.Parallel()
	_, service := adminPairFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := service.AdminCancelBatch(ctx, "bob", []passbooking.AdminCancellation{
		{Event: "dance", Key: "one", Version: 1, Target: "alice", TargetVersion: 1},
		{Event: "dance", Key: "two", Version: 1, Target: "bob", TargetVersion: 1},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, result, 2)
	assert.Equal(t, passbooking.AdminBatchNotAttempted, result[0].Status)
	assert.Equal(t, passbooking.AdminBatchNotAttempted, result[1].Status)
}

func TestPassAdminBatchInfrastructureFailureStopsRecipients(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	db.Close()
	result, err := service.AdminAssignBatch(t.Context(), "bob", []passbooking.AdminAssignment{
		{Event: "dance", Key: "one", Version: 1, Target: "alice", TargetVersion: 1},
		{Event: "dance", Key: "two", Version: 1, Target: "bob", TargetVersion: 1},
	})
	require.Error(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, passbooking.AdminBatchInterrupted, result[0].Status)
	assert.Empty(t, result[0].Code, "infrastructure details must not enter public outcomes")
	assert.Equal(t, passbooking.AdminBatchNotAttempted, result[1].Status)
}
