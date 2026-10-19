package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestMealDraftOperationRecoveryUsesOriginalReceiptAndCurrentAccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, change := mealFixture(t)
	first, err := f.H.store.SaveOwnMealDraft(ctx, scope, change)
	require.NoError(t, err)
	next := completeMeal(change)
	next.OperationID = uuid.New()
	next.ExpectedDraftVersion = 1
	second, err := f.H.store.SaveOwnMealDraft(ctx, scope, next)
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Version)
	receipt, err := f.H.store.OwnMealDraftOperationByID(ctx, scope, change.OperationID)
	require.NoError(t, err)
	require.Equal(t, first, receipt, "status returns the original result, not the latest draft")
	_, err = f.H.store.OwnMealDraftOperationByID(ctx, scope, uuid.New())
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnMealDraftOperationByID(ctx, scope, uuid.Nil)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	foreign := ownerProfileScope(t, f.H, f.AmbassadorID)
	_, err = f.H.store.OwnMealDraftOperationByID(ctx, foreign, change.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "another user cannot recover an owner's receipt")
	wrong := scope
	wrong.Subject = "wrong-subject"
	_, err = f.H.store.OwnMealDraftOperationByID(ctx, wrong, change.OperationID)
	require.Error(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", change.EventID)
	require.NoError(t, err)
	_, err = f.H.store.OwnMealDraftOperationByID(ctx, scope, change.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','meals'] WHERE id=$1", change.EventID)
	require.NoError(t, err)
	receipt, err = f.H.store.OwnMealDraftOperationByID(ctx, scope, change.OperationID)
	require.NoError(t, err)
	require.Equal(t, first, receipt)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.OwnMealDraftOperationByID(ctx, scope, change.OperationID)
	require.Error(t, err)
}
