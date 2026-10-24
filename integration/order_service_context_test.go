package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestOrderServiceContextSurvivesCatalogChangeAndRestart(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Len(t, prepared.Snapshot.Services, 1, "Two items in the same service retain one service interval")
	sold := prepared.Snapshot.Services[0]
	require.Equal(t, "lunch", sold.OfferID)
	require.Equal(t, "Lunch", sold.Name.EN)
	require.Equal(t, "Обед", sold.Name.RU)
	require.Equal(t, time.UTC, sold.StartsAt.Location())
	require.True(t, sold.EndsAt.After(sold.StartsAt))
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	doc := catalogDocument()
	doc.Offers[0].Name.EN = "Replacement service"
	doc.Offers[0].StartsAt = doc.Offers[0].StartsAt.Add(10 * time.Minute)
	change := db.OrdersCatalogChange{
		EventID:              args.EventID,
		ExpectedEventVersion: args.ExpectedEventVersion,
		ExpectedVersion:      1,
		Document:             doc,
	}
	edit := uuid.New()
	status, _ := commitCatalog(t, f, edit, prepareCatalog(t, f, edit, change))
	require.Equal(t, "succeeded", status)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	current, err := reopened.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Equal(t, prepared.Snapshot, current.Snapshot)
	replay, err := reopened.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, result, replay)
}

func TestOldOrderPreparationRequiresFreshServiceConsent(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	// Simulate a persisted preparation written before service context existed.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_preparations SET snapshot=snapshot-'services' WHERE id=$1", prepared.ID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	fresh, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.NotEqual(t, prepared.ID, fresh.ID)
	require.Len(t, fresh.Snapshot.Services, 1)
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, fresh.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, fresh.Snapshot, result.Order.Snapshot)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Equal(t, 1, count)
	// Completed historical records remain readable without inventing a service.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_orders SET submitted_result=submitted_result #- '{order,s" +
"napshot,services}' WHERE id=$1", result.Order.ID)
	require.NoError(t, err)
	old, err := f.H.store.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Nil(t, old.Snapshot.Services)
	raw, err := json.Marshal(old.Snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "services")
}
