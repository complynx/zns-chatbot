package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestContextBudgetIncludesJSONEscaping(t *testing.T) {
	t.Parallel()
	var received agent.Input
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := io.ReadAll(r.Body)
		assert.NoError(t, e)

		if len(raw) > 60000 {
			t.Errorf("oversized %d", len(raw))
		}
		{
			e = json.Unmarshal(raw, &received)
			assert.NoError(t, e)
		}

		api.JSON(w, http.StatusOK, agent.Plan{Text: "continue", View: "workflow"})
	}))
	defer server.Close()
	in := agent.Input{
		Text:     "finish",
		Workflow: workflow.Workflow{State: "draft", Version: 7},
		Catalog:  []workflow.Slot{},
	}
	for range 30 {
		raw, _ := json.Marshal(strings.Repeat("<\"\n", 1600))
		in.History = append(in.History, agent.Event{Kind: "input", Content: raw})
	}
	recent, _ := json.Marshal("most recent event")
	in.History = append(in.History, agent.Event{Kind: "result", Content: recent})
	{
		_, e := (agent.Remote{URL: server.URL}).Plan(context.Background(), in)
		require.NoError(t, e)
	}

	if received.Text != "finish" || received.Workflow.Version != 7 {
		t.Fatal("lost current state")
	}
	if len(received.History) == 0 || len(received.History) >= len(in.History) {
		t.Fatal("history was not bounded")
	}
	if string(received.History[len(received.History)-1].Content) != string(recent) {
		t.Fatal("newest context lost")
	}
}
func TestScriptedModelAndHostileModes(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer((&agent.ScriptedServer{}).Handler())
	defer s.Close()
	m := agent.Remote{URL: s.URL}
	for _, state := range []string{"empty", "draft", "booked"} {
		p, e := m.Plan(context.Background(), agent.Input{Workflow: workflow.Workflow{State: state}})
		if e != nil || p.Text == "" || p.Action != nil {
			t.Fatalf("%v %v", p, e)
		}
	}
	p, e := m.Plan(
		context.Background(),
		agent.Input{
			Business: &agent.BusinessCapabilities{CanBook: true},
			Text:     "выбери массаж",
			Catalog:  []workflow.Slot{{ID: "massage-1"}},
		},
	)
	if e != nil || p.Action == nil || p.Action.Name != "select" {
		t.Fatalf("%v %v", p, e)
	}
	for _, mode := range []string{"forbidden", "fail"} {
		r, modeError := http.Post(s.URL+"/mode", "application/json", strings.NewReader(`{"mode":"`+mode+`"}`))
		require.NoError(t, modeError)

		r.Body.Close()
		if _, e = m.Plan(context.Background(), agent.Input{}); e == nil {
			t.Fatal("accepted", mode)
		}
	}
}

func TestMealHistoryIsTrimmedWithoutLosingLatestChange(t *testing.T) {
	t.Parallel()
	var received agent.Input
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.LessOrEqual(t, len(raw), 60000)
		assert.NoError(t, json.Unmarshal(raw, &received))
		api.JSON(w, http.StatusOK, agent.Plan{View: "orders"})
	}))
	t.Cleanup(server.Close)
	input := agent.Input{Text: "finish", View: "orders"}
	for index := range 30 {
		input.OrderHistory = append(input.OrderHistory, orders.Change{OrderID: "own", Version: int64(index + 1),
			Dishes: []orders.DishChange{{Name: strings.Repeat("<", 2000), Before: 2, After: 1}}})
	}
	_, err := (agent.Remote{URL: server.URL}).Plan(t.Context(), input)
	require.NoError(t, err)
	require.NotEmpty(t, received.OrderHistory)
	assert.Less(t, len(received.OrderHistory), len(input.OrderHistory))
	assert.EqualValues(t, 30, received.OrderHistory[len(received.OrderHistory)-1].Version)
	assert.Equal(t, "finish", received.Text)
}

func TestScriptedModelAnswersFromManualOrderHistory(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer((&agent.ScriptedServer{}).Handler())
	t.Cleanup(server.Close)
	model := agent.Remote{URL: server.URL}
	input := agent.Input{View: "orders", Text: "Что я только что убрал?", OrderHistory: []orders.Change{
		{
			OrderID:      "own",
			Origin:       "manual",
			Action:       "edit",
			BeforeExtras: map[string]orders.Money{"shuttle": 6500, "preparty": 3500},
			Extras:       map[string]orders.Money{"preparty": 3500},
		},
	}, History: []agent.Event{{Kind: "input", Content: json.RawMessage(`{"extras":{"preparty":0}}`)}}}

	plan, err := model.Plan(t.Context(), input)
	require.NoError(t, err)
	assert.Contains(t, plan.Text, "Трансфер")
	assert.NotContains(t, plan.Text, "Препати")
	assert.Nil(t, plan.OrderAction)
	input.OrderHistory[0].Origin = "agent"
	plan, err = model.Plan(t.Context(), input)
	require.NoError(t, err)
	assert.Contains(t, plan.Text, "Трансфер", "a user-requested agent removal is also part of the shared history")
	input.OrderHistory[0].BeforeExtras = nil
	input.OrderHistory[0].Dishes = []orders.DishChange{
		{Day: "friday", Meal: "dinner", Name: "caesar", Before: 3, After: 2},
	}
	plan, err = model.Plan(t.Context(), input)
	require.NoError(t, err)
	assert.Contains(t, plan.Text, "Цезарь")
	assert.Contains(t, plan.Text, "1 порц.")
	input.OrderHistory[0].Origin = "system"
	plan, err = model.Plan(t.Context(), input)
	require.NoError(t, err)
	assert.Contains(t, plan.Text, "нет подтверждённого", "capacity changes must not be attributed to the user")
	input.OrderHistory = nil
	plan, err = model.Plan(t.Context(), input)
	require.NoError(t, err)
	assert.Contains(t, plan.Text, "нет подтверждённого")
}

func TestCancellationRequestOffersActualButtonWithoutClaimingWrite(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer((&agent.ScriptedServer{}).Handler())
	defer s.Close()
	p, e := (agent.Remote{URL: s.URL}).Plan(
		context.Background(),
		agent.Input{Text: "отмени бронирование", Workflow: workflow.Workflow{State: "booked"}},
	)
	if e != nil || p.Action != nil || !strings.Contains(p.Text, "Отменить заявку") ||
		!strings.Contains(p.Text, "Пока она не отменена") {
		t.Fatalf("%+v %v", p, e)
	}
}

func TestDraftWithoutCapacityGetsUsefulGuidance(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer((&agent.ScriptedServer{}).Handler())
	defer s.Close()
	p, e := (agent.Remote{URL: s.URL}).Plan(
		context.Background(),
		agent.Input{
			Text:     "помоги закончить",
			Workflow: workflow.Workflow{State: "draft", SlotID: "massage-1"},
			Catalog:  []workflow.Slot{{ID: "massage-1", Remaining: 0}},
		},
	)
	if e != nil || p.Action != nil || !strings.Contains(p.Text, "нет свободных мест") {
		t.Fatalf("%+v %v", p, e)
	}
}
