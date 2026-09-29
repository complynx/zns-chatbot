package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func profileCard(t *testing.T, f *fixture) telegram.Message {
	t.Helper()
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT message_id FROM bot.order_cards WHERE owner='alice' AND card_key='profile'`).
			Scan(&id),
	)
	for _, card := range chatMessages(t, f, 101) {
		if card.ID == id {
			return card
		}
	}
	t.Fatal("profile card missing")
	return telegram.Message{}
}

func TestProfileQuestionDoesNotConsumePendingName(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handle(t, f.b, message(1000, 101, "/name"))
	originalCard := profileCard(t, f)
	pending, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, "legal_name", pending.Pending)
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Use the payment methods button."}
	handle(t, f.b, message(1001, 101, "How do I pay?"))
	unchanged, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, pending, unchanged)
	require.NotNil(t, f.model.input.Profile)
	assert.Equal(t, "legal_name", f.model.input.Profile.Pending)
	f.model.plan = agent.Plan{View: agent.ProfilesView, Text: "Proposed name.",
		ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "Avery Example"}}
	handle(t, f.b, message(1002, 101, "Avery Example"))
	saved, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "Avery Example", saved.LegalName)
	assert.Empty(t, saved.Pending)
	assert.Equal(t, pending.Version+1, saved.Version)
	updatedCard := profileCard(t, f)
	assert.Equal(t, originalCard.ID, updatedCard.ID)
	assert.Contains(t, updatedCard.Text, "Avery Example")
	newer := profileCommand("set", "legal_name", "newer-name", saved)
	newer.Value = "Morgan Sample"
	_, err = f.b.API.ExecutePassProfile(t.Context(), "alice", newer)
	require.NoError(t, err)
	calls := f.model.calls
	handle(t, f.b, message(1002, 101, "Avery Example"))
	assert.Equal(t, calls, f.model.calls)
	replayed, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "Morgan Sample", replayed.LegalName)
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Current orders."}
	handle(t, f.b, message(1003, 101, "Show my orders"))
	history, err := json.Marshal(f.model.input.History)
	require.NoError(t, err)
	assert.NotContains(t, string(history), "Avery Example")
	assert.NotContains(t, string(history), "Morgan Sample")
	require.NotEmpty(t, f.model.input.Profile.History)
	lastChange := f.model.input.Profile.History[len(f.model.input.Profile.History)-1]
	assert.Equal(t, "manual", lastChange.Origin)
	assert.Equal(t, "legal_name", lastChange.Field)
	assert.Equal(t, "set", lastChange.Action)
	assert.Equal(t, replayed.Version, lastChange.Version)
	profileContext, err := json.Marshal(f.model.input.Profile)
	require.NoError(t, err)
	assert.NotContains(t, string(profileContext), "Avery Example")
	assert.NotContains(t, string(profileContext), "Morgan Sample")
	var interactions string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT json_agg(i)::text FROM bot.interactions i WHERE owner='alice'`).
			Scan(&interactions),
	)
	assert.NotContains(t, interactions, "Avery Example")
	assert.NotContains(t, interactions, "Morgan Sample")
}

func TestProfileQuestionAnswerAndStaleManualButton(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handle(t, f.b, message(1050, 101, "/profile"))
	card := profileCard(t, f)
	require.NotEmpty(t, card.Markup.Rows)
	staleButton := card.Markup.Rows[0][0].Data
	f.model.plan = agent.Plan{View: agent.ProfilesView, Text: "This question has an answer without changing your name."}
	handle(t, f.b, message(1051, 101, "Why do you need a full name?"))
	assert.Contains(t, profileCard(t, f).Text, f.model.plan.Text)
	_, err := f.b.API.ExecutePassProfile(t.Context(), "alice", passes.Command{
		Name: "set", Field: "legal_name", Value: "Avery Example", Origin: "manual", Key: "changed-before-click",
	})
	require.NoError(t, err)
	handle(t, f.b, aliceCallback(1052, card.ID, staleButton))
	current, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Empty(t, current.Pending)
	assert.EqualValues(t, 1, current.Version)
	assert.Contains(t, profileCard(t, f).Text, "Avery Example")
	assert.NotContains(t, profileCard(t, f).Text, f.model.plan.Text)
}

func TestProfileUnsolicitedSelfNameThroughScriptedModel(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ language, text, name string }{
		{"en", "My name is Avery Example", "Avery Example"},
		{"ru", "Меня зовут Иван Примеров", "Иван Примеров"},
	} {
		t.Run(test.language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			server := httptest.NewServer((&agent.ScriptedServer{}).Handler())
			t.Cleanup(server.Close)
			f.b.Model = agent.Remote{URL: server.URL, HTTP: server.Client()}
			handle(t, f.b, message(1100, 101, "/language "+test.language))
			handle(t, f.b, message(1101, 101, test.text))
			profile, err := f.b.API.PassProfile(t.Context(), "alice")
			require.NoError(t, err)
			assert.Equal(t, test.name, profile.LegalName)
			assert.Empty(t, profile.Pending)
			assert.EqualValues(t, 1, profile.Version)
			other, err := f.b.API.PassProfile(t.Context(), "bob")
			require.NoError(t, err)
			assert.Empty(t, other.LegalName)
		})
	}
}

