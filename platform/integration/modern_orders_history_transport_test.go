package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestModernLargeHistoryTransportAndRuntime(t *testing.T) {
	t.Parallel()
	f, current := modernSingleKeyOrder(t, "<&\\\"")
	for index := range 4 {
		command := orderCommand("edit", current)
		command.Key = fmt.Sprintf("history-edit-%d", index)
		command.Choice = &orders.ChoiceInput{Extras: map[string]json.RawMessage{}}
		for key := range current.Choice.Extras {
			command.Choice.Extras[key] = json.RawMessage(`0`)
		}
		var err error
		current, err = f.b.API.ExecuteOrder(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	service := orders.Service{DB: f.db}
	expected, err := service.History(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	body, err := json.Marshal(expected)
	require.NoError(t, err)
	require.Greater(t, len(body), 1<<20)
	transport := &historyAppendTransport{}
	f.b.API.HTTP = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	actual, err := f.b.API.OrderHistory(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	assert.LessOrEqual(t, transport.requests.Load(), int64(50))
	t.Logf("Complete host history read: %d bounded detail requests", transport.requests.Load())
	assertHistoryJSON(t, expected, actual)
	foreign, err := f.b.API.OrderHistory(t.Context(), "bob", current.EventID)
	require.NoError(t, err)
	assert.Empty(t, foreign)
	f.b.Model = sandbox.FixtureRemote{URL: f.fake.URL + "/lab/model"}
	f.b.Scripts = startModernRuntimeWorker(t)
	f.b.WebAppURL = "https://sandbox.invalid/orders"
	result := modernRuntimeScript(t, f, "Inspect retained order history", `return tools.orders.history.page({});`)
	assert.NotEmpty(t, result)
	t.Logf("Complete history %d bytes, %d changes; actual queued Bot.Run + external Sobek completed",
		len(body), len(actual))
}

type historyAppendTransport struct {
	once     sync.Once
	requests atomic.Int64
	append   func()
}

func (transport *historyAppendTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.Contains(request.URL.Path, "/history-recent/") {
		transport.requests.Add(1)
		if transport.append != nil {
			transport.once.Do(transport.append)
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestModernHistorySelectionStableAcrossAppend(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := orders.Service{DB: f.db}
	current, err := service.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "stable-history",
		Choice:  orderChoice("preparty"),
	})
	require.NoError(t, err)
	for range 32 {
		command := orderCommand("edit", current)
		command.Choice = orderChoice("preparty")
		current, err = service.Execute(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	key := strings.Repeat("history", 30000)
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET extras=$1 WHERE id=$2`,
		map[string]orders.Extra{key: {Price: 100}}, current.EventID)
	require.NoError(t, err)
	for range 5 {
		command := orderCommand("edit", current)
		command.Choice = orderChoice(key)
		current, err = service.Execute(t.Context(), "alice", command)
		require.NoError(t, err)
	}
	expected, err := service.History(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	require.Len(t, expected, 30)
	client := f.b.API
	client.HTTP = &http.Client{Transport: &historyAppendTransport{append: func() {
		_, appendErr := service.Execute(t.Context(), "alice", orderCommand("delete", current))
		require.NoError(t, appendErr)
	}}}
	actual, err := client.OrderHistory(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	assertHistoryJSON(t, expected, actual)
	latest, err := f.b.API.OrderHistory(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	require.Len(t, latest, 30)
	assert.Equal(t, expected[1].Version, latest[0].Version)
	assert.Equal(t, "deleted", latest[29].State)
	items, err := service.RecentHistory(t.Context(), "alice", current.EventID)
	require.NoError(t, err)
	_, err = service.HistoryDetail(t.Context(), "bob", current.EventID, items[0].ID, "")
	requireCode(t, err, "history_not_found")
}

func TestModernExpandedHistoryChunk(t *testing.T) {
	t.Parallel()
	f := setup(t)
	day := strings.Repeat("day", 700)
	menu := orders.Catalog{Dishes: map[string]orders.Definition{},
		Choices: map[string]map[string]map[string][]string{day: {"meal": {"category": {}}}}}
	meal := orders.MealInput{}
	for index := range 600 {
		name := fmt.Sprintf("dish-%d", index)
		menu.Dishes[name] = orders.Definition{Price: 100}
		menu.Choices[day]["meal"]["category"] = append(menu.Choices[day]["meal"]["category"], name)
		meal.Dishes = append(meal.Dishes, orders.Item{Name: name, Count: 1})
	}
	raw, err := json.Marshal(menu)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET menu=$1 WHERE id='sandbox-festival'`, raw)
	require.NoError(t, err)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "expanded-history",
		Choice: &orders.ChoiceInput{
			Days: map[string]orders.DayInput{day: {Mealtimes: map[string]orders.MealInput{"meal": meal}}},
		},
	})
	require.NoError(t, err)
	choice, err := json.Marshal(order.Choice)
	require.NoError(t, err)
	require.Less(t, len(choice), 256<<10)
	expected, err := (orders.Service{DB: f.db}).History(t.Context(), "alice", order.EventID)
	require.NoError(t, err)
	body, err := json.Marshal(expected)
	require.NoError(t, err)
	require.Greater(t, len(body), 1<<20)
	actual, err := f.b.API.OrderHistory(t.Context(), "alice", order.EventID)
	require.NoError(t, err)
	assertHistoryJSON(t, expected, actual)
	t.Logf(
		"Public canonical choice %d bytes expands into %d-byte immutable history; complete reconstruction verified",
		len(choice),
		len(body),
	)
}

func assertHistoryJSON(t *testing.T, expected, actual []orders.Change) {
	t.Helper()
	want, err := json.Marshal(expected)
	require.NoError(t, err)
	got, err := json.Marshal(actual)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}
