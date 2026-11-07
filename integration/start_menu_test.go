package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

type menuCall struct {
	Method   string                `json:"-"`
	Commands []telegram.BotCommand `json:"commands"`
	Scope    struct {
		Type   string `json:"type"`
		ChatID int64  `json:"chat_id"`
	} `json:"scope"`
	Language string `json:"language_code"`
	ChatID   int64  `json:"chat_id"`
	Menu     struct {
		Type string `json:"type"`
	} `json:"menu_button"`
}

func TestStartOnboardsBeforeWelcomeAndMenuAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, initial := range []string{"/start", "👋"} {
		t.Run(initial, func(t *testing.T) {
			t.Parallel()
			firstOnboardingBeforeWelcome(t, initial)
		})
	}
}

func firstOnboardingBeforeWelcome(t *testing.T, initial string) {
	t.Helper()
	p := newPeers(t)
	p.question = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	update := telegram.Update{
		ID: 98110,
		Message: &telegram.Message{
			ID:   98110,
			From: &telegram.User{ID: 456, LanguageCode: "pt-BR", FirstName: "Synthetic", LastName: "Hint"},
			Chat: telegram.Chat{ID: 456, Type: "private"},
			Text: initial,
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "awaiting_user")
	deliveredNamePrompt(t, h, accepted.ID)
	p.mu.Lock()
	require.Empty(t, visibleMenuCalls(p.menuCalls), "Visible menu must wait for confirmed name")
	require.Len(t, p.sent, 1, "Only the name question is delivered before onboarding")
	require.Contains(t, p.createdInputs[0], `"telegram_language_code":"pt-BR"`)
	require.Contains(t, p.createdInputs[0], `"needs_real_name":true`)
	p.mu.Unlock()
	scope, pending, token := agentRequestsName(t, h, accepted.ID)
	h.stop(t)
	h.start(t)
	code, result := profileRequest(
		t,
		h,
		token,
		"complete_name",
		uuid.New(),
		map[string]any{
			"expected_version": pending.Version,
			"expectation_id":   pending.NameExpectation.ID,
			"first_name":       "João",
			"last_name":        "da Silva",
			"first_name_latin": "João",
			"last_name_latin":  "da Silva",
		},
	)
	require.Equal(t, 200, code, string(result))
	answer := h.submitText(t, 98111, "João da Silva")
	h.await(t, answer, "completed")
	h.await(t, accepted.ID, "completed")
	deliveredNamePrompt(t, h, accepted.ID)
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.sent) == 4 },
		5*time.Second,
		10*time.Millisecond,
	)
	profile, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, "João da Silva", profile.FullName)
	p.mu.Lock()
	require.Len(t, visibleMenuCalls(p.menuCalls), 2)
	menus := visibleMenuCalls(p.menuCalls)
	require.Empty(t, menus[0].Language)
	require.Equal(t, "ru", menus[1].Language)
	require.Equal(t, int64(456), menus[0].Scope.ChatID)
	for _, menu := range menus {
		for _, command := range menu.Commands {
			require.NotContains(t, []string{"start", "knowledge", "payment_reviews"}, command.Command)
		}
	}
	require.Contains(t, p.sent[2].Text, "ZiNuSya")
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[2].ReplyMarkup)
	require.Contains(t, p.sent[3].Text, "/menu")
	launch := p.sent[3].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Equal(t, appTestOrigin+"/app/", launch.InlineKeyboard[0][0].WebApp.URL)
	p.mu.Unlock()
	h.stop(t)
	h.start(t)
	replay, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	require.Equal(t, accepted.ID, replay.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 4)
	require.GreaterOrEqual(t, len(visibleMenuCalls(p.menuCalls)), 2)
}

func TestStartCompletionWithoutConfirmedNameDoesNotSendWelcome(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submitText(t, 98120, "/start")
	h.await(t, id, "completed")
	deliveredNamePrompt(t, h, id)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, visibleMenuCalls(p.menuCalls))
	require.Len(t, p.sent, 1, "A model completion is not proof of name onboarding")
}

func visibleMenuCalls(calls []menuCall) []menuCall {
	var result []menuCall
	for _, call := range calls {
		if len(call.Commands) > 0 {
			result = append(result, call)
		}
	}
	return result
}

func TestMenuAvailabilityRefreshesWithoutIncomingMessages(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	h.stop(t)
	owner, event := uuid.New(), uuid.New()
	_, err := h.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_users
 (id,bot_id,tg_user_id,zitadel_org,zitadel_subject) VALUES($1,123,456,'org','user')`, owner)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_user_profiles
 (user_id,version,full_name,first_name,last_name,name_source) VALUES($1,1,'Test Owner','Test','Owner','user')`, owner)
	require.NoError(t, err)
	h.start(t)
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(visibleMenuCalls(p.menuCalls)) == 2 },
		5*time.Second,
		10*time.Millisecond,
	)
	p.mu.Lock()
	for _, call := range visibleMenuCalls(p.menuCalls) {
		for _, command := range call.Commands {
			require.NotEqual(t, "register", command.Command)
		}
	}
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_events
 (id,bot_id,organization_id,name,timezone,starts_at,ends_at,modules)
 VALUES($1,123,'org','Dance','UTC',now(),now()+interval '2 days',ARRAY['registrations'])`, event)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(t.Context(), `INSERT INTO shuttle_event_registration_rules(event_id,document)
 VALUES($1,jsonb_build_object('closes_at',now()+interval '1 day'))`, event)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		menus := visibleMenuCalls(p.menuCalls)
		if len(menus) < 4 {
			return false
		}
		for _, command := range menus[len(menus)-1].Commands {
			if command.Command == "register" {
				return true
			}
		}
		return false
	}, 40*time.Second, 50*time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.sent, "menu synchronization must not send messages")
	require.Empty(t, p.createdInputs, "menu synchronization must not start an agent run")
}
