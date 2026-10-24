package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMealCatalogExactCASAuthorityExpiryAndHistoricalConfiguration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	catalog, err := f.H.store.MealCatalogConfiguration(ctx, admin, a.EventID, 0)
	require.NoError(t, err)
	document := catalog.Document
	document.Days[0].Name.EN = "Updated day"
	change := db.MealCatalogChange{
		EventID:              a.EventID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      1,
		Document:             document,
	}
	op := uuid.New()
	prepared, err := f.H.store.PrepareMealCatalog(ctx, admin, op, change)
	require.NoError(t, err)
	same, err := f.H.store.PrepareMealCatalog(ctx, admin, op, change)
	require.NoError(t, err)
	require.Equal(t, prepared, same)
	_, err = f.H.store.ChangeMealCatalog(ctx, admin, op, uuid.New(), change, func(context.Context) error { return nil })
	require.ErrorIs(t, err, db.ErrConflict)
	denied := errors.New("qa lost administrator authority")
	_, err = f.H.store.ChangeMealCatalog(
		ctx,
		admin,
		op,
		prepared.ID,
		change,
		func(context.Context) error { return denied },
	)
	require.ErrorIs(t, err, denied)
	current, err := f.H.store.MealCatalogConfiguration(ctx, admin, a.EventID, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	accepted, err := f.H.store.ChangeMealCatalog(
		ctx,
		admin,
		op,
		prepared.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), accepted.Version)
	receipt, err := f.H.store.MealCatalogOperation(ctx, admin, a.EventID, op)
	require.NoError(t, err)
	require.Equal(t, accepted, receipt)
	replay, err := f.H.store.ChangeMealCatalog(
		ctx,
		admin,
		op,
		prepared.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	require.Equal(t, accepted, replay)
	old, err := f.H.store.OwnMealCatalog(ctx, scope, a.EventID, 1)
	require.NoError(t, err)
	require.Equal(t, "Day one", old.Document.Days[0].Name.EN)
	firstOp, secondOp := uuid.New(), uuid.New()
	change.ExpectedVersion = 2
	first, err := f.H.store.PrepareMealCatalog(ctx, admin, firstOp, change)
	require.NoError(t, err)
	second, err := f.H.store.PrepareMealCatalog(ctx, admin, secondOp, change)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMealCatalog(
		ctx,
		admin,
		firstOp,
		first.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMealCatalog(
		ctx,
		admin,
		secondOp,
		second.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrConflict)
	change.ExpectedVersion = 3
	expiredOp := uuid.New()
	expired, err := f.H.store.PrepareMealCatalog(ctx, admin, expiredOp, change)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_meal_catalog_preparations SET expires_at=clock_timestamp(" +
")-interval '1 second' WHERE id=$1", expired.ID)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMealCatalog(
		ctx,
		admin,
		expiredOp,
		expired.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrConflict)
	retained, err := f.H.store.PrepareMealCatalog(ctx, admin, expiredOp, change)
	require.NoError(t, err)
	require.Equal(t, expired.ID, retained.ID)
	require.True(t, retained.ExpiresAt.Before(expired.ExpiresAt))
}

func TestMealCatalogCannotExpireWhileAuthorityCheckWaits(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, _, a := mealFixture(t)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	catalog, err := f.H.store.MealCatalogConfiguration(ctx, admin, a.EventID, 0)
	require.NoError(t, err)
	change := db.MealCatalogChange{
		EventID:              a.EventID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      catalog.Version,
		Document:             catalog.Document,
	}
	op := uuid.New()
	prepared, err := f.H.store.PrepareMealCatalog(ctx, admin, op, change)
	require.NoError(t, err)
	var cutoff time.Time
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "UPDATE shuttle_meal_catalog_preparations SET expires_at=clock_timestamp(" +
")+interval '1 second' WHERE id=$1 RETURNING expires_at", prepared.ID).
			Scan(&cutoff),
	)
	_, err = f.H.store.ChangeMealCatalog(ctx, admin, op, prepared.ID, change, func(context.Context) error {
		require.Eventually(t, func() bool {
			var now time.Time
			e := f.H.store.Pool().QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
			return e == nil && !now.Before(cutoff)
		}, 3*time.Second, 20*time.Millisecond)
		return nil
	})
	require.ErrorIs(t, err, db.ErrConflict)
	current, err := f.H.store.MealCatalogConfiguration(ctx, admin, a.EventID, 0)
	require.NoError(t, err)
	require.Equal(t, catalog, current)
	_, err = f.H.store.MealCatalogOperation(ctx, admin, a.EventID, op)
	require.Error(t, err)
}
