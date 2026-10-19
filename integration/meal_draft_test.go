package integration_test

import (
	"context"
	"encoding/json"
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
	"github.com/complynx/zns-chatbot/migrations"
)

func mealDocument(now time.Time) orders.MealCatalog {
	service := orders.MealService{
		StartsAt:      now.Add(6 * time.Hour),
		EndsAt:        now.Add(7 * time.Hour),
		SalesOpensAt:  now.Add(-time.Hour),
		SalesClosesAt: now.Add(5 * time.Hour),
		Items: []orders.MealItem{
			{ID: "soup", Name: orders.Label{EN: "Soup", RU: "Суп"}, Category: orders.MealSoup, PriceMinor: 125},
			{ID: "main", Name: orders.Label{EN: "Main", RU: "Горячее"}, Category: orders.MealMain, PriceMinor: 250},
			{ID: "side", Name: orders.Label{EN: "Side", RU: "Гарнир"}, Category: orders.MealSide, PriceMinor: 150},
			{ID: "salad", Name: orders.Label{EN: "Salad", RU: "Салат"}, Category: orders.MealSalad, PriceMinor: 175},
		},
	}
	dinner := service
	dinner.StartsAt = now.Add(8 * time.Hour)
	dinner.EndsAt = now.Add(9 * time.Hour)
	dinner.Items = []orders.MealItem{
		{ID: "tea", Name: orders.Label{EN: "Tea", RU: "Чай"}, PriceMinor: 50},
		{ID: "juice", Name: orders.Label{EN: "Juice", RU: "Сок"}, PriceMinor: 75},
	}
	return orders.MealCatalog{
		Currency:      "BYN",
		CurrencyScale: 2,
		Days: []orders.MealDay{
			{
				ID:   "day_one",
				Name: orders.Label{EN: "Day one", RU: "Первый день"},
				Lunch: &orders.MealLunch{
					Service: service,
					Combos:  &orders.MealComboPrices{WithoutSoupMinor: 555, WithSoupMinor: 665},
				},
				Dinner: &dinner,
			},
		},
	}
}

func mealFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.MealDraftChange) {
	t.Helper()
	return mealFixtureWithPeers(t, newPeers(t))
}

func mealFixtureWithPeers(t *testing.T, p *peers) (paymentFixture, db.ProfileScope, db.MealDraftChange) {
	t.Helper()
	f := setupPayment(t, p)
	f.Event.Document.Modules = []string{"registrations", "meals"}
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPut,
		"/internal/admin/events/"+f.Event.ID.String(),
		map[string]any{
			"operation_id":     uuid.NewString(),
			"expected_version": f.Event.Version,
			"document":         f.Event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &f.Event))
	var now time.Time
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&now))
	now = now.UTC().Truncate(time.Microsecond)
	publishMealCatalog(t, f, 0, mealDocument(now))
	return f, ownerProfileScope(
		t,
		f.H,
		f.OwnerID,
	), db.MealDraftChange{
		OperationID:            uuid.New(),
		EventID:                f.Event.ID,
		ExpectedEventVersion:   f.Event.Version,
		ExpectedCatalogVersion: 1,
		Choices: []orders.MealDayChoice{
			{
				DayID:     "day_one",
				Lunch:     &orders.LunchChoice{Mode: orders.LunchComboWithSoup, SoupID: "soup", MainID: "main"},
				DinnerIDs: []string{"tea"},
			},
		},
	}
}

func publishMealCatalog(t *testing.T, f paymentFixture, version int64, document orders.MealCatalog) db.MealCatalog {
	t.Helper()
	scope := ownerProfileScope(t, f.H, f.AdminID)
	operation := uuid.New()
	change := db.MealCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      version,
		Document:             document,
	}
	prepared, err := f.H.store.PrepareMealCatalog(t.Context(), scope, operation, change)
	require.NoError(t, err)
	result, err := f.H.store.ChangeMealCatalog(
		t.Context(),
		scope,
		operation,
		prepared.ID,
		prepared.Arguments,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	return result
}

func completeMeal(a db.MealDraftChange) db.MealDraftChange {
	a.Choices = []orders.MealDayChoice{
		{
			DayID: "day_one",
			Lunch: &orders.LunchChoice{
				Mode:    orders.LunchComboWithSoup,
				SoupID:  "soup",
				MainID:  "main",
				SideID:  "side",
				SaladID: "salad",
			},
			DinnerIDs: []string{"tea"},
		},
	}
	return a
}

