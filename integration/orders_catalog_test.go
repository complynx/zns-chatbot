package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const ordersWritePath = "/internal/agent/administration/set_orders_catalog"

func catalogDocument() orders.CatalogDocument {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return orders.CatalogDocument{Currency: "BYN", CurrencyScale: 2, Items: []orders.Item{
		{ID: "soup", Name: orders.Label{EN: "Soup", RU: "Суп"}, BasePriceMinor: 1234}},
		Offers: []orders.Offer{{ID: "lunch", Name: orders.Label{EN: "Lunch", RU: "Обед"},
			StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), SalesOpensAt: now.Add(-time.Hour),
			SalesClosesAt: now.Add(30 * time.Minute), ItemIDs: []string{"soup"}}}}
}

func prepareCatalog(
	t *testing.T,
	f paymentFixture,
	op uuid.UUID,
	a db.OrdersCatalogChange,
) db.OrdersCatalogPreparation {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, ordersWritePath+"/prepare", map[string]any{
		"protocol_version": 1, "operation_id": op, "arguments": a})
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Arguments     db.OrdersCatalogChange `json:"arguments"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	return db.OrdersCatalogPreparation{
		ID:        response.Preconditions.ProposalID,
		Arguments: response.Arguments,
		ExpiresAt: response.ExpiresAt,
	}
}

func commitCatalog(
	t *testing.T,
	f paymentFixture,
	op uuid.UUID,
	p db.OrdersCatalogPreparation,
) (string, db.OrdersCatalog) {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, ordersWritePath, map[string]any{
		"protocol_version": 1,
		"operation_id":     op,
		"idempotency_key":  op,
		"arguments":        p.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": p.ID},
		},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Status string           `json:"status"`
		Result db.OrdersCatalog `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	return response.Status, response.Result
}

func readCatalog(t *testing.T, f paymentFixture, token string, eventID uuid.UUID) (int, db.OrdersCatalogView) {
	t.Helper()
	code, raw := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/orders/catalog", map[string]any{
		"protocol_version": 1, "operation_id": "catalog-read", "arguments": map[string]any{"event_id": eventID}})
	var response struct {
		Result db.OrdersCatalogView `json:"result"`
	}
	if code == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &response))
	}
	return code, response.Result
}

func TestOrdersCatalogExactEditAndAssignedUnpaidOwnerRead(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	a := db.OrdersCatalogChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, Document: catalogDocument()}
	op := uuid.New()
	p := prepareCatalog(t, f, op, a)
	code, _ := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusNotFound, code, "Preparation must not publish a catalog")
	expiredOperation := uuid.New()
	expired := prepareCatalog(t, f, expiredOperation, a)
	_, expiryErr := f.H.store.Pool().Exec(context.Background(),
		"UPDATE shuttle_orders_catalog_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", expired.ID)
	require.NoError(t, expiryErr)
	expiredStatus, _ := commitCatalog(t, f, expiredOperation, expired)
	require.Equal(t, "rejected", expiredStatus, "Expired approval cannot publish its first result")
	code, _ = readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusNotFound, code)
	status, first := commitCatalog(t, f, op, p)
	require.Equal(t, "succeeded", status)
	require.Equal(t, int64(1), first.Version)
	code, view := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusOK, code)
	require.True(t, view.PricesAreBase)
	require.True(t, view.Offers[0].SalesOpen)
	require.Equal(t, "Europe/Minsk", view.Timezone)
	require.Equal(t, "Суп", view.Items[0].Name.RU)
	require.Equal(t, int64(1234), view.Items[0].BasePriceMinor)
	code, _ = readCatalog(t, f, f.Admin, f.Event.ID)
	require.Equal(t, http.StatusNotFound, code, "Global admin cannot bypass own pass entitlement")
	code, _ = readCatalog(t, f, f.Ambassador, f.Event.ID)
	require.Equal(t, http.StatusNotFound, code)
	a.ExpectedVersion = first.Version
	a.Document.Items[0].BasePriceMinor = 2345
	a.Document.Offers[0].SalesOpensAt = time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	a.Document.Offers[0].SalesClosesAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	op2 := uuid.New()
	p2 := prepareCatalog(t, f, op2, a)
	status, _ = commitCatalog(t, f, uuid.New(), p2)
	require.Equal(t, "rejected", status, "Proposal is bound to its exact operation")
	status, second := commitCatalog(t, f, op2, p2)
	require.Equal(t, "succeeded", status)
	require.Equal(t, int64(2), second.Version)
	_, err := f.H.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_orders_catalog_preparations SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1", p.ID)
	require.NoError(t, err)
	status, replay := commitCatalog(t, f, op, p)
	require.Equal(t, "succeeded", status)
	require.Equal(t, first, replay, "Original successful receipt survives expiry and later edits")
	code, view = readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, int64(2345), view.Items[0].BasePriceMinor)
	require.False(t, view.Offers[0].SalesOpen, "Closed catalog remains readable")
	changed := p
	changed.Arguments.ExpectedVersion = first.Version
	changed.Arguments.Document = catalogDocument()
	status, _ = commitCatalog(t, f, op, changed)
	require.Equal(t, "rejected", status)
}

func TestOrdersCatalogRechecksModuleBindingEventVersionAndApproval(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	a := db.OrdersCatalogChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, Document: catalogDocument()}
	op := uuid.New()
	p := prepareCatalog(t, f, op, a)
	ctx := context.Background()
	_, err := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	status, _ := commitCatalog(t, f, op, p)
	require.Equal(t, "rejected", status)
	a.ExpectedEventVersion++
	op = uuid.New()
	p = prepareCatalog(t, f, op, a)
	status, _ = commitCatalog(t, f, op, p)
	require.Equal(t, "succeeded", status, "Disabled catalogs may be configured before module enablement")
	code, _ := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusNotFound, code)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	code, _ = readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusOK, code)
	scope := ownerProfileScope(t, f.H, f.OwnerID)
	scope.Subject = "another-subject"
	_, err = f.H.store.OwnOrdersCatalog(ctx, scope, f.Event.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	code, _ = readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusForbidden, code)
}
