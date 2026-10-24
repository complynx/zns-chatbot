package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func orderCapacityFixture(
	t *testing.T,
) (paymentFixture, db.ProfileScope, db.OrderSubmission, db.OrdersCatalogChange, uuid.UUID, db.OrdersCatalogPreparation) {
	t.Helper()
	f, scope, draft := selectionFixture(t)
	code, view := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusOK, code)
	doc := orders.CatalogDocument{
		Currency:        view.Currency,
		CurrencyScale:   view.CurrencyScale,
		Items:           view.Items,
		CompletePricing: view.CompletePricing,
	}
	for _, offer := range view.Offers {
		doc.Offers = append(doc.Offers, offer.Offer)
	}
	doc.CompleteCapacity = &orders.CompleteCapacity{Version: 1, Resources: []orders.CapacityResource{
		{ID: "seat", Name: orders.Label{EN: "Seat", RU: "Место"}, Ceiling: 0},
	}, Bindings: []orders.CapacityBinding{
		{OfferID: "lunch", ResourceID: "seat", Units: 1},
		{OfferID: "lunch", ItemID: "soup", ResourceID: "seat", Units: 2},
		{OfferID: "lunch", ItemID: "tea", ResourceID: "seat", Units: 3},
	}}
	change := db.OrdersCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      1,
		Document:             doc,
	}
	op := uuid.New()
	prepared := prepareCatalog(t, f, op, change)
	status, _ := commitCatalog(t, f, op, prepared)
	require.Equal(t, "succeeded", status)
	draft.ExpectedCatalogVersion = 2
	saved, err := f.H.store.SaveOwnOrdersSelection(t.Context(), scope, draft)
	require.NoError(t, err)
	return f, scope, db.OrderSubmission{
		EventID:                f.Event.ID,
		ExpectedEventVersion:   f.Event.Version,
		ExpectedCatalogVersion: 2,
		ExpectedDraftVersion:   saved.Version,
	}, change, op, prepared
}

