package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func webRequest(
	t *testing.T,
	handler http.Handler,
	method, path string,
	user int64,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set(
		"Authorization",
		"tma "+sandbox.WebAppInitData(telegram.User{ID: user}, "test-token", time.Now()),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestMiniAppUsesOwnerVersionPricesAndSharedAgentState(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	created, err := f.b.API.ExecuteOrder(
		ctx,
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("shuttle"),
		},
	)
	require.NoError(t, err)
	handler := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: "sandbox-festival"}).Handler()
	path := "/miniapp/api/orders/" + created.ID
	assert.Equal(t, http.StatusOK, webRequest(t, handler, http.MethodGet, path, 101, nil).Code)
	assert.Equal(t, http.StatusNotFound, webRequest(t, handler, http.MethodGet, path, 202, nil).Code)
	assert.Equal(t, http.StatusForbidden, webRequest(t, handler, http.MethodGet, path, 9999, nil).Code)
	choice := orders.ChoiceInput{
		FirstName:  "Alice",
		LastName:   "Example",
		Patronymic: "Middle",
		Customer:   "forged name",
		Extras:     map[string]json.RawMessage{"shuttle": json.RawMessage(`1`)},
		Days: map[string]orders.DayInput{
			"friday": {
				Mealtimes: map[string]orders.MealInput{
					"dinner": {Dishes: []orders.Item{{Name: "caesar", Count: 2, Price: json.RawMessage(`1`)}}},
				},
			},
		},
	}
	quote := webRequest(t, handler, http.MethodPost, "/miniapp/api/quote", 101, choice)
	require.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	var canonical orders.Choice
	require.NoError(t, json.Unmarshal(quote.Body.Bytes(), &canonical))
	assert.Equal(t, orders.Money(8830), canonical.Total, "20 salad + 3 containers + .30 fork + 65 transfer")
	save := map[string]any{"version": created.Version, "key": "web-save", "choice": choice}
	response := webRequest(t, handler, http.MethodPost, path, 101, save)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var saved orders.Order
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
	assert.Equal(t, canonical, saved.Choice)
	assert.Equal(t, "Alice Middle Example", saved.Choice.Customer)
	history, err := f.b.API.OrderHistory(ctx, "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(
		t,
		[]orders.DishChange{{Day: "friday", Meal: "dinner", Name: "caesar", Before: 0, After: 2}},
		history[1].Dishes,
	)
	assert.ElementsMatch(t, []string{"customer", "first_name", "last_name", "patronymic"}, history[1].CustomerFields)
	retry := webRequest(t, handler, http.MethodPost, path, 101, save)
	assert.JSONEq(t, response.Body.String(), retry.Body.String())
	save["key"] = "stale-save"
	assert.Equal(t, http.StatusConflict, webRequest(t, handler, http.MethodPost, path, 101, save).Code)
	assert.Equal(t, http.StatusNotFound, webRequest(t, handler, http.MethodPost, path, 202, save).Code)
	assert.Equal(t, http.StatusForbidden, webRequest(t, handler, http.MethodPost, path, 303, save).Code)
	f.model.plan = agent.Plan{
		View:        "orders",
		OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: saved.ID, Extra: "preparty"},
	}
	handle(t, f.b, message(100, 101, "add preparty to order "+saved.ID))
	updated, err := f.b.API.Order(ctx, "alice", "sandbox-festival", saved.ID)
	require.NoError(t, err)
	assert.Equal(t, saved.Choice.Days, updated.Choice.Days)
	assert.Equal(t, "Alice", updated.Choice.FirstName)
	assert.Equal(t, saved.Choice.Customer, updated.Choice.Customer)
	assert.Equal(t, orders.Money(12330), updated.Choice.Total)
	proof := orderCommand("proof", updated)
	proof.ProofFile = uploadProof(t, orders.Service{DB: f.db}, "alice")
	locked, err := f.b.API.ExecuteOrder(ctx, "alice", proof)
	require.NoError(t, err)
	save["version"] = locked.Version
	save["key"] = "locked-save"
	assert.Equal(t, http.StatusConflict, webRequest(t, handler, http.MethodPost, path, 101, save).Code)
	read := webRequest(t, handler, http.MethodGet, path, 101, nil)
	require.Equal(t, http.StatusOK, read.Code)
	var state struct {
		Editable bool `json:"editable"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &state))
	assert.False(t, state.Editable)
}

func TestMiniAppRejectsUnsignedAndInjectedActions(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handler := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: "sandbox-festival"}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/miniapp/api/orders/anything", nil)
	request.Header.Set("X-Miniapp-Owner", "alice")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	for _, field := range []string{"origin", "owner", "name", "event_id"} {
		body := map[string]any{"version": 1, "key": "forged", "choice": map[string]any{}, field: "agent"}
		result := webRequest(t, handler, http.MethodPost, "/miniapp/api/orders/anything", 101, body)
		assert.Equal(t, http.StatusBadRequest, result.Code, field)
	}
	assert.Equal(
		t,
		http.StatusForbidden,
		webRequest(t, handler, http.MethodPost, "/miniapp/api/quote", 303, orders.ChoiceInput{}).Code,
	)
}

func TestMiniAppDeadlineDisablesEditorAndRejectsSave(t *testing.T) {
	t.Parallel()
	f := setup(t)
	created, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("shuttle"),
		},
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET deadline=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	handler := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: "sandbox-festival"}).Handler()
	path := "/miniapp/api/orders/" + created.ID
	read := webRequest(t, handler, http.MethodGet, path, 101, nil)
	require.Equal(t, http.StatusOK, read.Code)
	var state struct {
		Editable bool `json:"editable"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &state))
	assert.False(t, state.Editable)
	response := webRequest(
		t,
		handler,
		http.MethodPost,
		path,
		101,
		map[string]any{"version": created.Version, "key": "expired", "choice": orderChoice("preparty")},
	)
	require.Equal(t, http.StatusConflict, response.Code)
	assert.JSONEq(t, `{"code":"deadline"}`, response.Body.String())
}
