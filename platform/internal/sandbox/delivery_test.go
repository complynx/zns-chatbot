package sandbox_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestSyntheticDeliveries(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer((&sandbox.Fake{Token: "sandbox"}).Handler())
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "sandbox"}
	cases := []struct {
		name                string
		chat                any
		topic               int64
		mode, text, visible string
		valid               bool
	}{
		{"UTF16 limit", "101", 0, "HTML", "<b>" + strings.Repeat("😀", 2048) + "</b>", strings.Repeat("😀", 2048), true},
		{"UTF16 overflow", "101", 0, "HTML", "<b>" + strings.Repeat("😀", 2049) + "</b>", "", false},
		{"boolean chat", true, 0, "", "hello", "", false},
		{"numeric string", "101", 0, "", "hello", "hello", true},
		{"channel alias", "@sandbox_channel", 0, "HTML", "😀 <b>hello &amp; <i>world</i></b>", "😀 hello & world", true},
		{"channel ID", int64(-1009001), 0, "Markdown", "*Hello* _world_!", "Hello world!", true},
		{"forum topic", "@sandbox_forum", 101, "Markdown", "[link](https://example.com) `x_y`", "link x_y", true},
		{"unknown chat", "@external_channel", 0, "", "hello", "", false},
		{"unknown topic", "@sandbox_forum", 999, "", "hello", "", false},
		{"private topic", 101, 101, "", "hello", "", false},
		{"HTML script", 101, 0, "HTML", "<script>alert(1)</script>", "", false},
		{"HTML attribute", 101, 0, "HTML", "<b onclick='alert(1)'>x</b>", "", false},
		{"unsafe link", 101, 0, "HTML", "<a href='javascript:alert(1)'>x</a>", "", false},
		{"HTML closing attribute", 101, 0, "HTML", "<b>x</b unexpected='yes'>", "", false},
		{"HTML closing slash", 101, 0, "HTML", "<b>x</b/>", "", false},
		{"HTML close whitespace", 101, 0, "HTML", "<b>x</b \t\r\n>", "x", true},
		{"HTML close plain", 101, 0, "HTML", "<b>x</b>", "x", true},
		{"HTML bad nesting", 101, 0, "HTML", "<b><i>x</b></i>", "", false},
		{"HTML unsupported entity", 101, 0, "HTML", "&copy;", "", false},
		{"Markdown nested", 101, 0, "Markdown", "*bold _italic_*", "", false},
		{"Markdown unclosed", 101, 0, "Markdown", "*bold", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var message telegram.Message
			err := client.Call(
				t.Context(),
				"sendMessage",
				map[string]any{
					"chat_id":           tc.chat,
					"message_thread_id": tc.topic,
					"parse_mode":        tc.mode,
					"text":              tc.text,
				},
				&message,
			)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.visible, message.Text)
			require.Equal(t, tc.topic, message.ThreadID)
			if tc.name == "channel alias" {
				require.Equal(t, "sandbox_channel", message.Chat.Username)
				require.Contains(t, message.Entities, telegram.MessageEntity{Type: "bold", Offset: 3, Length: 13})
			}
		})
	}
}
