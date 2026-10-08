package sandbox_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func attestedDocument(t *testing.T, fake *sandbox.Fake, recipient int64) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("chat_id", strconv.FormatInt(recipient, 10)))
	part, err := form.CreateFormFile("document", "attested.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("attested document bytes"))
	require.NoError(t, err)
	require.NoError(t, form.Close())
	request := httptest.NewRequest(http.MethodPost, "/botsynthetic-token/sendDocument", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	fake.Handler().ServeHTTP(response, request)
	return response
}

func TestAttestedOutboundMethodsPreserveMappedRecipientsAndDenyEffects(t *testing.T) {
	t.Parallel()
	db := newNativeIngressDB(t)
	fake, err := sandbox.NewWithAttestedOwners(t.Context(), db, "synthetic-token", map[int64]string{101: "owner-101"})
	require.NoError(t, err)
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: fake.Token}
	sent, err := client.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Mapped reply"})
	require.NoError(t, err)
	var message telegram.Message
	require.NoError(t, client.Call(t.Context(), "editMessageText", map[string]any{
		"chat_id": 101, "message_id": sent.ID, "text": "Edited mapped reply",
	}, &message))
	require.Equal(t, int64(101), message.Chat.ID)
	require.Equal(t, http.StatusOK, attestedDocument(t, fake, 101).Code)
	require.NoError(t, client.Call(t.Context(), "forwardMessage", map[string]any{
		"chat_id": 101, "from_chat_id": 101, "message_id": sent.ID,
	}, &message))
	require.Equal(t, int64(101), message.Chat.ID)
	var stateBefore, stateAfter []byte
	require.NoError(t, db.QueryRow(t.Context(), "SELECT data FROM bot.fake_state WHERE id=true").Scan(&stateBefore))
	for _, request := range []struct {
		method  string
		payload map[string]any
	}{
		{"sendMessage", map[string]any{"chat_id": 202, "text": "Denied"}},
		{"editMessageText", map[string]any{"chat_id": 202, "message_id": sent.ID, "text": "Denied"}},
		{"forwardMessage", map[string]any{"chat_id": 202, "from_chat_id": 101, "message_id": sent.ID}},
	} {
		require.Error(t, client.Call(t.Context(), request.method, request.payload, &message), request.method)
	}
	require.Equal(t, http.StatusBadRequest, attestedDocument(t, fake, 202).Code)
	require.NoError(t, db.QueryRow(t.Context(), "SELECT data FROM bot.fake_state WHERE id=true").Scan(&stateAfter))
	require.JSONEq(
		t,
		string(stateBefore),
		string(stateAfter),
		"denied destinations do not record messages or cursor changes",
	)
	var files int
	var body []byte
	var fileID string
	require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM bot.fake_files").Scan(&files))
	require.Equal(t, 1, files, "only the accepted document is stored")
	require.NoError(t, db.QueryRow(t.Context(), "SELECT id,body FROM bot.fake_files").Scan(&fileID, &body))
	require.Equal(t, []byte("attested document bytes"), body)
	fileResponse := httptest.NewRecorder()
	fake.Handler().ServeHTTP(fileResponse, httptest.NewRequest(http.MethodGet, "/lab/files/"+fileID+"?user=101", nil))
	require.Equal(t, http.StatusOK, fileResponse.Code)
	require.Equal(t, body, fileResponse.Body.Bytes())
	for _, destination := range []struct {
		id   int64
		kind string
	}{{-1009001, "channel"}, {-1009002, "supergroup"}} {
		require.NoError(t, client.Call(t.Context(), "forwardMessage", map[string]any{
			"chat_id": destination.id, "from_chat_id": 101, "message_id": sent.ID,
		}, &message))
		require.Equal(t, destination.kind, message.Chat.Type)
		response := attestedDocument(t, fake, destination.id)
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Result telegram.Message `json:"result"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, destination.kind, result.Result.Chat.Type)
	}
	defaultFake, err := sandbox.New(t.Context(), db, fake.Token)
	require.NoError(t, err)
	response := attestedDocument(t, defaultFake, 202)
	require.Equal(t, http.StatusOK, response.Code, "nil mapping preserves default actor delivery")
	var result struct {
		Result telegram.Message `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	restored, err := sandbox.NewWithAttestedOwners(t.Context(), db, fake.Token, map[int64]string{101: "owner-101"})
	require.NoError(t, err)
	response = httptest.NewRecorder()
	restored.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/lab/files/"+result.Result.Document.FileID+"?user=202", nil))
	require.Equal(t, http.StatusNotFound, response.Code, "omitted actor cannot read its restored transport file")
}
