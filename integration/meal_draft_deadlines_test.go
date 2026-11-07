package integration_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func TestMealDraftClosedLunchOpenDinnerRemovalAndAdminExtension(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	view, err := f.H.store.OwnMealCatalog(ctx, scope, a.EventID, 1)
	require.NoError(t, err)
	closeAt := view.ObservedAt.Add(3 * time.Second)
	document := view.Document
	document.Days[0].Lunch.Service.SalesClosesAt = closeAt
	publishMealCatalog(t, f, 1, document)
	a = completeMeal(a)
	a.ExpectedCatalogVersion = 2
	original, err := f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var now time.Time
		e := f.H.store.Pool().QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now)
		return e == nil && !now.Before(closeAt)
	}, 5*time.Second, 20*time.Millisecond)
	next := completeMeal(a)
	next.OperationID = uuid.New()
	next.ExpectedDraftVersion = 1
	next.Choices[0].DinnerIDs = []string{"juice"}
	dinner, err := f.H.store.SaveOwnMealDraft(ctx, scope, next)
	require.NoError(t, err)
	require.Equal(t, int64(740), dinner.Evaluation.Quote.TotalMinor)
	blocked := next
	blocked.OperationID = uuid.New()
	blocked.ExpectedDraftVersion = 2
	blocked.Choices = []orders.MealDayChoice{
		{DayID: "day_one", Lunch: &orders.LunchChoice{Mode: orders.LunchNone}, DinnerIDs: []string{"juice"}},
	}
	_, err = f.H.store.SaveOwnMealDraft(ctx, scope, blocked)
	require.ErrorIs(t, err, orders.ErrSalesClosed)
	clearing := blocked
	clearing.OperationID = uuid.New()
	clearing.Choices = []orders.MealDayChoice{}
	_, err = f.H.store.SaveOwnMealDraft(ctx, scope, clearing)
	require.ErrorIs(t, err, orders.ErrSalesClosed, "Clear cannot erase a closed lunch")
	removed := document
	removed.Days = append([]orders.MealDay{}, document.Days...)
	removed.Days[0].Lunch = nil
	publishMealCatalog(t, f, 2, removed)
	removal := blocked
	removal.OperationID = uuid.New()
	removal.ExpectedCatalogVersion = 3
	removal.Choices = []orders.MealDayChoice{{DayID: "day_one", DinnerIDs: []string{"juice"}}}
	_, err = f.H.store.SaveOwnMealDraft(ctx, scope, removal)
	require.ErrorIs(t, err, orders.ErrSalesClosed)
	saved, err := f.H.store.OwnMealDraft(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, dinner, saved, "No partial save after removed closed lunch")
	// Current authorized extension reopens the unsubmitted draft; its old receipt remains history.
	document.Days[0].Lunch.Service.SalesClosesAt = view.ObservedAt.Add(time.Hour)
	publishMealCatalog(t, f, 3, document)
	blocked.OperationID = uuid.New()
	blocked.ExpectedCatalogVersion = 4
	reopened, err := f.H.store.SaveOwnMealDraft(ctx, scope, blocked)
	require.NoError(t, err)
	require.Equal(t, int64(75), reopened.Evaluation.Quote.TotalMinor)
	replay, err := f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, original, replay)
	historical, err := f.H.store.OwnMealCatalog(ctx, scope, a.EventID, 2)
	require.NoError(t, err)
	require.True(t, historical.Document.Days[0].Lunch.Service.SalesClosesAt.Equal(closeAt))
}
