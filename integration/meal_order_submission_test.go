package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func mealSubmissionFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.OrderSubmission) {
	t.Helper()
	f, scope, change := mealFixture(t)
	catalog, err := f.H.store.OwnMealCatalog(t.Context(), scope, f.Event.ID, 0)
	require.NoError(t, err)
	deadline := catalog.ObservedAt.Add(2 * time.Hour).Truncate(time.Microsecond)
	catalog.Document.PaymentDeadline = &deadline
	published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
	change.ExpectedCatalogVersion = published.Version
	change.Choices[0].Lunch.SideID = "side"
	change.Choices[0].Lunch.SaladID = "salad"
	draft, err := f.H.store.SaveOwnMealDraft(t.Context(), scope, change)
	require.NoError(t, err)
	return f, scope, db.OrderSubmission{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		ExpectedCatalogVersion: published.Version, ExpectedDraftVersion: draft.Version}
}

func mealOrderCounts(t *testing.T, f paymentFixture, ordersCount, preparations int) {
	t.Helper()
	var actualOrders, actualPreparations, evidence, allocations int
	err := f.H.store.Pool().QueryRow(t.Context(), `SELECT
	 (SELECT count(*) FROM shuttle_orders),
	 (SELECT count(*) FROM shuttle_order_preparations),
	 (SELECT count(*) FROM shuttle_order_evidence),
	 (SELECT count(*) FROM shuttle_order_allocations)`).Scan(&actualOrders, &actualPreparations, &evidence, &allocations)
	require.NoError(t, err)
	require.Equal(t, ordersCount, actualOrders)
	require.Equal(t, preparations, actualPreparations)
	require.Zero(t, evidence)
	require.Zero(t, allocations)
}

func TestMealOrderSubmissionSharedReceiptsAndReopen(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, args := mealSubmissionFixture(t)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnMealOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.NotNil(t, prepared.Snapshot.Meal)
	require.NotNil(t, prepared.Snapshot.Capacity)
	require.Empty(t, prepared.Snapshot.Capacity.Demands)
	require.Equal(t, int64(715), prepared.Snapshot.Quote.TotalMinor)
	require.Equal(t, int64(715), prepared.Snapshot.Quote.Items[0].AmountMinor)
	require.Equal(t, int64(665), prepared.Snapshot.Meal.Lines[0].AmountMinor)
	require.Equal(t, *prepared.Snapshot.Meal.PaymentDeadline, prepared.Snapshot.SalesClosesAt)
	alias, err := f.H.store.PrepareOwnMealOrderSubmission(ctx, scope, uuid.New(), args, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, prepared, alias)
	mealOrderCounts(t, f, 0, 1)
	result, err := f.H.store.SubmitOwnMealOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, db.OrderKindMeal, result.Order.OrderKind)
	require.Nil(t, result.Selection)
	require.True(t, result.MealDraft.Cleared)
	require.Equal(t, args.ExpectedDraftVersion+1, result.MealDraft.Version)
	mealOrderCounts(t, f, 1, 1)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err := reopened.SubmitOwnMealOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	canonical, err := reopened.SubmitOwnMealOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, result, canonical)
	receipt, err := reopened.OwnMealOrderOperation(ctx, scope, args.EventID, op, db.OrderSubmit)
	require.NoError(t, err)
	var recovered db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(receipt.Result, &recovered))
	require.Equal(t, result, recovered)
	_, err = reopened.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Meals entitlement never grants service-order access")
	mealOrderCounts(t, f, 1, 1)
}

