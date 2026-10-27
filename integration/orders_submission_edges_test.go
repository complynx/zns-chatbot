package integration_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func submitSavedOrder(
	t *testing.T,
	f paymentFixture,
	scope db.ProfileScope,
	args db.OrderSubmission,
) db.OrderSubmissionResult {
	t.Helper()
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	return result
}

func TestOrderPaginationRetainsIndependentOrdersAndCancelledHistory(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, draft, args := submissionFixture(t)
	expected := map[uuid.UUID]db.SubmittedOrder{}
	for i := range 3 {
		result := submitSavedOrder(t, f, scope, args)
		expected[result.Order.ID] = result.Order
		if i == 0 {
			cancelled, err := f.H.store.CancelOwnOrder(
				ctx,
				scope,
				uuid.New(),
				db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 1},
				db.OrderSourceMiniApp,
			)
			require.NoError(t, err)
			expected[result.Order.ID] = cancelled
		}
		if i < 2 {
			draft.OperationID = uuid.New()
			draft.ExpectedDraftVersion = result.Selection.Version
			saved, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, draft)
			require.NoError(t, err)
			args.ExpectedDraftVersion = saved.Version
		}
	}
	seen := map[uuid.UUID]bool{}
	after := uuid.Nil
	for pageIndex := range 3 {
		page, err := f.H.store.ListOwnOrders(ctx, scope, args.EventID, after, 1)
		require.NoError(t, err)
		require.Len(t, page.Orders, 1)
		got := page.Orders[0]
		require.False(t, seen[got.ID], "Cursor must not repeat an order")
		require.Equal(t, expected[got.ID], got)
		seen[got.ID] = true
		if pageIndex < 2 {
			require.NotNil(t, page.Next)
			after = *page.Next
		} else {
			require.Nil(t, page.Next)
		}
	}
	require.Len(t, seen, 3)
	page, err := f.H.store.ListOwnOrders(ctx, scope, args.EventID, after, 20)
	require.NoError(t, err)
	require.Len(t, page.Orders, 1, "The same cursor can recover its final page")
	_, err = f.H.store.ListOwnOrders(ctx, scope, args.EventID, uuid.Nil, 21)
	require.ErrorIs(t, err, orders.ErrSelection)
}

func TestOrderWaitlistAndForeignIdentityCannotRecoverOrMutate(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	operation := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	wrong := scope
	wrong.Subject = "foreign-subject"
	_, err = f.H.store.OwnOrderOperationByID(ctx, wrong, operation, db.OrderSubmit)
	require.Error(t, err)
	wrong = scope
	wrong.Organization = "foreign-organization"
	_, err = f.H.store.OwnOrder(ctx, wrong, args.EventID, result.Order.ID)
	require.Error(t, err)
	wrong = scope
	wrong.BotID++
	_, err = f.H.store.ListOwnOrders(ctx, wrong, args.EventID, uuid.Nil, 20)
	require.Error(t, err)
	// Retain the old pass to prove waitlist state alone denies access even if an
	// incomplete legacy repair left an associated pass row behind.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AND user_id=$2",
			args.EventID, scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.CancelOwnOrder(
		ctx,
		scope,
		uuid.New(),
		db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 1},
		db.OrderSourceMiniApp,
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnOrderOperationByID(ctx, scope, operation, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestOrderConcurrentCancellationAndNewSubmissionTargetExactOrders(t *testing.T) {
	t.Parallel()
	f, scope, draft, args := submissionFixture(t)
	ctx := t.Context()
	first := submitSavedOrder(t, f, scope, args)
	draft.OperationID = uuid.New()
	draft.ExpectedDraftVersion = first.Selection.Version
	saved, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, draft)
	require.NoError(t, err)
	args.ExpectedDraftVersion = saved.Version
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	errs := make(chan error, 2)
	var next db.OrderSubmissionResult
	var cancelled db.SubmittedOrder
	var workers sync.WaitGroup
	cancelOp := uuid.New()
	cancellation := db.OrderCancellation{EventID: args.EventID, OrderID: first.Order.ID, ExpectedVersion: 1}
	workers.Go(func() {
		var e error
		cancelled, e = f.H.store.CancelOwnOrder(ctx, scope, cancelOp, cancellation, db.OrderSourceMiniApp)
		errs <- e
	})
	workers.Go(func() {
		var e error
		next, e = f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
		errs <- e
	})
	workers.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.Equal(t, first.Order.ID, cancelled.ID)
	require.Equal(t, "cancelled", cancelled.State)
	require.NotEqual(t, first.Order.ID, next.Order.ID)
	current, err := f.H.store.OwnOrder(ctx, scope, args.EventID, next.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "submitted", current.State)
	oldCurrent, err := f.H.store.OwnOrder(ctx, scope, args.EventID, first.Order.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, oldCurrent, "The old order must remain cancelled after both concurrent operations")
	// Construct a post-cutoff fixture without changing the immutable receipts.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_orders SET submitted_at=clock_timestamp()-interval '2 minutes',"+
			"sales_closes_at=clock_timestamp()-interval '1 minute' WHERE id=$1", first.Order.ID)
	require.NoError(t, err)
	replay, err := f.H.store.CancelOwnOrder(ctx, scope, cancelOp, cancellation, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, cancelled, replay, "A lost cancellation reply remains recoverable after cutoff")
	_, err = f.H.store.CancelOwnOrder(ctx, scope, uuid.New(), cancellation, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict, "A new operation cannot repeat a completed cancellation")
}

