package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMealOrderReadsPaginateOwnHistoryAndRecoverOriginalReceipt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, owner, args := mealSubmissionFixture(t)
	session := nativeOrderSession(t, f, owner, 11002)
	deployment := deploymentFor(owner)
	ids := map[uuid.UUID]bool{}
	var first db.OrderSubmissionResult
	var firstOp uuid.UUID
	for i := range 3 {
		op := uuid.New()
		prep, err := f.H.store.PrepareOwnMealOrderSubmission(ctx, owner, op, args, db.OrderSourceAgent)
		require.NoError(t, err)
		submitted, err := f.H.store.SubmitOwnMealOrder(ctx, owner, op, args, prep.ID, db.OrderSourceAgent)
		require.NoError(t, err)
		ids[submitted.Order.ID] = true
		if i == 0 {
			first, firstOp = submitted, op
		}
		if i < 2 {
			draft, saveErr := f.H.store.SaveOwnMealDraft(ctx, owner, db.MealDraftChange{
				OperationID: uuid.New(), EventID: args.EventID,
				ExpectedEventVersion: args.ExpectedEventVersion, ExpectedCatalogVersion: args.ExpectedCatalogVersion,
				ExpectedDraftVersion: submitted.MealDraft.Version, Choices: prep.Snapshot.Meal.Choices,
			})
			require.NoError(t, saveErr)
			args.ExpectedDraftVersion = draft.Version
		}
	}
	seen := map[uuid.UUID]bool{}
	after := uuid.Nil
	for pageNumber := range 3 {
		page, err := f.H.store.ListOwnMealOrders(ctx, owner, args.EventID, after, 1)
		require.NoError(t, err)
		native, err := f.H.store.MiniAppOwnMealOrders(ctx, deployment, session, args.EventID, after, 1)
		require.NoError(t, err)
		require.Equal(t, page, native)
		require.Len(t, page.Orders, 1)
		id := page.Orders[0].ID
		require.False(t, seen[id])
		seen[id] = true
		if pageNumber < 2 {
			require.NotNil(t, page.Next)
			after = *page.Next
		} else {
			require.Nil(t, page.Next)
		}
	}
	require.Equal(t, ids, seen)
	cancelled, err := f.H.store.CancelOwnMealOrder(ctx, owner, uuid.New(), db.OrderCancellation{
		EventID: args.EventID, OrderID: first.Order.ID, ExpectedVersion: 1,
	}, db.OrderSourceAgent)
	require.NoError(t, err)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	current, err := reopened.MiniAppOwnMealOrder(ctx, deployment, session, args.EventID, first.Order.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, current)
	original, err := reopened.OwnMealOrderOperationByID(ctx, owner, firstOp, db.OrderSubmit)
	require.NoError(t, err)
	native, err := reopened.MiniAppOwnMealOrderOperation(
		ctx,
		deployment,
		session,
		args.EventID,
		firstOp,
		db.OrderSubmit,
	)
	require.NoError(t, err)
	require.Equal(t, original, native)
	var recovered db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(original.Result, &recovered))
	require.Equal(t, first, recovered, "Current cancellation must not rewrite the submission receipt")
	_, err = reopened.OwnOrderOperationByID(ctx, owner, firstOp, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Service recovery cannot read a meal operation")
	// The fixture's sole leader place is occupied by the first owner.
	entry := registerForEvent(t, f.H, f.Ambassador, f.Event.ID, uuid.NewString(), "follower")
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, assignmentPath(f.Event.ID, entry.ID),
		assignmentBody(uuid.NewString(), f.Event, entry))
	require.Equal(t, http.StatusOK, code, string(raw))
	other := ownerProfileScope(t, f.H, f.AmbassadorID)
	_, err = reopened.OwnMealOrder(ctx, other, args.EventID, first.Order.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = reopened.OwnMealOrderOperationByID(ctx, other, firstOp, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	otherPage, err := reopened.ListOwnMealOrders(ctx, other, args.EventID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, otherPage.Orders)
	_, err = reopened.Pool().Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['orders'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	_, err = reopened.OwnMealOrderOperationByID(ctx, owner, firstOp, db.OrderSubmit)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = reopened.MiniAppOwnMealOrder(ctx, deployment, session, args.EventID, first.Order.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = reopened.ListOwnMealOrders(ctx, owner, args.EventID, uuid.Nil, 20)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	services, err := reopened.ListOwnOrders(ctx, owner, args.EventID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, services.Orders, "Enabled service module cannot list meal orders")
}

func TestMiniAppMealOrderReadsRecheckLogoutAfterEventWait(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"detail", "list", "receipt"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			f, owner, args := mealSubmissionFixture(t)
			session := nativeOrderSession(t, f, owner, 11002)
			deployment := deploymentFor(owner)
			op := uuid.New()
			prep, err := f.H.store.PrepareMiniAppMealOrderSubmission(ctx, deployment, session, op, args)
			require.NoError(t, err)
			submitted, err := f.H.store.SubmitMiniAppMealOrder(ctx, deployment, session, op, args, prep.ID)
			require.NoError(t, err)
			blocking, err := f.H.store.Pool().Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = blocking.Rollback(ctx) }()
			_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", args.EventID)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				var readErr error
				switch method {
				case "detail":
					_, readErr = f.H.store.MiniAppOwnMealOrder(
						ctx,
						deployment,
						session,
						args.EventID,
						submitted.Order.ID,
					)
				case "list":
					_, readErr = f.H.store.MiniAppOwnMealOrders(ctx, deployment, session, args.EventID, uuid.Nil, 20)
				case "receipt":
					_, readErr = f.H.store.MiniAppOwnMealOrderOperation(
						ctx,
						deployment,
						session,
						args.EventID,
						op,
						db.OrderSubmit,
					)
				}
				done <- readErr
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				e := f.H.store.Pool().QueryRow(ctx,
					"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND "+
						"pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))",
					blocking.Conn().PgConn().PID()).
					Scan(&waiting)
				return e == nil && waiting
			}, 3*time.Second, 5*time.Millisecond)
			require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, owner.BotID, session.Bearer, session.Generation))
			require.NoError(t, blocking.Commit(ctx))
			select {
			case readErr := <-done:
				require.ErrorIs(t, readErr, db.ErrMiniAppDenied)
			case <-time.After(5 * time.Second):
				t.Fatal("Native meal read did not finish after releasing the event lock")
			}
		})
	}
}
