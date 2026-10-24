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

const mealWritePath = "/internal/agent/administration/set_meal_catalog"

func TestMealCatalogAgentApprovalHistoryAndAuthority(t *testing.T) {
	t.Parallel()
	f, _, a := mealFixture(t)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	old, err := f.H.store.MealCatalogConfiguration(t.Context(), admin, a.EventID, 1)
	require.NoError(t, err)
	change := db.MealCatalogChange{
		EventID:              a.EventID,
		ExpectedEventVersion: a.ExpectedEventVersion,
		ExpectedVersion:      1,
		Document:             old.Document,
	}
	change.Document.Days[0].Dinner.Items[0].PriceMinor = 90
	operation := uuid.New()
	prepare := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": change}
	for _, token := range []string{f.Owner, f.Ambassador} {
		code, _ := adminRequest(t, f.H, token, http.MethodPost, mealWritePath+"/prepare", prepare)
		require.Equal(t, http.StatusForbidden, code)
	}
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, mealWritePath+"/prepare", prepare)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared struct {
		Arguments     db.MealCatalogChange `json:"arguments"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.NotEqual(t, uuid.Nil, prepared.Preconditions.ProposalID)
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        prepared.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": prepared.Preconditions.ProposalID},
		},
	}
	status := func(token, path string, payload any) (string, db.MealCatalog) {
		t.Helper()
		c, b := adminRequest(t, f.H, token, http.MethodPost, path, payload)
		require.Equal(t, http.StatusOK, c, string(b))
		var response struct {
			Status string         `json:"status"`
			Result db.MealCatalog `json:"result"`
		}
		require.NoError(t, json.Unmarshal(b, &response))
		return response.Status, response.Result
	}
	wrong := prepared.Arguments
	wrong.Document.CurrencyScale = 1
	body["arguments"] = wrong
	state, _ := status(f.Admin, mealWritePath, body)
	require.Equal(t, "rejected", state)
	body["arguments"] = prepared.Arguments
	code, _ = adminRequest(t, f.H, f.Owner, http.MethodPost, mealWritePath, body)
	require.Equal(t, http.StatusForbidden, code)
	state, second := status(f.Admin, mealWritePath, body)
	require.Equal(t, "succeeded", state)
	require.Equal(t, int64(2), second.Version)
	lookup := map[string]any{"protocol_version": 1, "operation_id": operation}
	state, recovered := status(f.Admin, mealWritePath+"/status", lookup)
	require.Equal(t, "succeeded", state)
	require.Equal(t, second, recovered)
	code, _ = adminRequest(t, f.H, f.Owner, http.MethodPost, mealWritePath+"/status", lookup)
	require.Equal(t, http.StatusForbidden, code)
	query := func(token, path string, version int64) (int, []byte) {
		t.Helper()
		return adminRequest(
			t,
			f.H,
			token,
			http.MethodPost,
			path,
			map[string]any{
				"protocol_version": 1,
				"operation_id":     "read",
				"arguments":        map[string]any{"event_id": a.EventID, "version": version},
			},
		)
	}
	code, raw = query(f.Owner, "/internal/agent/meals/catalog", 1)
	require.Equal(t, http.StatusOK, code, string(raw))
	var historical struct {
		Result db.MealCatalogView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &historical))
	require.Equal(t, int64(50), historical.Result.Document.Days[0].Dinner.Items[0].PriceMinor)
	require.Equal(t, "Europe/Minsk", historical.Result.Timezone)
	_, offset := historical.Result.Document.Days[0].Dinner.StartsAt.Zone()
	require.Equal(t, 3*60*60, offset)
	code, raw = query(f.Owner, "/internal/agent/meals/catalog", 0)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &historical))
	require.Equal(t, int64(90), historical.Result.Document.Days[0].Dinner.Items[0].PriceMinor)
	code, _ = query(f.Admin, "/internal/agent/meals/catalog", 0)
	require.Equal(t, http.StatusNotFound, code, "administrator cannot borrow owner entitlement")
	code, _ = query(f.Owner, "/internal/agent/administration/meal_catalog", 0)
	require.Equal(t, http.StatusForbidden, code)
	code, raw = query(f.Admin, "/internal/agent/administration/meal_catalog", 0)
	require.Equal(t, http.StatusOK, code, string(raw))
	// A later catalog version never changes an original administrative receipt.
	var thirdDocument orders.MealCatalog
	copyRaw, err := json.Marshal(second.Document)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(copyRaw, &thirdDocument))
	thirdDocument.Days[0].Dinner.Items[0].PriceMinor = 100
	publishMealCatalog(t, f, 2, thirdDocument)
	state, recovered = status(f.Admin, mealWritePath+"/status", lookup)
	require.Equal(t, "succeeded", state)
	require.Equal(t, second, recovered)
	state, recovered = status(f.Admin, mealWritePath, body)
	require.Equal(t, "succeeded", state)
	require.Equal(t, second, recovered)
	code, raw = adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		"/internal/agent/administration/discover",
		map[string]any{"protocol_version": 1, "operation_id": "discover", "arguments": map[string]any{}},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "set_meal_catalog")
	require.Contains(t, string(raw), "exact_parameters")
}