func TestMealOrderSubmissionRejectsEmptyAndUnconfiguredDeadline(t *testing.T) {
	t.Parallel()
	for _, empty := range []bool{true, false} {
		name := "missing_deadline"
		if empty {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, scope, change := mealFixture(t)
			if empty {
				change.Choices[0].Lunch = &orders.LunchChoice{Mode: orders.LunchNone}
				change.Choices[0].DinnerIDs = []string{}
			} else {
				change.Choices[0].Lunch.SideID = "side"
				change.Choices[0].Lunch.SaladID = "salad"
			}
			draft, err := f.H.store.SaveOwnMealDraft(t.Context(), scope, change)
			require.NoError(t, err)
			args := db.OrderSubmission{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
				ExpectedCatalogVersion: change.ExpectedCatalogVersion, ExpectedDraftVersion: draft.Version}
			_, err = f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, uuid.New(), args, db.OrderSourceAgent)
			if empty {
				require.ErrorIs(t, err, db.ErrEmptyMealOrder)
			} else {
				require.ErrorIs(t, err, db.ErrMealDeadlineMissing)
			}
			mealOrderCounts(t, f, 0, 0)
			current, err := f.H.store.OwnMealDraft(t.Context(), scope, f.Event.ID)
			require.NoError(t, err)
			require.Equal(t, draft, current)
		})
	}
}

func TestMealOrderSubmissionConcurrentNativeAndAgent(t *testing.T) {
	t.Parallel()
	f, scope, args := mealSubmissionFixture(t)
	op1, op2 := uuid.New(), uuid.New()
	p1, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op1, args, db.OrderSourceAgent)
	require.NoError(t, err)
	p2, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op2, args, db.OrderSourceMiniApp)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]db.OrderSubmissionResult, 2)
	errors := make([]error, 2)
	for index, op := range []uuid.UUID{op1, op2} {
		wg.Go(func() {
			source, proposal := db.OrderSourceAgent, p1.ID
			if index == 1 {
				source, proposal = db.OrderSourceMiniApp, p2.ID
			}
			results[index], errors[index] = f.H.store.SubmitOwnMealOrder(t.Context(), scope, op, args, proposal, source)
		})
	}
	wg.Wait()
	for _, err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, results[0], results[1])
	mealOrderCounts(t, f, 1, 1)
}

func TestMealOrderSubmissionKeepsNewerDraftAndChecksCurrentAccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, args := mealSubmissionFixture(t)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnMealOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnMealOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	choices := append([]orders.MealDayChoice{}, prepared.Snapshot.Meal.Choices...)
	choices[0].DinnerIDs = []string{"juice"}
	newDraft, err := f.H.store.SaveOwnMealDraft(ctx, scope, db.MealDraftChange{
		OperationID:            uuid.New(),
		EventID:                args.EventID,
		ExpectedEventVersion:   args.ExpectedEventVersion,
		ExpectedCatalogVersion: args.ExpectedCatalogVersion,
		ExpectedDraftVersion:   result.MealDraft.Version,
		Choices:                choices,
	})
	require.NoError(t, err)
	replay, err := f.H.store.SubmitOwnMealOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	current, err := f.H.store.OwnMealDraft(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, newDraft, current)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnMealOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows, "A historical receipt must not bypass revoked ownership")
}

func TestMealOrderSubmissionRejectsChangedTermsAndExpiredApproval(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{true, false} {
		name := "expired_approval"
		if changed {
			name = "changed_catalog"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, scope, args := mealSubmissionFixture(t)
			op := uuid.New()
			prepared, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
			require.NoError(t, err)
			if changed {
				catalog, readErr := f.H.store.OwnMealCatalog(t.Context(), scope, args.EventID, 0)
				require.NoError(t, readErr)
				deadline := catalog.Document.PaymentDeadline.Add(time.Hour)
				catalog.Document.PaymentDeadline = &deadline
				publishMealCatalog(t, f, catalog.Version, catalog.Document)
			} else {
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_order_preparations SET created_at=clock_timestamp()-inter" +
"val '10 minutes', expires_at=clock_timestamp()-interval '1 second' WHERE" +
" id=$1", prepared.ID)
				require.NoError(t, err)
			}
			_, err = f.H.store.SubmitOwnMealOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
			require.ErrorIs(t, err, db.ErrConflict)
			mealOrderCounts(t, f, 0, 1)
			current, err := f.H.store.OwnMealDraft(t.Context(), scope, args.EventID)
			require.NoError(t, err)
			require.Equal(t, args.ExpectedDraftVersion, current.Version)
		})
	}
}

