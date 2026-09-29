package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
)

type assetModel struct {
	input *agent.Input
	plan  agent.Plan
	err   error
}

func (m assetModel) Plan(_ context.Context, input agent.Input) (agent.Plan, error) {
	*m.input = input
	return m.plan, m.err
}

func TestCanonicalAssetDescription(t *testing.T) {
	t.Parallel()
	var received agent.Input
	model := assetModel{input: &received, plan: agent.Plan{View: "media", Text: "  A red circle.  "}}
	frame := agent.Attachment{
		ID:          "owner-private-id",
		Filename:    "private-name.png",
		MIME:        "image/png",
		Body:        testImage(),
		TimestampMS: 500,
	}
	description, err := agent.DescribeAsset(t.Context(), model, "custom_emoji", []agent.Attachment{frame})
	require.NoError(t, err)
	assert.Equal(t, "A red circle.", description)
	assert.Equal(t, agent.Input{AssetTask: &agent.AssetTask{Kind: "custom_emoji"}, Frames: []agent.Attachment{
		{ID: "asset", Filename: "frame-0", MIME: "image/png", Body: frame.Body, TimestampMS: 500},
	}}, received)
	assert.Equal(t, "private-name.png", frame.Filename)
}

func TestCanonicalAssetRejectsInvalidInputAndActions(t *testing.T) {
	t.Parallel()
	var received agent.Input
	frames := []agent.Attachment{{MIME: "image/png", Body: testImage()}}
	for _, plan := range []agent.Plan{
		{View: "media", Text: " "},
		{View: "orders", Text: "Done"},
		{View: "media", Text: "Done", MediaAction: &agent.MediaProposal{Intent: "cancel", MediaID: "asset"}},
		{View: "media", Text: "A circle", ScriptAction: &agent.ScriptProposal{Code: "return 1;", InputJSON: "null"}},
		{View: "profile", Text: "Done", ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "John Doe"}},
	} {
		_, err := agent.DescribeAsset(t.Context(), assetModel{input: &received, plan: plan}, "sticker", frames)
		require.Error(t, err)
	}
	model := assetModel{input: &received, err: errors.New("provider unavailable")}
	_, err := agent.DescribeAsset(t.Context(), model, "sticker", frames)
	require.ErrorContains(t, err, "provider unavailable")
	_, err = agent.DescribeAsset(t.Context(), model, "avatar", frames)
	require.ErrorContains(t, err, "invalid asset")
	_, err = agent.DescribeAsset(t.Context(), model, "sticker", nil)
	require.ErrorContains(t, err, "invalid asset")
	_, err = agent.DescribeAsset(
		t.Context(),
		model,
		"sticker",
		[]agent.Attachment{{MIME: "image/png", Body: []byte("broken")}},
	)
	require.ErrorContains(t, err, "readable image")
}

func TestCanonicalAssetRejectsConversationContext(t *testing.T) {
	t.Parallel()
	var received agent.Input
	server := httptest.NewServer(
		agent.ModelHandler(assetModel{input: &received, plan: agent.Plan{View: "media", Text: "A circle."}}),
	)
	defer server.Close()
	model := agent.Remote{URL: server.URL}
	base := agent.Input{
		AssetTask: &agent.AssetTask{Kind: "sticker"},
		Frames:    []agent.Attachment{{MIME: "image/png", Body: testImage()}},
	}
	_, err := model.Plan(t.Context(), base)
	require.NoError(t, err)
	for _, contextual := range []agent.Input{
		{Text: "private caption"}, {Language: "ru"}, {OrderCount: 1},
		{History: []agent.Event{{Kind: "input", Content: json.RawMessage(`"private history"`)}}},
		{MediaContext: &agent.MediaContext{}},
		{Script: &agent.ScriptContext{Available: true}},
	} {
		contextual.AssetTask, contextual.Frames = base.AssetTask, base.Frames
		_, err = model.Plan(t.Context(), contextual)
		require.ErrorContains(t, err, "conversation context")
	}
}

func TestOpenAICanonicalAssetInstructions(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []struct {
				Content json.RawMessage `json:"content"`
			} `json:"input"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) || !assert.Len(t, request.Input, 2) {
			return
		}
		assert.Contains(t, string(request.Input[0].Content), "Describe the supplied sticker or custom emoji itself")
		assert.NotContains(t, string(request.Input[0].Content), "Skill catalog:")
		assert.NotContains(t, string(request.Input[0].Content), "Selected skill:")
		assert.NotContains(t, string(request.Input[0].Content), "ЗиНуСя")
		assert.NotContains(t, string(request.Input[1].Content), "private-name")
		assert.Contains(t, string(request.Input[1].Content), "input_image")
		api.JSON(w, http.StatusOK, map[string]any{"status": "completed", "output": []any{
			map[string]any{"type": "message", "content": []any{
				map[string]any{"type": "output_text", "text": `{"view":"media","text":"A circle."}`},
			}},
		}})
	}))
	defer server.Close()
	text, err := agent.DescribeAsset(t.Context(), agent.OpenAI{Key: "test", BaseURL: server.URL}, "sticker",
		[]agent.Attachment{{Filename: "private-name", MIME: "image/png", Body: testImage()}})
	require.NoError(t, err)
	assert.Equal(t, "A circle.", text)
}
