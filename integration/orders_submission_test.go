package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func submissionFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.OrdersSelectionChange, db.OrderSubmission) {
	t.Helper()
	f, scope, draft := selectionFixture(t)
	saved, err := f.H.store.SaveOwnOrdersSelection(t.Context(), scope, draft)
	require.NoError(t, err)
	return f, scope, draft, db.OrderSubmission{
		EventID:                draft.EventID,
		ExpectedEventVersion:   draft.ExpectedEventVersion,
		ExpectedCatalogVersion: draft.ExpectedCatalogVersion,
		ExpectedDraftVersion:   saved.Version,
	}
}

func TestOrderSubmissionSharedPreparationRestartAndImmutableReceipts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, draft, args := submissionFixture(t)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	alias, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, prepared, alias)
	require.Equal(t, int64(2568), prepared.Snapshot.Quote.TotalMinor)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count, "Preparation creates no order")
	_, err = f.H.store.OwnOrderOperationByID(ctx, scope, operation, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Prepare is not a submission receipt")
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, "submitted", result.Order.State)
	require.Equal(t, prepared.Snapshot, result.Order.Snapshot)
	require.Equal(t, int64(2), result.Selection.Version)
	require.Empty(t, result.Selection.Selections)
	require.Nil(t, result.Selection.Quote)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err := reopened.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	old, err := reopened.OwnOrdersSelectionOperation(ctx, scope, args.EventID, draft.OperationID)
	require.NoError(t, err)
	require.Equal(t, int64(1), old.Version)
	require.Len(t, old.Selections, 2)
	receipt, err := reopened.OwnOrderOperationByID(ctx, scope, operation, db.OrderSubmit)
	require.NoError(t, err)
	var recovered db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(receipt.Result, &recovered))
	require.Equal(t, result, recovered)
	cancellation := db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 1}
	cancelOp := uuid.New()
	cancelled, err := reopened.CancelOwnOrder(ctx, scope, cancelOp, cancellation, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	require.Equal(t, int64(2), cancelled.Version)
	require.NotNil(t, cancelled.CancelledAt)
	current, err := reopened.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, current)
	page, err := reopened.ListOwnOrders(ctx, scope, args.EventID, uuid.Nil, 1)
	require.NoError(t, err)
	require.Equal(t, []db.SubmittedOrder{cancelled}, page.Orders)
	require.Nil(t, page.Next)
	// A new draft and catalog must not change the original completed effect.
	draft.OperationID = uuid.New()
	draft.ExpectedDraftVersion = 2
	_, err = reopened.SaveOwnOrdersSelection(ctx, scope, draft)
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, e := reopened.Pool().
			Exec(ctx, "UPDATE shuttle_orders_catalogs SET version=version+1 WHERE event_id=$1", args.EventID)
		return e
	}())
	replay, err = reopened.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, result, replay, "Canonical recovery keeps the original submitted revision")
	retryCancel, err := reopened.CancelOwnOrder(ctx, scope, cancelOp, cancellation, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, cancelled, retryCancel)
	saved, err := reopened.OwnOrdersSelection(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(3), saved.Version)
	require.Len(t, saved.Selections, 2, "Canonical replay cannot clear newer quantities")
	require.NoError(t, reopened.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_payment_evidence").Scan(&count))
	require.Zero(t, count)
}

func TestOrderSubmissionConcurrentAliasesCreateOneOrder(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceAgent)
	require.NoError(t, err)
	type outcome struct {
		result db.OrderSubmissionResult
		err    error
	}
	results := make(chan outcome, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			r, e := f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceAgent)
			results <- outcome{r, e}
		})
	}
	workers.Wait()
	close(results)
	var first *db.OrderSubmissionResult
	for got := range results {
		require.NoError(t, got.err)
		if first == nil {
			first = &got.result
		} else {
			require.Equal(t, *first, got.result)
		}
	}
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Equal(t, 1, count)
}