func TestMealOrderSubmissionFreeDishIsNotEmptyComposition(t *testing.T) {
	t.Parallel()
	f, scope, change := mealFixture(t)
	catalog, err := f.H.store.OwnMealCatalog(t.Context(), scope, f.Event.ID, 0)
	require.NoError(t, err)
	deadline := catalog.ObservedAt.Add(2 * time.Hour).Truncate(time.Microsecond)
	catalog.Document.PaymentDeadline = &deadline
	catalog.Document.Days[0].Dinner.Items[0].PriceMinor = 0
	published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
	change.ExpectedCatalogVersion = published.Version
	change.Choices[0].Lunch = &orders.LunchChoice{Mode: orders.LunchNone}
	draft, err := f.H.store.SaveOwnMealDraft(t.Context(), scope, change)
	require.NoError(t, err)
	args := db.OrderSubmission{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		ExpectedCatalogVersion: published.Version, ExpectedDraftVersion: draft.Version}
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Zero(t, prepared.Snapshot.Quote.TotalMinor)
	require.Len(t, prepared.Snapshot.Meal.Lines, 1)
	_, err = f.H.store.SubmitOwnMealOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	mealOrderCounts(t, f, 1, 1)
}

func TestMealOrderSubmissionOperationCannotBecomeServiceOrder(t *testing.T) {
	t.Parallel()
	f, scope, args := mealSubmissionFixture(t)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	f.Event.Document.Modules = append(f.Event.Document.Modules, "orders")
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPut,
		"/internal/admin/events/"+f.Event.ID.String(),
		map[string]any{
			"operation_id": uuid.NewString(), "expected_version": f.Event.Version, "document": f.Event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	_, err = f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.SubmitOwnOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.OwnOrderOperation(t.Context(), scope, args.EventID, op, db.OrderPrepare)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	mealOrderCounts(t, f, 0, 1)
}

func TestMealOrderSubmissionLargeCompositionFitsSharedReceipt(t *testing.T) {
	t.Parallel()
	f, scope, change := mealFixture(t)
	catalog, err := f.H.store.OwnMealCatalog(t.Context(), scope, f.Event.ID, 0)
	require.NoError(t, err)
	deadline := catalog.ObservedAt.Add(2 * time.Hour).Truncate(time.Microsecond)
	catalog.Document.PaymentDeadline = &deadline
	day := &catalog.Document.Days[0]
	day.Lunch = nil
	day.Name = orders.Label{
		EN: strings.Repeat("D", orders.MaxLabelRunes),
		RU: strings.Repeat("d", orders.MaxLabelRunes),
	}
	day.Dinner.Items = []orders.MealItem{}
	ids := []string{}
	for i := range orders.MaxItems {
		id := fmt.Sprintf("dish_%03d", i)
		ids = append(ids, id)
		day.Dinner.Items = append(day.Dinner.Items, orders.MealItem{
			ID: id,
			Name: orders.Label{
				EN: strings.Repeat("A", orders.MaxLabelRunes),
				RU: strings.Repeat("a", orders.MaxLabelRunes),
			},
			PriceMinor: 1,
		})
	}
	published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
	change.ExpectedCatalogVersion = published.Version
	change.Choices = []orders.MealDayChoice{{DayID: day.ID, DinnerIDs: ids}}
	draft, err := f.H.store.SaveOwnMealDraft(t.Context(), scope, change)
	require.NoError(t, err)
	current, err := f.H.store.OwnMealCatalog(t.Context(), scope, f.Event.ID, 0)
	require.NoError(t, err)
	_, err = db.FreezeMealDraft(current, draft)
	require.NoError(t, err, "A supported composition already fits the frozen-terms contract")
	args := db.OrderSubmission{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		ExpectedCatalogVersion: published.Version, ExpectedDraftVersion: draft.Version}
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnMealOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Len(t, result.Order.Snapshot.Meal.Lines, orders.MaxItems)
	require.Equal(t, int64(orders.MaxItems), result.Order.Snapshot.Quote.TotalMinor)
	mealOrderCounts(t, f, 1, 1)
}