func TestProfileAgentUsesCurrentPermissionsAndFrozenState(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.model.plan = agent.Plan{View: agent.ProfilesView,
		ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "Avery Example"}}
	handle(t, f.b, message(1200, 303, "My name is Avery Example"))
	visitor, err := f.b.API.PassProfile(t.Context(), "visitor")
	require.NoError(t, err)
	assert.Empty(t, visitor.LegalName)
	_, err = f.b.API.ExecutePassProfile(t.Context(), "alice", passes.Command{
		Name: "set", Field: "legal_name", Value: "Morgan Sample", Origin: "manual", Key: "freeze-test",
	})
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET frozen=true WHERE owner='alice'`)
	require.NoError(t, err)
	handle(t, f.b, message(1201, 101, "My name is Avery Example"))
	profile, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "Morgan Sample", profile.LegalName)
	assert.True(t, f.model.input.Profile.Frozen)
}

func TestProfileModelFailureDoesNotExposeNameToLaterContext(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, view := range []string{"/start", "/profile"} {
			t.Run(language+view, func(t *testing.T) {
				t.Parallel()
				f := setup(t)
				handle(t, f.b, message(1300, 101, "/language "+language))
				handle(t, f.b, message(1301, 101, view))
				failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				t.Cleanup(failed.Close)
				f.b.Model = agent.Remote{URL: failed.URL, HTTP: failed.Client()}
				handle(t, f.b, message(1302, 101, "My name is Avery Example"))
				notice, err := i18n.Translate(language, i18n.AgentUnavailable, nil)
				require.NoError(t, err)
				var displayed bool
				for _, card := range chatMessages(t, f, 101) {
					if strings.Contains(card.Text, notice) {
						displayed = true
					}
				}
				assert.True(t, displayed)
				f.b.Model = f.model
				f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Orders."}
				handle(t, f.b, message(1303, 101, "Show orders"))
				history, err := json.Marshal(f.model.input.History)
				require.NoError(t, err)
				assert.NotContains(t, string(history), "Avery Example")
				profile, err := f.b.API.PassProfile(t.Context(), "alice")
				require.NoError(t, err)
				assert.Empty(t, profile.LegalName)
			})
		}
	}
}

func TestUnstructuredIdentityTextDoesNotEnterSharedActionHistory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handle(t, f.b, message(1400, 101, "/name"))
	f.model.plan = agent.Plan{View: "workflow", Text: "Avery Example, please clarify TEST-PASSPORT-123."}
	handle(t, f.b, message(1401, 101, "Avery Example, what should I do with TEST-PASSPORT-123?"))
	var input string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=1401 AND kind='input'`).
			Scan(&input),
	)
	assert.NotContains(t, input, "Avery Example")
	assert.NotContains(t, input, "TEST-PASSPORT-123")
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
		VALUES('alice',1402,'input','{"origin":"agent","text":"Legacy Sensitive Name"}')`)
	require.NoError(t, err)
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Orders."}
	handle(t, f.b, message(1403, 101, "Show orders"))
	history, err := json.Marshal(f.model.input.History)
	require.NoError(t, err)
	assert.NotContains(t, string(history), "Avery Example")
	assert.NotContains(t, string(history), "TEST-PASSPORT-123")
	assert.NotContains(t, string(history), "Legacy Sensitive Name")
	profile, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "legal_name", profile.Pending)
	assert.Empty(t, profile.LegalName)
}

func TestProfileAnswerDoesNotSurviveNewerManualState(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.model.plan = agent.Plan{View: agent.ProfilesView, Text: "Your name has not been saved."}
	handle(t, f.b, message(1500, 101, "Is my name saved?"))
	card := profileCard(t, f)
	assert.Contains(t, card.Text, f.model.plan.Text)
	_, err := f.b.API.ExecutePassProfile(t.Context(), "alice", passes.Command{
		Name: "set", Field: "legal_name", Value: "Avery Example", Origin: "manual", Key: "external-name",
	})
	require.NoError(t, err)
	require.NoError(t, f.b.RenderProfile(t.Context(), "alice", 101))
	updated := profileCard(t, f)
	assert.Equal(t, card.ID, updated.ID)
	assert.Contains(t, updated.Text, "Avery Example")
	assert.NotContains(t, updated.Text, f.model.plan.Text)
}
