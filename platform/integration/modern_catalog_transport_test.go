package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func largeModernCatalog(t *testing.T) (*fixture, orders.Order, orders.Event) {
	t.Helper()
	f := setup(t)
	menu, err := orders.Menu()
	require.NoError(t, err)
	keys := make([]string, 520)
	for index := range keys {
		keys[index] = fmt.Sprintf("catalog-dish-%03d", index)
		menu.Dishes[keys[index]] = orders.Definition{NameRU: strings.Repeat("Ж<&", 200), Price: 100}
	}
	menu.Choices["catalog-day"] = map[string]map[string][]string{"dinner": {"main": keys}}
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET menu=$1 WHERE id='sandbox-festival'`, menu)
	require.NoError(t, err)
	choice := orderChoice("preparty")
	choice.Days = map[string]orders.DayInput{"catalog-day": {Mealtimes: map[string]orders.MealInput{
		"dinner": {Dishes: []orders.Item{{Name: keys[0], Count: 1}}},
	}}}
	_, err = f.b.API.QuoteOrder(t.Context(), "alice", "sandbox-festival", *choice)
	require.NoError(t, err)
	current, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "large-catalog", Choice: choice,
	})
	require.NoError(t, err)
	expected, err := (orders.Service{DB: f.db}).Event(t.Context(), "sandbox-festival")
	require.NoError(t, err)
	encoded, err := json.Marshal(expected)
	require.NoError(t, err)
	require.Greater(t, len(encoded), 1<<20)
	t.Logf("Valid catalog response: %d bytes; public quote/create succeeded", len(encoded))
	return f, current, expected
}

func catalogReadResponse(t *testing.T, f *fixture, owner, event, route, cursor string) (int, core.ReadChunk) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		f.b.API.Base+"/v1/order-events/"+url.PathEscape(event)+"/"+route+"?cursor="+url.QueryEscape(cursor), nil)
	require.NoError(t, err)
	if owner != "" {
		request.Header.Set("Authorization", "Bearer "+f.b.Host.Signer.Token(owner))
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	require.NoError(t, err)
	require.Less(t, len(body), 1<<20)
	var chunk core.ReadChunk
	if response.StatusCode == http.StatusOK {
		require.NoError(t, json.Unmarshal(body, &chunk))
	}
	return response.StatusCode, chunk
}

func TestModernCatalogReadContinuations(t *testing.T) {
	t.Parallel()
	f, current, expected := largeModernCatalog(t)
	status, first := catalogReadResponse(t, f, "alice", current.EventID, "catalog-read", "")
	require.Equal(t, http.StatusOK, status)
	require.True(t, first.More)
	assert.Len(t, []rune(first.JSON), 4000)
	status, _ = catalogReadResponse(t, f, "bob", current.EventID, "catalog-read", first.NextCursor)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = catalogReadResponse(t, f, "alice", "other-event", "catalog-read", first.NextCursor)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = catalogReadResponse(t, f, "", current.EventID, "catalog-read", first.NextCursor)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = catalogReadResponse(t, f, "unknown-user", current.EventID, "catalog-read", first.NextCursor)
	assert.Equal(t, http.StatusForbidden, status)
	var body strings.Builder
	cursor := ""
	for {
		var page core.ReadChunk
		status, page = catalogReadResponse(t, f, "alice", current.EventID, "catalog-transport", cursor)
		require.Equal(t, http.StatusOK, status)
		body.WriteString(page.JSON)
		if !page.More {
			break
		}
		require.NotEmpty(t, page.NextCursor)
		require.NotEqual(t, cursor, page.NextCursor)
		cursor = page.NextCursor
	}
	expectedJSON, err := json.Marshal(expected)
	require.NoError(t, err)
	assert.JSONEq(t, string(expectedJSON), body.String())
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.order_events SET deadline=deadline+interval '1 second' WHERE id=$1`,
		current.EventID,
	)
	require.NoError(t, err)
	status, _ = catalogReadResponse(t, f, "alice", current.EventID, "catalog-read", first.NextCursor)
	assert.Equal(t, http.StatusConflict, status)
	status, _ = catalogReadResponse(t, f, "alice", current.EventID, "catalog-transport", cursor)
	assert.Equal(t, http.StatusConflict, status)
}

type catalogChangeTransport struct {
	once   sync.Once
	change func()
}

func (transport *catalogChangeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/catalog-transport") && request.URL.Query().Get("cursor") != "" {
		transport.once.Do(transport.change)
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestModernCatalogHostRejectsChangingSnapshot(t *testing.T) {
	t.Parallel()
	f, current, _ := largeModernCatalog(t)
	f.b.API.HTTP = &http.Client{Transport: &catalogChangeTransport{change: func() {
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.order_events SET deadline=deadline+interval '1 second' WHERE id=$1`,
			current.EventID,
		)
		require.NoError(t, err)
	}}}
	f.b.Host.HTTP = f.b.API.HTTP
	_, err := f.b.API.OrderEvent(t.Context(), "alice", current.EventID)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	assert.Equal(t, http.StatusConflict, problem.Status)
	assert.Equal(t, "read_stale", problem.Code)
}

func TestModernLargeCatalogHostAndMiniApp(t *testing.T) {
	t.Parallel()
	f, current, expected := largeModernCatalog(t)
	actual, err := f.b.API.OrderEvent(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	expectedJSON, marshalErr := json.Marshal(expected)
	require.NoError(t, marshalErr)
	actualJSON, marshalErr := json.Marshal(actual)
	require.NoError(t, marshalErr)
	assert.JSONEq(t, string(expectedJSON), string(actualJSON))
	gateway := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: current.EventID}).Handler()
	path := "/miniapp/api/orders/" + current.ID
	response := webRequest(t, gateway, http.MethodGet, path, 101, nil)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, http.StatusNotFound, webRequest(t, gateway, http.MethodGet, path, 202, nil).Code)
	quote := webRequest(
		t,
		gateway,
		http.MethodPost,
		"/miniapp/api/quote?order_id="+current.ID,
		101,
		orderChoice("preparty"),
	)
	assert.Equal(t, http.StatusOK, quote.Code, quote.Body.String())
	save := webRequest(t, gateway, http.MethodPost, path, 101, map[string]any{
		"version": current.Version, "key": "large-catalog-save", "choice": orderChoice("preparty"),
	})
	assert.Equal(t, http.StatusOK, save.Code, save.Body.String())
}

func TestModernLargeCatalogQueuedRuntime(t *testing.T) {
	t.Parallel()
	f, _, _ := largeModernCatalog(t)
	f.b.Model = sandbox.FixtureRemote{URL: f.fake.URL + "/lab/model"}
	f.b.Scripts = startModernRuntimeWorker(t)
	f.b.WebAppURL = "https://sandbox.invalid/orders"
	result := catalogRuntimeScript(
		t,
		f,
		`const page=tools.orders.event({});return {more:page.more,length:page.json.length};`,
	)
	var page struct {
		More   bool `json:"more"`
		Length int  `json:"length"`
	}
	require.NoError(t, json.Unmarshal(result, &page))
	assert.True(t, page.More)
	assert.Positive(t, page.Length)
	assert.LessOrEqual(t, page.Length, 8000)
}
