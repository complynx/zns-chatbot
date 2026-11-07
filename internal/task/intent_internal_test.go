package task

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestNormalizeTrustedPrivateSender(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		user telegram.User
		chat telegram.Chat
		want bool
	}{
		{"private", telegram.User{ID: 7}, telegram.Chat{ID: 7, Type: "private"}, true},
		{"foreign chat", telegram.User{ID: 7}, telegram.Chat{ID: 8, Type: "private"}, false},
		{"group", telegram.User{ID: 7}, telegram.Chat{ID: 7, Type: "group"}, false},
		{"bot", telegram.User{ID: 7, IsBot: true}, telegram.Chat{ID: 7, Type: "private"}, false},
		{"zero", telegram.User{}, telegram.Chat{Type: "private"}, false},
		{"too large", telegram.User{ID: 1 << 52}, telegram.Chat{ID: 1 << 52, Type: "private"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := telegram.Update{ID: 1, Message: &telegram.Message{From: &tt.user, Chat: tt.chat, Text: "hello"}}
			raw, _ := json.Marshal(u)
			_, err := normalize(u, raw)
			if (err == nil) != tt.want {
				t.Fatalf("accepted=%v want=%v", err == nil, tt.want)
			}
		})
	}
}
func TestNormalizeUnsupportedMediaDoesNotBecomeModelText(t *testing.T) {
	t.Parallel()
	u := telegram.Update{
		ID: 1,
		Message: &telegram.Message{
			From: &telegram.User{ID: 7},
			Chat: telegram.Chat{ID: 7, Type: "private"},
			Text: "caption",
		},
	}
	i, err := normalize(u, json.RawMessage(`{"update_id":1,"message":{"sticker":{"file_id":"sensitive-file-id"}}}`))
	if err != nil || i.Kind != "unsupported" {
		t.Fatal(i.Kind, err)
	}
	u.Message.Text = "/cancel"
	raw, _ := json.Marshal(u)
	i, err = normalize(u, raw)
	if err != nil || i.Kind != kindMessage {
		t.Fatal(i.Kind, err)
	}
}
func TestOperationCallbacksStayBoundedAndBindExactPrompt(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	q := loom.Question{
		ID:        "long-question-identifier",
		Text:      "Choose",
		AllowText: true,
		ExpiresAt: time.Now().Add(time.Minute),
		Choices:   []loom.Choice{{ID: "choice", Label: "First"}},
	}
	i := intent{User: telegram.User{ID: 7}, ChatID: 7}
	data := fmt.Sprintf("a:%s:%s:approve", id, promptKey(q))
	if len(data) > 64 {
		t.Fatalf("callback length %d", len(data))
	}
	u := telegram.Update{
		ID: 2,
		CallbackQuery: &telegram.CallbackQuery{
			From:    i.User,
			Message: &telegram.Message{Chat: telegram.Chat{ID: 7, Type: "private"}},
			Data:    data,
		},
	}
	raw, _ := json.Marshal(u)
	got, err := normalize(u, raw)
	if err != nil || got.CallbackTaskID != id || got.PromptKey != promptKey(q) || got.Decision != "approve" {
		t.Fatal(got, err)
	}
	old := promptKey(q)
	q.Text = "Different"
	if promptKey(q) == old {
		t.Fatal("prompt change did not invalidate callback")
	}
	for _, bad := range []string{"q:invalid:12345678:0", fmt.Sprintf("q:%s:invalid:0", id),
		fmt.Sprintf("q:%s:12345678:-1", id), fmt.Sprintf("a:%s:12345678:yes", id)} {
		u.CallbackQuery.Data = bad
		raw, _ = json.Marshal(u)
		if _, err = normalize(u, raw); err == nil {
			t.Fatalf("accepted callback %q", bad)
		}
	}
}
func TestLanguageChoice(t *testing.T) {
	t.Parallel()
	tests := []struct{ code, fallback, want string }{
		{
			"ru-RU",
			"en",
			"ru",
		},
		{"BE", "en", "ru"},
		{"uk-UA", "en", "ru"},
		{"en-GB", "ru", "en"},
		{"de", "ru", "en"},
		{"", "ru", "ru"},
		{"", "en", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.code+"/"+tt.fallback, func(t *testing.T) {
			t.Parallel()
			if got := languageChoice(tt.code, tt.fallback); got != tt.want {
				t.Fatalf("language=%s want=%s", got, tt.want)
			}
		})
	}
}