func TestOrderConcurrentCancellationUsesOneRevision(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	result := submitSavedOrder(t, f, scope, args)
	cancellation := db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 1}
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			_, e := f.H.store.CancelOwnOrder(ctx, scope, uuid.New(), cancellation, db.OrderSourceAgent)
			errs <- e
		})
	}
	workers.Wait()
	close(errs)
	success, conflict := 0, 0
	for e := range errs {
		switch {
		case e == nil:
			success++
		case errors.Is(e, db.ErrConflict):
			conflict++
		default:
			require.NoError(t, e)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	current, err := f.H.store.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), current.Version)
	require.Equal(t, "cancelled", current.State)
}

func TestOrderCancellationSQLGuardIsExclusiveAtFrozenInstant(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	result := submitSavedOrder(t, f, scope, args)
	cutoff := result.Order.Snapshot.SalesClosesAt
	// Exercise the real SQL guard with an explicit observation instant. Public
	// store calls use database time; no production clock override is introduced.
	for _, offset := range []time.Duration{0, time.Microsecond, -time.Microsecond} {
		tx, err := f.H.store.Pool().Begin(ctx)
		require.NoError(t, err)
		q := sqlc.New(tx)
		row, err := q.CancelSubmittedOrder(
			ctx,
			sqlc.CancelSubmittedOrderParams{OrderKind: string(db.OrderKindService),
				ObservedAt:      pgtype.Timestamptz{Time: cutoff.Add(offset), Valid: true},
				ID:              result.Order.ID,
				UserID:          scope.UserID,
				EventID:         args.EventID,
				ExpectedVersion: 1,
			},
		)
		if offset < 0 {
			require.NoError(t, err)
			require.Equal(t, "cancelled", row.State)
		} else {
			require.ErrorIs(t, err, pgx.ErrNoRows)
		}
		require.NoError(t, tx.Rollback(ctx))
	}
	current, err := f.H.store.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "submitted", current.State, "SQL boundary probes are rolled back")
}

func TestOrderSnapshotRetainsNamedZoneAndDSTDeadlineInstants(t *testing.T) {
	t.Parallel()
	for _, month := range []time.Month{time.March, time.October} {
		t.Run(month.String(), func(t *testing.T) {
			t.Parallel()
			f, scope, draft, _ := submissionFixture(t)
			ctx := t.Context()
			zone, err := time.LoadLocation("Europe/Berlin")
			require.NoError(t, err)
			// Both future service windows cross a real Berlin DST transition. Keep the
			// sale open now; the store still observes actual database time.
			year := time.Now().UTC().Year() + 1
			last := time.Date(year, month+1, 0, 0, 0, 0, 0, zone)
			sunday := last.Day() - int(last.Weekday())
			start := time.Date(year, month, sunday, 1, 30, 0, 0, zone)
			finish := start.Add(3 * time.Hour)
			_, before := start.Zone()
			_, after := finish.Zone()
			require.NotEqual(t, before, after)
			_, err = f.H.store.Pool().
				Exec(ctx, "UPDATE shuttle_events SET timezone='Europe/Berlin',version=version+1 WHERE id=$1", draft.EventID)
			require.NoError(t, err)
			doc := catalogDocument()
			doc.CompletePricing = &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{}}
			doc.Items = append(
				doc.Items,
				orders.Item{ID: "tea", Name: orders.Label{EN: "Tea", RU: "Чай"}, BasePriceMinor: 100},
			)
			doc.Offers[0].ItemIDs = append(doc.Offers[0].ItemIDs, "tea")
			doc.Offers[0].StartsAt = start
			doc.Offers[0].EndsAt = finish
			doc.Offers[0].SalesClosesAt = finish
			change := db.OrdersCatalogChange{
				EventID:              draft.EventID,
				ExpectedEventVersion: draft.ExpectedEventVersion + 1,
				ExpectedVersion:      1,
				Document:             doc,
			}
			op := uuid.New()
			status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
			require.Equal(t, "succeeded", status)
			draft.OperationID = uuid.New()
			draft.ExpectedDraftVersion = 1
			draft.ExpectedEventVersion++
			draft.ExpectedCatalogVersion = 2
			saved, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, draft)
			require.NoError(t, err)
			args := db.OrderSubmission{
				EventID:                draft.EventID,
				ExpectedEventVersion:   draft.ExpectedEventVersion,
				ExpectedCatalogVersion: 2,
				ExpectedDraftVersion:   saved.Version,
			}
			result := submitSavedOrder(t, f, scope, args)
			reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			got, err := reopened.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
			require.NoError(t, err)
			require.Equal(t, "Europe/Berlin", got.Snapshot.Quote.Timezone)
			require.True(t, finish.Equal(got.Snapshot.SalesClosesAt))
			require.Equal(t, finish.Format(time.RFC3339), got.Snapshot.SalesClosesAt.In(zone).Format(time.RFC3339))
			require.Equal(t, int64(2568), got.Snapshot.Quote.TotalMinor)
		})
	}
}
