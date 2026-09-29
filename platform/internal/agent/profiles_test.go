package agent_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestProfileProposalValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		plan agent.Plan
		ok   bool
	}{
		{"read", agent.Plan{View: agent.ProfilesView}, true},
		{"set", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "Иван Иванов"}}, true},
		{"wrong_view", agent.Plan{View: agent.OrdersView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "John Smith"}}, false},
		{"passport", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "passport", Value: "x"}}, true},
		{"role", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "role", Value: "leader"}}, true},
		{"invalid_role", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "role", Value: "administrator"}}, false},
		{"delete", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "delete", Field: "legal_name", Value: "John Smith"}}, false},
		{"blank", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: " "}}, false},
		{"control", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "John\nSmith"}}, false},
		{"long", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: strings.Repeat("Ж", 201)}}, false},
		{"orders_conflict", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "John Smith"}, OrderAction: &agent.OrderProposal{Name: "create"}}, false},
		{"booking_conflict", agent.Plan{View: agent.ProfilesView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "John Smith"}, Action: &agent.Proposal{Name: "select", SlotID: "slot"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := agent.Validate(tc.plan)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestScriptedProfileIntent(t *testing.T) {
	t.Parallel()
	fixture := new(agent.ScriptedServer)
	server := httptest.NewServer(fixture.Handler())
	defer server.Close()
	model := agent.Remote{URL: server.URL}
	for _, tc := range []struct {
		text, language, name string
		frozen               bool
	}{
		{"My name is John Smith", "en", "John Smith", false},
		{"Меня зовут Иван Иванов", "ru", "Иван Иванов", false},
		{"How do I pay?", "en", "", false},
		{"Как оплатить заказ?", "ru", "", false},
		{"John Smith", "en", "", false},
		{"My name is John Smith?", "en", "", false},
		{"My name is John Or Jane", "en", "", false},
		{"He said my name is John Smith", "en", "", false},
		{"My name is \"John Smith\"", "en", "", false},
		{"Его зовут Иван Иванов", "ru", "", false},
		{"Меня зовут Иван Иванов", "ru", "", true},
	} {
		plan, err := model.Plan(
			t.Context(),
			agent.Input{
				View:     agent.ProfilesView,
				Text:     tc.text,
				Language: tc.language,
				Profile:  &agent.ProfileContext{Version: 7, Pending: "legal_name", Frozen: tc.frozen},
			},
		)
		require.NoError(t, err)
		if tc.name == "" {
			assert.Nil(t, plan.ProfileAction, tc.text)
			continue
		}
		require.NotNil(t, plan.ProfileAction, tc.text)
		assert.Equal(t, tc.name, plan.ProfileAction.Value)
		assert.Equal(t, agent.ProfilesView, plan.View)
		assert.Nil(t, plan.OrderAction)
		assert.Nil(t, plan.Action)
	}
	unsolicited, err := model.Plan(t.Context(), agent.Input{
		Text: "My name is John Smith", Profile: &agent.ProfileContext{Version: 1},
	})
	require.NoError(t, err)
	require.NotNil(t, unsolicited.ProfileAction)
	data, err := json.Marshal(agent.ProfileContext{Version: 7, Pending: "legal_name", HasLegalName: true})
	require.NoError(t, err)
	assert.JSONEq(
		t,
		`{"version":7,"pending":"legal_name","frozen":false,"has_legal_name":true,"has_passport":false,"role":""}`,
		string(data),
	)
}
