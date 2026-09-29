package telegram_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestNativeFormattingKeepsManualSegmentsLiteral(t *testing.T) {
	t.Parallel()
	payload, err := telegram.PrepareSend(
		telegram.Send{
			Text:           "**Hello**",
			NativeMarkdown: true,
			LiteralPrefix:  "Name: A_*B!\n",
			LiteralSuffix:  "\nStatus: a_b",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, telegram.MarkdownV2, payload.ParseMode)
	text, entities, err := telegram.VisibleText(payload)
	require.NoError(t, err)
	assert.Equal(t, "Name: A_*B!\nHello\nStatus: a_b", text)
	require.Len(t, entities, 1)
	assert.Equal(t, "bold", entities[0].Type)
	assert.Equal(t, 12, entities[0].Offset)
	source := "Unsafe [link](javascript:alert) **keep original**"
	payload, err = telegram.PrepareSend(telegram.Send{Text: source, NativeMarkdown: true})
	require.NoError(t, err)
	assert.Empty(t, payload.ParseMode)
	assert.Equal(t, source, payload.Text)
	manual, err := telegram.PrepareSend(telegram.Send{Text: "A_*B!"})
	require.NoError(t, err)
	assert.Equal(t, "A_*B!", manual.Text)
	assert.Empty(t, manual.ParseMode)
}

func TestVisibleTextLimitUsesUTF16AfterParsing(t *testing.T) {
	t.Parallel()
	for _, source := range []string{strings.Repeat("a", 4096), strings.Repeat("🚀", 2048)} {
		_, err := telegram.PrepareSend(telegram.Send{Text: "**" + source + "**", NativeMarkdown: true})
		require.NoError(t, err)
	}
	for _, source := range []string{strings.Repeat("a", 4097), strings.Repeat("🚀", 2049), "", string([]byte{0xff})} {
		_, err := telegram.PrepareSend(telegram.Send{Text: source})
		require.ErrorIs(t, err, telegram.ErrMessageText)
	}
}