func TestMealDraftDurableIncompleteCompleteClearAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	unsaved, err := f.H.store.OwnMealDraft(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Zero(t, unsaved.Version)
	require.Nil(t, unsaved.Evaluation)
	first, err := f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.Version)
	require.False(t, first.Evaluation.Complete)
	require.Nil(t, first.Evaluation.Quote)
	require.Equal(t, int64(50), first.Evaluation.KnownAmountMinor)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	recovered, err := reopened.OwnMealDraft(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	next := completeMeal(a)
	next.OperationID = uuid.New()
	next.ExpectedDraftVersion = 1
	complete, err := reopened.SaveOwnMealDraft(ctx, scope, next)
	require.NoError(t, err)
	require.Equal(t, int64(2), complete.Version)
	require.True(t, complete.Evaluation.Complete)
	require.Equal(t, int64(715), complete.Evaluation.Quote.TotalMinor)
	replay, err := reopened.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	receipt, err := reopened.OwnMealDraftOperation(ctx, scope, a.EventID, a.OperationID)
	require.NoError(t, err)
	require.Equal(t, first, receipt)
	changed := a
	changed.Choices = []orders.MealDayChoice{{DayID: "day_one", Lunch: &orders.LunchChoice{Mode: orders.LunchNone}}}
	_, err = reopened.SaveOwnMealDraft(ctx, scope, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	next.OperationID = uuid.New()
	_, err = reopened.SaveOwnMealDraft(ctx, scope, next)
	require.ErrorIs(t, err, db.ErrConflict)
	clearing := a
	clearing.OperationID = uuid.New()
	clearing.ExpectedDraftVersion = 2
	clearing.Choices = []orders.MealDayChoice{}
	cleared, err := reopened.SaveOwnMealDraft(ctx, scope, clearing)
	require.NoError(t, err)
	require.Equal(t, int64(3), cleared.Version)
	require.True(t, cleared.Cleared)
	require.False(t, cleared.Evaluation.Complete)
	clearing.OperationID = uuid.New()
	clearing.ExpectedDraftVersion = 3
	clearing.Choices = nil
	_, err = reopened.SaveOwnMealDraft(ctx, scope, clearing)
	require.ErrorIs(t, err, orders.ErrMealChoices)
	var financial, serviceDrafts int
	require.NoError(
		t,
		reopened.Pool().
			QueryRow(ctx, `SELECT (SELECT count(*) FROM shuttle_orders)+(SELECT count(*) FROM shuttle_payment_evidence),(SELECT count(*) FROM shuttle_orders_selections)`).
			Scan(&financial, &serviceDrafts),
	)
	require.Zero(t, financial)
	require.Zero(t, serviceDrafts)
}

func TestMealDraftCatalogHistoryAndReplayDoNotRepriceOrLoseOldIDs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	a = completeMeal(a)
	first, err := f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	original, err := f.H.store.MealCatalogConfiguration(ctx, ownerProfileScope(t, f.H, f.AdminID), a.EventID, 1)
	require.NoError(t, err)
	updated := original.Document
	updated.Days[0].Dinner.Items[0].ID = "new_tea"
	updated.Days[0].Dinner.Items[0].PriceMinor = 99
	second := publishMealCatalog(t, f, 1, updated)
	require.Equal(t, int64(2), second.Version)
	replay, err := f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	saved, err := f.H.store.OwnMealDraft(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, first, saved)
	historical, err := f.H.store.OwnMealCatalog(ctx, scope, a.EventID, 1)
	require.NoError(t, err)
	require.Equal(t, "tea", historical.Document.Days[0].Dinner.Items[0].ID)
	require.Equal(t, int64(50), historical.Document.Days[0].Dinner.Items[0].PriceMinor)
	stale := a
	stale.OperationID = uuid.New()
	stale.ExpectedDraftVersion = 1
	_, err = f.H.store.SaveOwnMealDraft(ctx, scope, stale)
	require.ErrorIs(t, err, db.ErrConflict)
	stale.ExpectedCatalogVersion = 2
	stale.Choices = completeMeal(a).Choices
	stale.Choices[0].DinnerIDs = []string{"new_tea"}
	fresh, err := f.H.store.SaveOwnMealDraft(ctx, scope, stale)
	require.NoError(t, err)
	require.Equal(t, int64(764), fresh.Evaluation.Quote.TotalMinor)
	receipt, err := f.H.store.OwnMealDraftOperation(ctx, scope, a.EventID, a.OperationID)
	require.NoError(t, err)
	require.Equal(t, first, receipt)
}

func TestMealDraftTwoEditorsCASAndCanonicalSameOperation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	a = completeMeal(a)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-start
			b := a
			b.OperationID = uuid.New()
			_, err := f.H.store.SaveOwnMealDraft(ctx, scope, b)
			errs <- err
		})
	}
	close(start)
	group.Wait()
	close(errs)
	var successes, conflicts int
	for err := range errs {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, db.ErrConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	b := a
	b.OperationID = uuid.New()
	b.ExpectedDraftVersion = 1
	b.Choices = []orders.MealDayChoice{
		{
			DayID:     "day_one",
			Lunch:     &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"soup", "main"}},
			DinnerIDs: []string{"tea", "juice"},
		},
	}
	first, err := f.H.store.SaveOwnMealDraft(ctx, scope, b)
	require.NoError(t, err)
	reversed := b
	reversed.Choices = []orders.MealDayChoice{
		{
			DayID:     "day_one",
			Lunch:     &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"main", "soup"}},
			DinnerIDs: []string{"juice", "tea"},
		},
	}
	replay, err := f.H.store.SaveOwnMealDraft(ctx, scope, reversed)
	require.NoError(t, err)
	require.Equal(t, first, replay)
}