func TestOrderSubmissionConcurrentDraftEditNeverLosesNewQuantities(t *testing.T) {
	t.Parallel()
	f, scope, draft, args := submissionFixture(t)
	ctx := t.Context()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceAgent)
	require.NoError(t, err)
	draft.OperationID = uuid.New()
	draft.ExpectedDraftVersion = 1
	draft.Selections[0].Quantity = 3
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Go(func() {
		_, e := f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceAgent)
		errs <- e
	})
	workers.Go(func() { _, e := f.H.store.SaveOwnOrdersSelection(ctx, scope, draft); errs <- e })
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
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(2), saved.Version)
	if len(saved.Selections) > 0 {
		require.Equal(t, int64(3), saved.Selections[0].Quantity)
	}
}

func TestOrderSubmissionStaleExpiredAndChangedOperationAreRejected(t *testing.T) {
	t.Parallel()
	f, scope, draft, args := submissionFixture(t)
	ctx := t.Context()
	operation := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	changed := args
	changed.ExpectedDraftVersion++
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, operation, changed, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	// Expire only this disposable preparation while preserving its schema constraints.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_order_preparations SET created_at=clock_timestamp()-inter" +
"val '2 minutes',expires_at=clock_timestamp()-interval '1 minute' WHERE i" +
"d=$1", prepared.ID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	fresh, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.NotEqual(t, prepared.ID, fresh.ID)
	draft.OperationID = uuid.New()
	draft.ExpectedDraftVersion = 1
	_, err = f.H.store.SaveOwnOrdersSelection(ctx, scope, draft)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, fresh.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
}

func TestOrderSubmissionReceiptFailureRollsBackOrderAndDraft(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceAgent)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, "CREATE FUNCTION reject_order_receipt() RETURNS trigger LANGUAGE plpgsql " +
"AS $$ BEGIN RAISE EXCEPTION 'synthetic receipt failure'; END $$;\n CREATE" +
" TRIGGER reject_order_receipt BEFORE INSERT ON shuttle_order_operations " +
"FOR EACH ROW EXECUTE FUNCTION reject_order_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceAgent)
	require.ErrorContains(t, err, "synthetic receipt failure")
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
	saved, err := f.H.store.OwnOrdersSelection(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	require.Len(t, saved.Selections, 2)
	_, err = f.H.store.Pool().
		Exec(ctx, "DROP TRIGGER reject_order_receipt ON shuttle_order_operations; DROP FUNCTION reject_order_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err, "Rollback must leave preparation usable")
}

func TestOrderSubmissionCurrentAccessControlsReceiptsAndEffects(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	operation := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	stranger := ownerProfileScope(t, f.H, f.AmbassadorID)
	_, err = f.H.store.OwnOrder(ctx, stranger, args.EventID, result.Order.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnOrderOperationByID(ctx, stranger, operation, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnOrderOperationByID(ctx, scope, operation, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','orders'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.Error(t, err)
}

func TestOrderCancellationFrozenCutoffAndExactOperation(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := t.Context()
	operation := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, operation, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrder(ctx, scope, operation, args, uuid.New(), db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	cancel := db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 2}
	_, err = f.H.store.CancelOwnOrder(ctx, scope, uuid.New(), cancel, db.OrderSourceMiniApp)
	require.ErrorIs(t, err, db.ErrConflict)
	cancel.ExpectedVersion = 1
	// Set up a closed retained order; catalog edits cannot reopen this stored cutoff.
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_orders SET submitted_at=clock_timestamp()-interval '2 min" +
"utes',sales_closes_at=clock_timestamp()-interval '1 minute' WHERE id=$1", result.Order.ID)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnOrder(ctx, scope, uuid.New(), cancel, db.OrderSourceMiniApp)
	require.ErrorIs(t, err, db.ErrConflict)
	current, err := f.H.store.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Equal(t, "submitted", current.State)
	require.WithinDuration(t, time.Now(), current.SubmittedAt, time.Minute)
}

func TestOrderPreparationRetainsOriginalReceipt(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	ctx := context.Background()
	operation := uuid.New()
	original, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	replay, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, operation, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, original, replay)
}
