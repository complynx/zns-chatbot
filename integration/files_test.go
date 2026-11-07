package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func submitFile(t *testing.T, h *harness, id int64, sentAt time.Time) uuid.UUID {
	t.Helper()
	return submitSizedFile(t, h, id, sentAt, 0)
}

func submitSizedFile(t *testing.T, h *harness, id int64, sentAt time.Time, size int64) uuid.UUID {
	t.Helper()
	body, err := json.Marshal(
		telegram.Update{
			ID: id,
			Message: &telegram.Message{
				ID:      id,
				Date:    sentAt.Unix(),
				From:    &telegram.User{ID: 456, LanguageCode: "ru"},
				Chat:    telegram.Chat{ID: 456, Type: "private"},
				Caption: "Посмотри этот документ",
				Document: &telegram.MediaFile{
					FileID:    "private-telegram-file",
					UniqueID:  "unique-fixture",
					MIMEType:  "application/pdf",
					Filename:  "proof.pdf",
					SizeBytes: size,
				},
			},
		},
	)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/telegram/webhook", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	response, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	var result struct {
		ID uuid.UUID `json:"task_id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
	require.NotEqual(t, uuid.Nil, result.ID)
	return result.ID
}
func fileInfo(t *testing.T, h *harness, token string, id uuid.UUID) files.Metadata {
	t.Helper()
	code, data := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/files/info",
		map[string]any{"protocol_version": 1, "operation_id": "info", "arguments": map[string]any{"external_id": id}},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotContains(t, string(data), "private-telegram-file")
	var result struct {
		Result files.Metadata `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	return result.Result
}
func TestFileIntakePersistsOpaqueProvenanceAndRecoversAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileBytes = []byte("%PDF-1.4\nfixture document")
	h := setup(t, p)
	ownerID, owner := registrationUser(t, h, 456, false)
	_, other := registrationUser(t, h, 9952, false)
	sentAt := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	taskID := submitFile(t, h, 181, sentAt)
	require.Equal(t, taskID, submitFile(t, h, 181, sentAt))
	h.await(t, taskID, "awaiting_authorization")
	task, err := h.store.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, ownerID, task.UserID)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	id := intent.Attachments[0].ID
	before := fileInfo(t, h, owner, id)
	require.Equal(t, sentAt, before.SentAt)
	require.True(t, before.AcceptedAt.After(sentAt))
	require.Equal(t, taskID, before.TaskID)
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, taskID, "completed")
	require.Equal(t, before, fileInfo(t, h, owner, id))
	p.mu.Lock()
	input := p.createdInputs[len(p.createdInputs)-1]
	p.mu.Unlock()
	require.Contains(t, input, id.String())
	require.Contains(t, input, "Посмотри этот документ")
	require.NotContains(t, input, "private-telegram-file")
	require.NotContains(t, input, "%PDF-")
	code, data := adminRequest(
		t,
		h,
		owner,
		http.MethodPost,
		"/internal/agent/files/fetch",
		map[string]any{"protocol_version": 1, "operation_id": "fetch", "arguments": map[string]any{"external_id": id}},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotContains(t, string(data), "test-token")
	var fetched struct {
		Artifacts []struct {
			ExternalID  string `json:"external_id"`
			FetchMethod string `json:"fetch_method"`
			Path        string `json:"download_path"`
			MIME        string `json:"mime_type"`
			SHA         string `json:"sha256"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(data, &fetched))
	require.Len(t, fetched.Artifacts, 1)
	descriptor := fetched.Artifacts[0]
	require.Equal(t, id.String(), descriptor.ExternalID)
	require.Equal(t, "fetch", descriptor.FetchMethod)
	require.Equal(t, "application/pdf", descriptor.MIME)
	require.Len(t, descriptor.SHA, 64)
	server := httptest.NewServer(h.endpoint.InternalHandler())
	t.Cleanup(server.Close)
	req, err := http.NewRequest(http.MethodGet, server.URL+descriptor.Path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+owner)
	response, err := server.Client().Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, p.fileBytes, raw)
	code, _ = adminRequest(
		t,
		h,
		other,
		http.MethodPost,
		"/internal/agent/files/info",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "foreign",
			"arguments":        map[string]any{"external_id": id},
		},
	)
	require.Equal(t, http.StatusNotFound, code)
	p.mu.Lock()
	p.fileGone = true
	p.mu.Unlock()
	code, data = adminRequest(
		t,
		h,
		owner,
		http.MethodPost,
		"/internal/agent/files/fetch",
		map[string]any{"protocol_version": 1, "operation_id": "lost", "arguments": map[string]any{"external_id": id}},
	)
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.NotContains(t, string(data), "artifacts")
	require.NotContains(t, string(data), "test-token")
	require.NotContains(t, string(data), "private-telegram-file")
	require.Equal(t, before, fileInfo(t, h, owner, id))
	response, err = server.Client().Do(req)
	require.NoError(t, err)
	lostBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	require.NotContains(t, string(lostBody), string(p.fileBytes))
	require.NotContains(t, string(lostBody), "test-token")
	require.NotContains(t, string(lostBody), "private-telegram-file")
	h.stop(t)
	h.start(t)
	server.Close()
	server = httptest.NewServer(h.endpoint.InternalHandler())
	t.Cleanup(server.Close)
	req, err = http.NewRequest(http.MethodGet, server.URL+descriptor.Path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+owner)
	require.Equal(t, before, fileInfo(t, h, owner, id))
	p.mu.Lock()
	p.fileGone = false
	p.mu.Unlock()
	code, data = adminRequest(
		t,
		h,
		owner,
		http.MethodPost,
		"/internal/agent/files/fetch",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "restored",
			"arguments":        map[string]any{"external_id": id},
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	restored := fetched
	restored.Artifacts = nil
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, fetched.Artifacts, restored.Artifacts)
	response, err = server.Client().Do(req)
	require.NoError(t, err)
	recoveredBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, p.fileBytes, recoveredBody)
	require.Equal(t, before, fileInfo(t, h, owner, id))
	_, err = h.store.SetUserRevoked(context.Background(), ownerID, "fixture")
	require.NoError(t, err)
	code, _ = adminRequest(
		t,
		h,
		owner,
		http.MethodPost,
		"/internal/agent/files/info",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "revoked",
			"arguments":        map[string]any{"external_id": id},
		},
	)
	require.Equal(t, http.StatusForbidden, code)
}

func TestPrimaryMediaKindsAreAcceptedAsOpaqueReferences(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileBytes = []byte("%PDF-1.4\nfixture")
	h := setup(t, p)
	_, owner := registrationUser(t, h, 456, false)
	small := telegram.MediaFile{FileID: "small-photo", UniqueID: "small-unique", Width: 10, Height: 10}
	large := telegram.MediaFile{FileID: "large-photo", UniqueID: "large-unique", Width: 100, Height: 100}
	media := telegram.MediaFile{FileID: "media-reference", UniqueID: "media-unique"}
	messages := []struct {
		kind     string
		message  telegram.Message
		wantedID string
	}{
		{"photo", telegram.Message{Photo: []telegram.MediaFile{large, small}}, large.FileID},
		{"audio", telegram.Message{Audio: &media}, media.FileID},
		{"voice", telegram.Message{Voice: &media}, media.FileID},
		{"video", telegram.Message{Video: &media}, media.FileID},
		{"video_note", telegram.Message{VideoNote: &media}, media.FileID},
		{"animation", telegram.Message{Animation: &media, Document: &media}, media.FileID},
	}
	for index, tc := range messages {
		m := tc.message
		m.ID = int64(200 + index)
		m.Date = time.Now().Unix()
		m.From = &telegram.User{ID: 456, LanguageCode: "uk"}
		m.Chat = telegram.Chat{ID: 456, Type: "private"}
		raw, err := json.Marshal(telegram.Update{ID: m.ID, Message: &m})
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPost, h.server.URL+"/telegram/webhook", bytes.NewReader(raw))
		require.NoError(t, err)
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
		response, err := h.server.Client().Do(req)
		require.NoError(t, err)
		var accepted struct {
			ID uuid.UUID `json:"task_id"`
		}
		require.NoError(t, json.NewDecoder(response.Body).Decode(&accepted))
		response.Body.Close()
		require.Equal(t, http.StatusAccepted, response.StatusCode)
		task, err := h.store.GetTask(context.Background(), accepted.ID)
		require.NoError(t, err)
		var saved struct {
			Attachments []files.Incoming `json:"attachments"`
			Language    string           `json:"preferred_language"`
		}
		require.NoError(t, json.Unmarshal(task.Request, &saved))
		require.Len(t, saved.Attachments, 1)
		require.Equal(t, "ru", saved.Language)
		info := fileInfo(t, h, owner, saved.Attachments[0].ID)
		require.Equal(t, tc.kind, info.Kind)
		code, data := adminRequest(
			t,
			h,
			owner,
			http.MethodPost,
			"/internal/agent/files/fetch",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     "media-fetch",
				"arguments":        map[string]any{"external_id": info.ID},
			},
		)
		require.Equal(t, http.StatusOK, code, string(data))
		p.mu.Lock()
		requestedID := p.fileIDs[len(p.fileIDs)-1]
		p.mu.Unlock()
		require.Equal(t, tc.wantedID, requestedID)
	}
}
