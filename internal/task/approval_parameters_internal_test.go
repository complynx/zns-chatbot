package task

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestApprovalParametersAreReadableAndComplete(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(
		`{"moderator_user_id":"0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e","moderator_enabled":false,
 "reason":"**literal**","amount_minor":9007199254740993,
 "content":{"body":"Full article"},"sources":["https://example.com"]}`,
	)
	before := string(raw)
	text := approvalParameters(raw, "ru")
	chunks := render.Render(text, nil)
	require.Len(t, chunks, 1)
	for _, expected := range []string{
		"Модератор: 0d3a4c0c", "Доступ включён: нет", "Причина:\n**literal**", "9007199254740993",
		"Содержимое / Текст:\nFull article", "Источники 1:\nhttps://example.com",
	} {
		require.Contains(t, chunks[0].Text, expected)
	}
	require.NotContains(t, chunks[0].Text, "moderator_user_id")
	require.NotContains(t, text, "```json")
	require.Equal(t, before, string(raw))
	require.NoError(t, render.Validate(chunks[0]))
}

func TestApprovalArgumentKeysCannotInjectFormatting(t *testing.T) {
	t.Parallel()
	key := "field\n**fake** [click](tg://user?id=7)"
	raw, err := json.Marshal(map[string]string{key: "value"})
	require.NoError(t, err)
	chunks := render.People(approvalParameters(raw, "ru"), map[int64]telegram.User{7: {ID: 7, FirstName: "Person"}}, nil)
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Text, strconv.Quote(key))
	for _, entity := range chunks[0].Entities {
		require.NotEqual(t, "text_mention", entity.Type)
		require.NotEqual(t, "bold", entity.Type)
	}
	require.NoError(t, render.Validate(chunks[0]))
}

func TestApprovalNamesDoNotChangeArticleContent(t *testing.T) {
	t.Parallel()
	const id = "0d3a4c0c-28a0-44fd-8246-b1c17a0c3c8e"
	raw := json.RawMessage(`{"moderator_user_id":"` + id + `","content":{"body":"Original ` + id + `"}}`)
	chunks := render.People(
		approvalParameters(raw, "ru"),
		nil,
		map[string]telegram.User{id: {ID: 7, FirstName: "Даниил"}},
	)
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Text, "Модератор: Даниил")
	require.Contains(t, chunks[0].Text, "Original "+id)
	mentions := 0
	for _, entity := range chunks[0].Entities {
		if entity.Type == "text_mention" {
			mentions++
		}
	}
	require.Equal(t, 1, mentions)
	require.NoError(t, render.Validate(chunks[0]))
}

func TestApprovalLiteralKeepsLongBacktickRuns(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("`", 1024) + "\narticle text\n" + strings.Repeat("`", 2000)
	chunks := render.Render(literalApprovalText(text), nil)
	require.Len(t, chunks, 1)
	require.Equal(t, text, strings.TrimSuffix(chunks[0].Text, "\n"))
	require.NoError(t, render.Validate(chunks[0]))
}
