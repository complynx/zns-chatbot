package telegram_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestBroadcastCommandEntityPreservesUnicodeArgument(t *testing.T) {
	t.Parallel()
	message := telegram.Message{
		Text:     "/send_message_to 101 --msg 😀 hi 'friend'",
		Entities: []telegram.MessageEntity{{Type: "code", Offset: 27, Length: 14}},
	}
	raw, err := telegram.CommandText(message)
	require.NoError(t, err)
	command, err := adminmessage.ParseCommand(raw)
	require.NoError(t, err)
	assert.Equal(t, "😀 hi 'friend'", command.Content.Text)
	message.Entities[0].Offset++
	_, err = telegram.CommandText(message)
	require.Error(t, err)
}

func TestBroadcastHTMLNestedEntities(t *testing.T) {
	t.Parallel()
	message := telegram.Message{
		Text: "😀 A&B",
		Entities: []telegram.MessageEntity{
			{Type: "bold", Offset: 0, Length: 6},
			{Type: "italic", Offset: 3, Length: 3},
		},
	}
	text, err := telegram.MessageHTML(message)
	require.NoError(t, err)
	assert.Equal(t, "<b>😀 <i>A&amp;B</i></b>", text)
	message.Entities = append(message.Entities, telegram.MessageEntity{Type: "underline", Offset: 1, Length: 2})
	_, err = telegram.MessageHTML(message)
	require.Error(t, err)
}
