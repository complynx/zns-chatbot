package agent_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
)

func testImage() []byte {
	var body bytes.Buffer
	_ = png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	return body.Bytes()
}

type attachmentModel struct {
	input chan agent.Input
}

func (m attachmentModel) Plan(_ context.Context, in agent.Input) (agent.Plan, error) {
	m.input <- in
	return agent.Plan{View: "workflow"}, nil
}

func TestRemoteAttachment(t *testing.T) {
	t.Parallel()
	model := attachmentModel{input: make(chan agent.Input, 1)}
	server := httptest.NewServer(agent.ModelHandler(model))
	defer server.Close()
	// Large legal PNG source bytes exceed the old generic JSON body budget.
	body := append(testImage(), make([]byte, 2<<20)...)
	in := agent.Input{Text: "caption", Attachment: &agent.Attachment{
		ID: "private-id", Filename: "receipt.png", MIME: "image/png", Body: body,
	}, History: []agent.Event{{Kind: "old", Content: json.RawMessage(`"` + strings.Repeat("x", 60000) + `"`)}}}
	_, err := (agent.Remote{URL: server.URL}).Plan(t.Context(), in)
	require.NoError(t, err)
	got := <-model.input
	assert.Equal(t, body, got.Attachment.Body)
	assert.Equal(t, "caption", got.Text)
	assert.Empty(t, got.History)
	assert.Len(t, in.History, 1, "budgeting must not mutate caller input")
}

func TestModelInputStrictBody(t *testing.T) {
	t.Parallel()
	var scripted agent.ScriptedServer
	for name, handler := range map[string]http.Handler{
		"provider": agent.ModelHandler(attachmentModel{}), "scripted": scripted.Handler(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, body := range []string{`{"unknown":true}`, `{} {}`, `{"text":"` + strings.Repeat("x", 60001) + `"}`,
				`{"attachment":{"mime":"image/png","body":"%%%"}}`, "{}" + strings.Repeat(" ", 29<<20)} {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/plan", strings.NewReader(body)))
				assert.Equal(t, http.StatusBadRequest, recorder.Code)
			}
		})
	}
}

func TestAttachmentRejectsBeforeProvider(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	tooManyPixels := testImage()
	binary.BigEndian.PutUint32(tooManyPixels[16:20], 40_000_001)
	binary.BigEndian.PutUint32(tooManyPixels[29:33], crc32.ChecksumIEEE(tooManyPixels[12:29]))
	for name, attachment := range map[string]agent.Attachment{
		"unsupported": {MIME: "application/pdf", Body: []byte("private-secret")},
		"spoof":       {MIME: "image/jpeg", Body: testImage()},
		"corrupt":     {MIME: "image/png", Body: testImage()[:40]},
		"oversize":    {MIME: "image/png", Body: make([]byte, (20<<20)+1)},
		"pixels":      {MIME: "image/png", Body: tooManyPixels},
		"metadata":    {MIME: "image/png", Filename: strings.Repeat("x", 1025), Body: testImage()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, model := range []agent.Model{agent.OpenAI{Key: "test", BaseURL: "http://127.0.0.1:1"}, agent.Remote{URL: "http://127.0.0.1:1"}, agent.Codex{Executable: executable, SyntheticOnly: true}} {
				_, planErr := model.Plan(t.Context(), agent.Input{Attachment: &attachment})
				require.Error(t, planErr)
				assert.Contains(t, planErr.Error(), "model attachment")
				assert.NotContains(t, planErr.Error(), "private-secret")
			}
		})
	}
}

func TestJPEGAttachment(t *testing.T) {
	t.Parallel()
	var body bytes.Buffer
	require.NoError(t, jpeg.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	model := attachmentModel{input: make(chan agent.Input, 1)}
	server := httptest.NewServer(agent.ModelHandler(model))
	defer server.Close()
	_, err := (agent.Remote{URL: server.URL}).Plan(t.Context(), agent.Input{
		Attachment: &agent.Attachment{MIME: "image/jpeg", Body: body.Bytes()},
	})
	require.NoError(t, err)
	assert.Equal(t, body.Bytes(), (<-model.input).Attachment.Body)
}

type privateErrorTransport struct{}

func (privateErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private-media-secret")
}

func TestAttachmentErrorsArePrivate(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: privateErrorTransport{}}
	for _, model := range []agent.Model{
		agent.Remote{HTTP: client, URL: "https://example.test/private-token"},
		agent.OpenAI{HTTP: client, Key: "private-key"},
	} {
		_, err := model.Plan(
			t.Context(),
			agent.Input{Attachment: &agent.Attachment{MIME: "image/png", Body: testImage()}},
		)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private")
	}
}

func TestOpenAIImageContent(t *testing.T) {
	t.Parallel()
	body := testImage()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondSkillSelection(t, w, r, "receipts") {
			return
		}
		var request struct {
			Store bool `json:"store"`
			Input []struct {
				Content json.RawMessage `json:"content"`
			} `json:"input"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) || !assert.Len(t, request.Input, 2) {
			return
		}
		assert.False(t, request.Store)
		var parts []map[string]string
		if !assert.NoError(t, json.Unmarshal(request.Input[1].Content, &parts)) || !assert.Len(t, parts, 2) {
			return
		}
		assert.Equal(t, "input_text", parts[0]["type"])
		assert.Contains(t, parts[0]["text"], "caption")
		assert.Contains(t, parts[0]["text"], "receipt.png")
		assert.NotContains(t, parts[0]["text"], `"body"`)
		assert.NotContains(t, parts[0]["text"], base64.StdEncoding.EncodeToString(body))
		assert.Equal(t, "input_image", parts[1]["type"])
		assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(body), parts[1]["image_url"])
		api.JSON(w, http.StatusOK, map[string]any{"status": "completed", "output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]string{
						"type": "output_text",
						"text": `{"text":"Evidence","view":"media","action":null,"order_action":null,"profile_action":null,"media_action":{"media_id":"id","intent":"receipt","amount":"80.00","currency":"BYN","order_id":"","start_ms":0,"end_ms":0,"frame_count":0}}`,
					},
				},
			},
		}})
	}))
	defer server.Close()
	plan, err := (agent.OpenAI{Key: "test", BaseURL: server.URL}).Plan(t.Context(), agent.Input{
		Text:       "caption",
		Attachment: &agent.Attachment{ID: "id", Filename: "receipt.png", MIME: "image/png", Body: body},
	})
	require.NoError(t, err)
	require.NotNil(t, plan.MediaAction)
	assert.Equal(t, "id", plan.MediaAction.MediaID)
	assert.Equal(t, "80.00", plan.MediaAction.Amount)
}
