package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func selectionFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.OrdersSelectionChange) {
	t.Helper()
	return selectionFixtureWithPeers(t, newPeers(t))
}

func selectionFixtureWithPeers(t *testing.T, p *peers) (paymentFixture, db.ProfileScope, db.OrdersSelectionChange) {
	t.Helper()
	f := setupPayment(t, p)
	doc := catalogDocument()
	doc.CompletePricing = &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{}}
	doc.Items = append(doc.Items, orders.Item{ID: "tea", Name: orders.Label{EN: "Tea", RU: "Чай"}, BasePriceMinor: 100})
	doc.Offers[0].ItemIDs = append(doc.Offers[0].ItemIDs, "tea")
	change := db.OrdersCatalogChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, Document: doc}
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	return f, ownerProfileScope(
		t,
		f.H,
		f.OwnerID,
	), db.OrdersSelectionChange{
		OperationID:            uuid.New(),
		EventID:                f.Event.ID,
		ExpectedEventVersion:   f.Event.Version,
		ExpectedCatalogVersion: 1,
		Selections: []orders.Selection{
			{OfferID: "lunch", ItemID: "soup", Quantity: 2}, {OfferID: "lunch", ItemID: "tea", Quantity: 1}},
	}
}

func TestOrdersSelectionDurableRecoveryClearAndHistoricalReceipt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, a := selectionFixture(t)
	empty, err := f.H.store.OwnOrdersSelection(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Zero(t, empty.Version)
	require.Empty(t, empty.Selections)
	require.Nil(t, empty.Quote)
	first, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Version)
	require.Equal(t, int64(2568), first.Quote.TotalMinor)
	require.WithinDuration(t, time.Now(), *first.SavedAt, 5*time.Second)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	saved, err := reopened.OwnOrdersSelection(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, first, saved, "A new store recovers durable quantities and quote")
	swapped := a
	swapped.Selections = []orders.Selection{a.Selections[1], a.Selections[0]}
	replay, err := reopened.SaveOwnOrdersSelection(ctx, scope, swapped)
	require.NoError(t, err)
	require.Equal(t, first, replay, "Canonical order is independent of caller slice order")
	clearing := a
	clearing.OperationID = uuid.New()
	clearing.ExpectedDraftVersion = 1
	clearing.Selections = []orders.Selection{}
	cleared, err := reopened.SaveOwnOrdersSelection(ctx, scope, clearing)
	require.NoError(t, err)
	require.Equal(t, int64(2), cleared.Version)
	require.Empty(t, cleared.Selections)
	require.Nil(t, cleared.Quote)
	replay, err = reopened.SaveOwnOrdersSelection(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, first, replay, "Lost old receipt cannot overwrite a newer clearing")
	receipt, err := reopened.OwnOrdersSelectionOperation(ctx, scope, a.EventID, a.OperationID)
	require.NoError(t, err)
	require.Equal(t, first, receipt)
	changed := a
	changed.Selections = []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 3}}
	_, err = reopened.SaveOwnOrdersSelection(ctx, scope, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	stale := a
	stale.OperationID = uuid.New()
	_, err = reopened.SaveOwnOrdersSelection(ctx, scope, stale)
	require.ErrorIs(t, err, db.ErrConflict, "Clear retains revision; initial version cannot be reused")
	invalid := clearing
	invalid.OperationID = uuid.New()
	invalid.ExpectedDraftVersion = 2
	invalid.Selections = nil
	_, err = reopened.SaveOwnOrdersSelection(ctx, scope, invalid)
	require.ErrorIs(t, err, orders.ErrSelection, "Only explicit empty array clears")
	var drafts, receipts, evidence int
	require.NoError(
		t,
		reopened.Pool().
			QueryRow(ctx, "SELECT (SELECT count(*) FROM shuttle_orders_selections),(SELECT count(*)" +
" FROM shuttle_orders_selection_operations),(SELECT count(*) FROM shuttle" +
"_payment_evidence)").
			Scan(&drafts, &receipts, &evidence),
	)
	require.Equal(t, 1, drafts)
	require.Equal(t, 2, receipts)
	require.Zero(t, evidence)
}

func TestOrdersSelectionConcurrentSavesUseOneRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, a := selectionFixture(t)
	b := a
	b.OperationID = uuid.New()
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for _, change := range []db.OrdersSelectionChange{a, b} {
		workers.Go(func() { _, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, change); errs <- err })
	}
	workers.Wait()
	close(errs)
	var success, conflict int
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, db.ErrConflict):
			conflict++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	c := a
	c.OperationID = uuid.New()
	c.ExpectedDraftVersion = 1
	results := make(chan db.OrdersSelection, 2)
	errs = make(chan error, 2)
	for range 2 {
		workers.Go(func() {
			result, saveErr := f.H.store.SaveOwnOrdersSelection(ctx, scope, c)
			results <- result
			errs <- saveErr
		})
	}
	workers.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for result := range results {
		require.Equal(t, int64(2), result.Version)
	}
	var receipts int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders_selection_operations").Scan(&receipts),
	)
	require.Equal(t, 2, receipts)
}

func TestOrdersSelectionRechecksAccessBeforeHistoricalReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, a := selectionFixture(t)
	_, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.NoError(t, err)
	wrong := scope
	wrong.Subject = "wrong-subject"
	_, err = f.H.store.OwnOrdersSelection(ctx, wrong, a.EventID)
	require.Error(t, err)
	stranger := ownerProfileScope(t, f.H, f.AmbassadorID)
	_, err = f.H.store.OwnOrdersSelectionOperation(ctx, stranger, a.EventID, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.SaveOwnOrdersSelection(ctx, stranger, a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'],version=version+1 WHERE id=$1", a.EventID)
	require.NoError(t, err)
	_, err = f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnOrdersSelectionOperation(ctx, scope, a.EventID, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','orders'] WHERE id=$1", a.EventID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.Error(t, err)
	_, err = f.H.store.OwnOrdersSelection(ctx, scope, a.EventID)
	require.Error(t, err)
}

func TestOrdersSelectionPreservesHistoryAcrossCatalogChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, a := selectionFixture(t)
	first, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.NoError(t, err)
	doc := catalogDocument()
	doc.CompletePricing = &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{}}
	doc.Offers[0].SalesClosesAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	change := db.OrdersCatalogChange{
		EventID:              a.EventID,
		ExpectedEventVersion: a.ExpectedEventVersion,
		ExpectedVersion:      1,
		Document:             doc,
	}
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, first, saved)
	replay, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	next := a
	next.OperationID = uuid.New()
	next.ExpectedDraftVersion = 1
	_, err = f.H.store.SaveOwnOrdersSelection(ctx, scope, next)
	require.ErrorIs(t, err, db.ErrConflict)
	next.ExpectedCatalogVersion = 2
	next.Selections = []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}}
	_, err = f.H.store.SaveOwnOrdersSelection(ctx, scope, next)
	require.ErrorIs(t, err, orders.ErrSalesClosed)
	next.Selections = []orders.Selection{}
	cleared, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, next)
	require.NoError(t, err)
	require.Equal(t, int64(2), cleared.Version)
	require.Nil(t, cleared.Quote)
}

func TestOrdersSelectionPreservesRetainedDraftAndReceipt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := selectionFixture(t)
	original, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, a)
	require.NoError(t, err)
	receipt, err := f.H.store.OwnOrdersSelectionOperation(ctx, scope, a.EventID, a.OperationID)
	require.NoError(t, err)
	require.Equal(t, original, receipt)
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, original, saved)
}