func TestMealDraftCurrentEntitlementBeforeHistoricalReplay(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	_, err := f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.NoError(t, err)
	_, err = f.H.store.OwnOrdersSelection(ctx, scope, a.EventID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "meals does not require or enable service orders")
	for name, wrong := range map[string]db.ProfileScope{"bot": {BotID: 999, Organization: scope.Organization, UserID: scope.UserID, Subject: scope.Subject}, "organization": {BotID: scope.BotID, Organization: "elsewhere", UserID: scope.UserID, Subject: scope.Subject}, "subject": {BotID: scope.BotID, Organization: scope.Organization, UserID: scope.UserID, Subject: "wrong"}, "unassigned ambassador": ownerProfileScope(t, f.H, f.AmbassadorID)} {
		_, e := f.H.store.OwnMealDraft(ctx, wrong, a.EventID)
		require.Error(t, e, name)
		_, e = f.H.store.SaveOwnMealDraft(ctx, wrong, a)
		require.Error(t, e, name)
	}
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", a.EventID)
	require.NoError(t, err)
	_, err = f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnMealDraftOperation(ctx, scope, a.EventID, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','meals'] WHERE id=$1", a.EventID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.OwnMealCatalog(ctx, scope, a.EventID, 1)
	require.Error(t, err)
	_, err = f.H.store.SaveOwnMealDraft(ctx, scope, a)
	require.Error(t, err)
}

func TestMealDraftNativeAndAgentShareReceiptsAndJournalAtomically(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, scope, a := mealFixture(t)
	session := nativePaymentSession(t, f, paymentScope(t, f.H, f.OwnerID, false), 11002)
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	first, err := f.H.store.SaveMiniAppMealDraft(ctx, deployment, session.Bearer, session.Generation, a)
	require.NoError(t, err)
	next := completeMeal(a)
	next.OperationID = uuid.New()
	next.ExpectedDraftVersion = 1
	second, err := f.H.store.SaveOwnMealDraft(ctx, scope, next)
	require.NoError(t, err)
	require.Equal(t, int64(2), second.Version)
	replay, err := f.H.store.SaveMiniAppMealDraft(ctx, deployment, session.Bearer, session.Generation, a)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	bad := session.Generation
	bad[0] ^= 1
	_, err = f.H.store.SaveMiniAppMealDraft(ctx, deployment, session.Bearer, bad, a)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.Pool().
		Exec(ctx, `CREATE FUNCTION fail_meal_journal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'qa meal journal failure'; END $$; CREATE TRIGGER fail_meal_journal BEFORE INSERT ON shuttle_miniapp_meal_draft_actions FOR EACH ROW EXECUTE FUNCTION fail_meal_journal()`)
	require.NoError(t, err)
	attempt := a
	attempt.OperationID = uuid.New()
	attempt.ExpectedDraftVersion = 2
	_, err = f.H.store.SaveMiniAppMealDraft(ctx, deployment, session.Bearer, session.Generation, attempt)
	require.ErrorContains(t, err, "qa meal journal failure")
	current, err := f.H.store.OwnMealDraft(ctx, scope, a.EventID)
	require.NoError(t, err)
	require.Equal(t, second, current)
	_, err = f.H.store.OwnMealDraftOperation(ctx, scope, a.EventID, attempt.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().Exec(ctx, "DROP TRIGGER fail_meal_journal ON shuttle_miniapp_meal_draft_actions")
	require.NoError(t, err)
	accepted, err := f.H.store.SaveMiniAppMealDraft(ctx, deployment, session.Bearer, session.Generation, attempt)
	require.NoError(t, err)
	require.Equal(t, int64(3), accepted.Version)
	var actions int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_miniapp_meal_draft_actions").Scan(&actions),
	)
	require.Equal(t, 2, actions)
}

func TestMealDraftRollbackRefusesRetainedHistory(t *testing.T) {
	t.Parallel()
	f, scope, a := mealFixture(t)
	saved, err := f.H.store.SaveOwnMealDraft(t.Context(), scope, a)
	require.NoError(t, err)
	raw, err := migrations.Files.ReadFile("00053_meal_drafts.sql")
	require.NoError(t, err)
	_, down, ok := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, ok)
	tx, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), down)
	require.ErrorContains(t, err, "Meal catalogs, drafts and operation history must be exported before rollback")
	require.NoError(t, tx.Rollback(t.Context()))
	receipt, err := f.H.store.OwnMealDraftOperation(t.Context(), scope, a.EventID, a.OperationID)
	require.NoError(t, err)
	require.Equal(t, saved, receipt)
}
