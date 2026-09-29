package agent_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
)

func TestOpenAIFrames(t *testing.T) {
	t.Parallel()
	frame := agent.Attachment{ID: "video", MIME: "image/png", Body: testImage(), TimestampMS: 1000}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondSkillSelection(t, w, r, "av") {
			return
		}
		var request struct {
			Input []struct {
				Content json.RawMessage `json:"content"`
			} `json:"input"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) || !assert.Len(t, request.Input, 2) {
			return
		}
		var parts []map[string]string
		if !assert.NoError(t, json.Unmarshal(request.Input[1].Content, &parts)) || !assert.Len(t, parts, 3) {
			return
		}
		assert.Contains(t, parts[0]["text"], `"timestamp_ms":1000`)
		assert.NotContains(t, parts[0]["text"], `"body"`)
		for _, part := range parts[1:] {
			assert.Equal(t, "input_image", part["type"])
			assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(frame.Body), part["image_url"])
		}
		api.JSON(w, http.StatusOK, map[string]any{"status": "completed", "output": []any{
			map[string]any{"type": "message", "content": []any{
				map[string]string{
					"type": "output_text",
					"text": `{"text":"Need detail","view":"media","action":null,"order_action":null,"profile_action":null,"media_action":{"media_id":"video","intent":"inspect_video","amount":"","currency":"","order_id":"","start_ms":1000,"end_ms":4000,"frame_count":4}}`,
				},
			}},
		}})
	}))
	defer server.Close()
	plan, err := (agent.OpenAI{Key: "test", BaseURL: server.URL}).Plan(t.Context(), agent.Input{
		Frames: []agent.Attachment{frame, frame},
		MediaContext: &agent.MediaContext{
			Pending: []agent.MediaHint{{ID: "video", Kind: "video", DurationMS: 8000, CanInspect: true}},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, plan.MediaAction)
	assert.Equal(t, "inspect_video", plan.MediaAction.Intent)
	assert.Equal(t, int64(1000), plan.MediaAction.StartMS)
	assert.Equal(t, 4, plan.MediaAction.FrameCount)
	assert.NotEmpty(t, frame.Body)
}
