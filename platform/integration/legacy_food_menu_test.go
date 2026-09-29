package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestFoodLegacyMenuWireBindsOwnerAndReplay(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	handler := (miniapp.Gateway{API: f.b.API, Token: "test-token"}).Handler()
	signed := sandbox.WebAppInitData(telegram.User{ID: 101}, "test-token", time.Now())
	path := "/menu?pass_key=food-bot&order_id=another-owner&orig_chat_id=202&initData=" + url.QueryEscape(signed)
	raw := []byte(`{"friday":{"lunch":{"type":"individual-items","items":[0]}}}`)
	send := func(target string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, target, bytes.NewReader(raw)))
		return response
	}
	require.Equal(t, http.StatusUnauthorized, send("/menu?pass_key=food-bot").Code)
	first := send(path)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.JSONEq(t, `{"message":"Order saved successfully"}`, first.Body.String())
	order, err := service.Get(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	require.Equal(t, "alice", order.Owner)
	require.Equal(t, http.StatusOK, send(path).Code)
	changed, err := service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{
			EventID:  order.EventID,
			OrderID:  order.ID,
			Version:  order.Version,
			Name:     "toggle_activity",
			Activity: "open",
			Key:      "later-edit",
		},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, send(path).Code)
	current, err := service.Get(t.Context(), "alice", "food-bot", order.ID)
	require.NoError(t, err)
	require.Equal(t, changed.Version, current.Version)
	_, err = service.DB.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, send(path).Code)
}

func TestFoodMenuAuthenticatesOwnerAndPreservesVersion(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	order, err := service.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{EventID: "food-bot", Name: "save_meals", Key: "initial", Meals: legacyfood.MealSelection{}},
	)
	require.NoError(t, err)
	handler := (miniapp.Gateway{API: f.b.API, Token: "test-token"}).Handler()
	shell := httptest.NewRecorder()
	handler.ServeHTTP(shell, httptest.NewRequest(http.MethodGet, "/menu?pass_key=food-bot&order_id="+order.ID, nil))
	require.Equal(t, http.StatusOK, shell.Code)
	require.NotContains(t, shell.Body.String(), order.ID)
	path := "/miniapp/api/food?pass_key=food-bot&order_id=" + order.ID
	unsigned := httptest.NewRecorder()
	handler.ServeHTTP(unsigned, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusUnauthorized, unsigned.Code)
	require.Equal(t, http.StatusForbidden, webRequest(t, handler, http.MethodGet, path, 202, nil).Code)
	require.Equal(t, http.StatusOK, webRequest(t, handler, http.MethodGet, path, 101, nil).Code)
	save := legacyfood.Command{
		EventID: "food-bot",
		OrderID: order.ID,
		Name:    "save_meals",
		Version: order.Version,
		Key:     "web-save",
		Meals: legacyfood.MealSelection{
			"friday": {Lunch: &legacyfood.LunchSelection{Type: "individual-items", Items: json.RawMessage(`[0]`)}},
		},
	}
	response := webRequest(t, handler, http.MethodPost, "/miniapp/api/food", 101, save)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	replay := webRequest(t, handler, http.MethodPost, "/miniapp/api/food", 101, save)
	require.JSONEq(t, response.Body.String(), replay.Body.String())
	save.Key = "stale"
	require.Equal(t, http.StatusConflict, webRequest(t, handler, http.MethodPost, "/miniapp/api/food", 101, save).Code)
	save.Name = "accept"
	require.Equal(
		t,
		http.StatusBadRequest,
		webRequest(t, handler, http.MethodPost, "/miniapp/api/food", 101, save).Code,
	)
	for _, file := range []string{"soup_rassolnik.jpg", "vegetables.jpg"} {
		image := httptest.NewRecorder()
		handler.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/miniapp/foodphotos/"+file, nil))
		require.Equal(t, http.StatusOK, image.Code)
		require.NotEmpty(t, image.Body.Bytes())
	}
}