func TestOrderCapacityFrozenRequirementsAndCatalogReplay(t *testing.T) {
	t.Parallel()
	f, scope, args, change, catalogOp, catalogPreparation := orderCapacityFixture(t)
	ctx := t.Context()
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	require.NotNil(t, view.CompleteCapacity)
	require.Zero(t, view.CompleteCapacity.Resources[0].Ceiling)
	quote, err := f.H.store.OwnOrdersQuote(
		ctx,
		scope,
		db.OrdersQuoteArguments{
			EventID:              args.EventID,
			ExpectedEventVersion: args.ExpectedEventVersion,
			ExpectedVersion:      2,
			Selections:           []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(2468), quote.TotalMinor)
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(ctx, scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.NotNil(t, prepared.Snapshot.Capacity)
	require.Equal(
		t,
		[]orders.ResourceDemand{{ResourceID: "seat", Name: orders.Label{EN: "Seat", RU: "Место"}, Units: 8}},
		prepared.Snapshot.Capacity.Demands,
	)
	result, err := f.H.store.SubmitOwnOrder(ctx, scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err, "Submission does not occupy capacity, including a zero ceiling")
	change.ExpectedVersion = 2
	change.Document.CompleteCapacity.Resources[0].Ceiling = 10
	change.Document.CompleteCapacity.Resources[0].Name.EN = "Corrected label"
	change.Document.CompleteCapacity.Bindings[1].Units = 9
	edit := uuid.New()
	status, latest := commitCatalog(t, f, edit, prepareCatalog(t, f, edit, change))
	require.Equal(t, "succeeded", status)
	status, old := commitCatalog(t, f, catalogOp, catalogPreparation)
	require.Equal(t, "succeeded", status)
	require.Equal(t, int64(2), old.Version)
	_, view = readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, latest.Version, view.Version)
	require.Equal(t, int64(10), view.CompleteCapacity.Resources[0].Ceiling)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	current, err := reopened.OwnOrder(ctx, scope, args.EventID, result.Order.ID)
	require.NoError(t, err)
	require.Equal(t, result.Order.Snapshot, current.Snapshot)
	replay, err := reopened.SubmitOwnOrder(ctx, scope, uuid.New(), args, prepared.ID, db.OrderSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	var count int
	require.NoError(t, reopened.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_payment_evidence").Scan(&count))
	require.Zero(t, count, "No payment evidence or money is created by capacity configuration/submission")
}

func TestOrderCapacityResourceIdentityCannotDisappear(t *testing.T) {
	t.Parallel()
	f, _, _, change, _, _ := orderCapacityFixture(t)
	change.ExpectedVersion = 2
	for _, policy := range []*orders.CompleteCapacity{nil, {Version: 1, Resources: []orders.CapacityResource{}, Bindings: []orders.CapacityBinding{}}} {
		change.Document.CompleteCapacity = policy
		code, _ := adminRequest(
			t,
			f.H,
			f.Admin,
			http.MethodPost,
			ordersWritePath+"/prepare",
			map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": change},
		)
		require.Equal(t, http.StatusConflict, code)
	}
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, int64(2), view.Version)
	require.Equal(t, "seat", view.CompleteCapacity.Resources[0].ID)
}

func TestOrderCapacityEmptyAndUnknownRemainDifferentInStoredOrders(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "empty"}[known], func(t *testing.T) {
			t.Parallel()
			f, scope, _, args := submissionFixture(t)
			if known {
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_orders_catalogs SET document=jsonb_set(document,'{complet" +
"e_capacity}','{\"version\":1,\"resources\":[],\"bindings\":[]}') WHERE event_i" +
"d=$1", args.EventID)
				require.NoError(t, err)
			}
			op := uuid.New()
			prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, op, args, db.OrderSourceMiniApp)
			require.NoError(t, err)
			result, err := f.H.store.SubmitOwnOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceMiniApp)
			require.NoError(t, err)
			raw, err := json.Marshal(result.Order.Snapshot)
			require.NoError(t, err)
			if known {
				require.Contains(t, string(raw), `"capacity":{"version":1,"demands":[]}`)
			} else {
				require.NotContains(t, string(raw), `"capacity"`)
			}
		})
	}
}

func TestOrderCapacityCatalogReceiptFailureRollsBackConfiguration(t *testing.T) {
	t.Parallel()
	f, _, _, change, _, _ := orderCapacityFixture(t)
	ctx := t.Context()
	change.ExpectedVersion = 2
	change.Document.CompleteCapacity.Resources[0].Ceiling = 7
	op := uuid.New()
	prepared := prepareCatalog(t, f, op, change)
	_, err := f.H.store.Pool().
		Exec(ctx, "CREATE FUNCTION fail_capacity_catalog_receipt() RETURNS trigger LANGUAGE" +
" plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic catalog receipt failure'" +
"; END $$;\n CREATE TRIGGER fail_capacity_catalog_receipt BEFORE INSERT ON" +
" shuttle_orders_catalog_operations FOR EACH ROW EXECUTE FUNCTION fail_ca" +
"pacity_catalog_receipt()")
	require.NoError(t, err)
	code, _ := adminRequest(t, f.H, f.Admin, http.MethodPost, ordersWritePath, map[string]any{
		"protocol_version": 1,
		"operation_id":     op,
		"idempotency_key":  op,
		"arguments":        prepared.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": prepared.ID},
		},
	})
	require.Equal(t, http.StatusServiceUnavailable, code)
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, int64(2), view.Version)
	require.Zero(t, view.CompleteCapacity.Resources[0].Ceiling)
	_, err = f.H.store.Pool().
		Exec(ctx, "DROP TRIGGER fail_capacity_catalog_receipt ON shuttle_orders_catalog_ope" +
"rations; DROP FUNCTION fail_capacity_catalog_receipt()")
	require.NoError(t, err)
	status, result := commitCatalog(t, f, op, prepared)
	require.Equal(t, "succeeded", status)
	require.Equal(t, int64(3), result.Version)
	require.Equal(t, int64(7), result.Document.CompleteCapacity.Resources[0].Ceiling)
}
