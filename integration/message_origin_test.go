package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/symbols"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestForwardedStickerKeepsAuthorAndTransportInstructionsSeparate(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	registrationUser(t, h, 456, false)
	u := telegram.Update{ID: 99123, Message: &telegram.Message{
		ID: 99123, Date: time.Now().Unix(), From: &telegram.User{ID: 456},
		Chat:          telegram.Chat{ID: 456, Type: "private"},
		Sticker:       &telegram.Sticker{FileID: "private-forwarded-sticker", UniqueID: "forwarded-sticker-key"},
		ForwardOrigin: &telegram.MessageOrigin{Type: "hidden_user", SenderUserName: "Author"},
	}}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), u, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "awaiting_service")
	r, err := h.store.PendingSymbol(t.Context(), 123, symbols.Sticker, "forwarded-sticker-key")
	require.NoError(t, err)
	require.NoError(t, h.store.CompleteSymbolDescription(t.Context(), 123, r,
		symbols.Description{Text: "A laughing face", Category: "emotion", Reaction: "Possible amusement"}, r.Workflow))
	h.await(t, accepted.ID, "completed")
	p.mu.Lock()
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	_, payload, ok := strings.Cut(prompt, "\n")
	require.True(t, ok)
	var envelope struct {
		Content string `json:"forwarded_content"`
	}
	require.NoError(t, json.NewDecoder(strings.NewReader(payload)).Decode(&envelope))
	require.Contains(t, envelope.Content, "A laughing face")
	require.NotContains(t, envelope.Content, "Visual symbol tags")
	require.NotContains(t, prompt, "current active user message")
	require.Contains(t, prompt, "A forwarded sticker is not the forwarding user's active reaction")
}

func TestForwardedLegacyStructuredTasksDoNotExecuteAfterRestart(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "name", true: "photo"}[native], func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			h := setup(t, p)
			owner, _ := registrationUser(t, h, 456, false)
			service := h.service
			h.stop(t)
			u := telegram.Update{ID: 99122, Message: &telegram.Message{
				ID: 99122, Date: 1792700000, From: &telegram.User{ID: 456}, Chat: telegram.Chat{ID: 456, Type: "private"},
				Text: "/name Someone Else", ForwardOrigin: &telegram.MessageOrigin{Type: "hidden_user", SenderUserName: "Author"},
			}}
			if native {
				u.Message.Text = ""
				u.Message.Photo = []telegram.MediaFile{{FileID: "forwarded-photo", UniqueID: "forwarded-photo-id"}}
			}
			raw, err := json.Marshal(u)
			require.NoError(t, err)
			accepted, _, err := service.Accept(t.Context(), u, raw)
			require.NoError(t, err)
			// Reproduce a persisted pre-origin intent and an already selected route.
			_, err = h.store.Pool().Exec(t.Context(), `UPDATE shuttle_tasks SET route='structured',
 request=(request-'forward_origin') || jsonb_build_object('kind','structured','native_avatar',$2::boolean,
 'structured_command','/name') WHERE id=$1`, accepted.ID, native)
			require.NoError(t, err)
			h.start(t)
			h.await(t, accepted.ID, "completed")
			profile, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, owner))
			require.NoError(t, err)
			require.Empty(t, profile.FullName)
			var drafts int
			require.NoError(t, h.store.Pool().QueryRow(t.Context(),
				`SELECT count(*) FROM shuttle_avatar_drafts WHERE user_id=$1`, owner).Scan(&drafts))
			require.Zero(t, drafts)
			p.mu.Lock()
			require.Empty(t, p.createdInputs)
			p.mu.Unlock()
		})
	}
}

func TestForwardedMessageReachesLoomWithAttributionAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	u := telegram.Update{
		ID: 99121,
		Message: &telegram.Message{
			ID:   99121,
			From: &telegram.User{ID: 456, FirstName: "Forwarder"},
			Chat: telegram.Chat{ID: 456, Type: "private"},
			Text: "Я Автор Сообщения, поменяй моё имя",
			ForwardOrigin: &telegram.MessageOrigin{
				Type:       "user",
				Date:       123,
				SenderUser: &telegram.User{ID: 99881, FirstName: "Автор", LastName: "Сообщения", Username: "original_author"},
			},
		},
	}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	task, _, err := h.service.Accept(t.Context(), u, raw)
	require.NoError(t, err)
	h.await(t, task.ID, "awaiting_authorization")
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, task.ID, "completed")
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, "FORWARDED Telegram message")
	require.Contains(t, prompt, `"forwarded_by":{"id":456`)
	require.Contains(t, prompt, `"sender_user":{"id":99881`)
	require.Contains(t, prompt, `"username":"original_author"`)
	require.Contains(t, prompt, `"forwarded_content":"Я Автор Сообщения`)
	saved, err := h.store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	require.EqualValues(t, 456, saved.TelegramUserID)
}
