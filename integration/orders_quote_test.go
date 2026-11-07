package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func readOrdersQuote(t *testing.T, f paymentFixture, token string, arguments any) (int, db.OrdersQuoteView) {
	t.Helper()
	code, raw := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/orders/quote", map[string]any{
		"protocol_version": 1, "operation_id": "quote-read", "arguments": arguments})
	var response struct {
		Result db.OrdersQuoteView `json:"result"`
	}
	if code == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &response))
	}
	return code, response.Result
}

func TestOrdersQuoteUsesCompletePricingCurrentVersionsAndOwnerEntitlement(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	change := db.OrdersCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		Document:             catalogDocument(),
	}
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	args := db.OrdersQuoteArguments{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, ExpectedVersion: 1,
		Selections: []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}}}
	code, _ := readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusConflict, code, "An old base-price catalog is not a complete quote")
	change.ExpectedVersion = 1
	change.Document.Items[0].RequiredSupplementIDs = []string{"box", "spoon"}
	change.Document.CompletePricing = &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{
		{ID: "box", Name: orders.Label{EN: "Box", RU: "Контейнер"}, PriceMinor: 10, Mode: orders.PerPortion},
		{ID: "spoon", Name: orders.Label{EN: "Spoon", RU: "Ложка"}, PriceMinor: 5, Mode: orders.PerOffer}}}
	op = uuid.New()
	status, _ = commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	code, _ = readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusConflict, code, "Stale catalog version must not recalculate silently")
	args.ExpectedVersion = 2
	code, quote := readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, int64(2493), quote.TotalMinor)
	require.Equal(t, "Europe/Minsk", quote.Timezone)
	require.WithinDuration(t, time.Now(), quote.ObservedAt, 5*time.Second)
	for _, token := range []string{f.Admin, f.Ambassador} {
		code, _ = readOrdersQuote(t, f, token, args)
		require.Equal(t, http.StatusNotFound, code, "Other actors cannot borrow the owner's assigned pass")
	}
	code, _ = readOrdersQuote(t, f, f.Owner, map[string]any{
		"event_id":               args.EventID,
		"expected_event_version": args.ExpectedEventVersion,
		"expected_version":       2,
		"selections":             []any{map[string]any{"offer_id": "lunch", "item_id": "soup", "quantity": 1.5}},
	})
	require.Equal(t, http.StatusUnprocessableEntity, code)
	ctx := context.Background()
	var catalogOperations, proofs int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_orders_catalog_operations").
			Scan(&catalogOperations),
	)
	require.Equal(t, 2, catalogOperations, "Reads create no catalog operations")
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_payment_evidence").Scan(&proofs))
	require.Zero(t, proofs)
	_, eventErr := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET name='Changed event',version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, eventErr)
	code, _ = readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusConflict, code, "Event changes invalidate a quote even while orders remains enabled")
	args.ExpectedEventVersion++
	change.ExpectedEventVersion++
	code, _ = readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusOK, code)
	change.ExpectedVersion = 2
	change.Document.Offers[0].SalesClosesAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	op = uuid.New()
	status, _ = commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	args.ExpectedVersion = 3
	code, _ = readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusConflict, code, "Selected sales window has closed")
	_, err := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	code, _ = readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusNotFound, code)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	code, _ = readOrdersQuote(t, f, f.Owner, args)
	require.Equal(t, http.StatusForbidden, code)
}
