package agent_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
)

// Existing final-plan contract tests use this selector fixture; dedicated skill
// pipeline tests separately verify selection validation and selective prompts.
func respondSkillSelection(t *testing.T, writer http.ResponseWriter, request *http.Request, ids ...string) bool {
	t.Helper()
	data, err := io.ReadAll(request.Body)
	if err != nil {
		t.Error(err)
		return true
	}
	request.Body = io.NopCloser(bytes.NewReader(data))
	var envelope struct {
		Text struct {
			Format struct {
				Name string `json:"name"`
			} `json:"format"`
		} `json:"text"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		t.Error(err)
		return true
	}
	if envelope.Text.Format.Name != "zns_skill_selection" {
		return false
	}
	if ids == nil {
		ids = []string{}
	}
	selection, err := json.Marshal(map[string]any{"skills": ids, "reply_language": "en"})
	if err != nil {
		t.Error(err)
		return true
	}
	api.JSON(writer, http.StatusOK, map[string]any{"status": "completed", "output": []any{
		map[string]any{
			"type":    "message",
			"content": []any{map[string]string{"type": "output_text", "text": string(selection)}},
		},
	}})
	return true
}

func skillSelectingHandler(t *testing.T, next http.Handler, ids ...string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if respondSkillSelection(t, writer, request, ids...) {
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func TestOpenAISelectsSkillsBeforePlanning(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Model string `json:"model"`
			Max   int    `json:"max_output_tokens"`
			Input []struct {
				Content string `json:"content"`
			} `json:"input"`
			Text struct {
				Format struct {
					Name string `json:"name"`
				} `json:"format"`
			} `json:"text"`
		}
		if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) || !assert.Len(t, body.Input, 2) {
			return
		}
		assert.Equal(t, "gpt-6-luna", body.Model)
		assert.Contains(t, body.Input[1].Content, "Current user utterance")
		assert.NotContains(t, body.Input[1].Content, `"language"`)
		text := `{"text":"Hello","view":"workflow","action":null,"order_action":null,"profile_action":null,"media_action":null}`
		switch calls.Add(1) {
		case 1:
			assert.Equal(t, "zns_skill_selection", body.Text.Format.Name)
			assert.Equal(t, 256, body.Max)
			assert.Contains(t, body.Input[0].Content, "Skill catalog:")
			assert.NotContains(t, body.Input[0].Content, "Video frames carry")
			text = `{"skills":[],"reply_language":"en"}`
		case 2:
			assert.Equal(t, "zns_action_plan", body.Text.Format.Name)
			assert.Equal(t, 1600, body.Max)
			assert.NotContains(t, body.Input[0].Content, "Selected skill:")
			assert.NotContains(t, body.Input[0].Content, "Skill catalog:")
			assert.NotContains(t, body.Input[0].Content, "OCR never accepts")
		default:
			t.Error("unexpected extra provider call")
		}
		api.JSON(writer, http.StatusOK, map[string]any{"status": "completed", "output": []any{
			map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": text}}},
		}})
	}))
	defer server.Close()
	plan, err := (agent.OpenAI{Key: "test", BaseURL: server.URL}).Plan(
		t.Context(),
		agent.Input{Text: "Hello", Language: "ru"},
	)
	require.NoError(t, err)
	assert.Equal(t, "Hello", plan.Text)
	assert.EqualValues(t, 2, calls.Load())
}
