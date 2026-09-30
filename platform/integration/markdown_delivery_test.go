package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestMarkdownBrowser(t *testing.T) {
	t.Parallel()
	if os.Getenv("MARKDOWN_BROWSER") != "1" {
		t.Skip("set MARKDOWN_BROWSER=1 and NODE_BINARY for isolated browser QA")
	}
	f := setup(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	node := os.Getenv("NODE_BINARY")
	if node == "" {
		node = "node"
	}
	command := exec.CommandContext(ctx, node, "tests/markdown.mjs")
	command.Dir = ".."
	command.Env = append(os.Environ(), "SANDBOX_URL="+f.fake.URL)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Log(string(output))
}

func TestMarkdownProfileNameRemainsLiteral(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(90, 101, "/name"))
	profile, err := f.b.API.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	command := profileCommand("set", "legal_name", "markdown-name", profile)
	command.Value = "Avery *Example*"
	_, err = f.b.API.ExecutePassProfile(t.Context(), "alice", command)
	require.NoError(t, err)
	f.model.plan = agent.Plan{View: agent.ProfilesView, Text: "**Answer**"}
	handleVisible(t, f.b, message(91, 101, "explain my profile"))
	card := profileCard(t, f)
	assert.Contains(t, card.Text, "Avery *Example*")
	require.Len(t, card.Entities, 1)
	assert.Equal(t, "bold", card.Entities[0].Type)
	assert.Equal(t, len("Answer"), card.Entities[0].Length)
}

func TestMarkdownAgentDeliveryAndEditedManualView(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.model.plan = agent.Plan{
		View: "workflow",
		Text: "🚀 **Привет** [Даня](tg://user?id=101) [тема](https://t.me/c/123/7/9?thread=7&single)\n\n```go\na := `x`\n```",
	}
	handleVisible(t, f.b, message(100, 101, "tell me"))
	messages := chatMessages(t, f, 101)
	require.Len(t, messages, 1)
	card := messages[0]
	assert.Contains(t, card.Text, "🚀 Привет Даня тема")
	assert.NotContains(t, card.Text, "**Привет**")
	require.NotEmpty(t, card.Entities)
	assert.Equal(t, "bold", card.Entities[0].Type)
	assert.Equal(t, 3, card.Entities[0].Offset)
	var native bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT native_markdown FROM bot.interactions WHERE owner='alice' AND update_id=100 AND kind='reply'`).
			Scan(&native),
	)
	assert.True(t, native)
	restarted, err := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, err)
	server := httptest.NewServer(restarted.Handler())
	defer server.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/lab/state?user=101", nil)
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	var state struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&state))
	require.NoError(t, response.Body.Close())
	require.Len(t, state.Messages, 1)
	assert.Equal(t, card.Entities, state.Messages[0].Entities)
	// An ordinary manual action replaces the formatted answer in the same message.
	handleVisible(t, f.b, message(101, 101, "/start"))
	edited := chatMessages(t, f, 101)
	require.Len(t, edited, 1)
	assert.Equal(t, card.ID, edited[0].ID)
	assert.Empty(t, edited[0].Entities)
	assert.NotContains(t, edited[0].Text, "Привет")
}

func TestMarkdownUnsafeTargetFallsBackWithoutLosingAnswer(t *testing.T) {
	t.Parallel()
	f := setup(t)
	source := "[опасно](javascript:alert) **оригинальный ответ** <b>literal</b>"
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: source}
	handleVisible(t, f.b, message(100, 101, "tell me about orders"))
	card := pagingCard(t, f, 101, "menu")
	assert.Equal(t, source, card.Text)
	assert.Empty(t, card.Entities)
}

func TestMarkdownFakeWireValidationAndEdits(t *testing.T) {
	t.Parallel()
	f := setup(t)
	good := telegram.Send{ChatID: 101, Text: "*Bold 🚀* and _italic_", ParseMode: telegram.MarkdownV2}
	var sent telegram.Message
	require.NoError(t, f.b.TG.Call(t.Context(), "sendMessage", good, &sent))
	assert.Equal(t, "Bold 🚀 and italic", sent.Text)
	require.Len(t, sent.Entities, 2)
	assert.Equal(t, 7, sent.Entities[0].Length)
	assert.Equal(t, 12, sent.Entities[1].Offset)
	good.MessageID = sent.ID
	good.Text = "[topic](https://t.me/c/123/7/9)"
	require.NoError(t, f.b.TG.Call(t.Context(), "editMessageText", good, &sent))
	assert.Equal(t, "topic", sent.Text)
	require.Len(t, sent.Entities, 1)
	assert.Equal(t, "text_link", sent.Entities[0].Type)
	assert.Equal(t, "https://t.me/c/123/7/9", sent.Entities[0].URL)
	for _, payload := range []telegram.Send{
		{ChatID: 101, Text: "*unclosed", ParseMode: telegram.MarkdownV2},
		{ChatID: 101, Text: strings.Repeat("🚀", 2049)},
		{ChatID: 101, Text: strings.Repeat("a", 4097)},
	} {
		err := f.b.TG.Call(t.Context(), "sendMessage", payload, nil)
		var apiError *telegram.APIError
		require.ErrorAs(t, err, &apiError)
		assert.Equal(t, http.StatusBadRequest, apiError.Code)
	}
	require.NoError(
		t,
		f.b.TG.Call(
			t.Context(),
			"sendMessage",
			telegram.Send{ChatID: 101, Text: "*" + strings.Repeat("🚀", 2048) + "*", ParseMode: telegram.MarkdownV2},
			nil,
		),
	)
}
