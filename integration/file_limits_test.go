package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/files"
)

func sizedFileID(t *testing.T, h *harness, updateID, size int64) uuid.UUID {
	t.Helper()
	taskID := submitSizedFile(t, h, updateID, time.Now(), size)
	task, err := h.store.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	return intent.Attachments[0].ID
}

func TestKnownOversizeFileRejectsReadingBeforeTelegramIO(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileGone = true
	h := setup(t, p)
	ownerID, owner := registrationUser(t, h, 456, false)
	_, foreign := registrationUser(t, h, 9952, false)
	id := sizedFileID(t, h, 981, files.MaxReadBytes+1)
	require.Equal(t, files.MaxReadBytes+1, fileInfo(t, h, owner, id).SizeBytes)

	assertReads := func(token string, allowed ...int) {
		t.Helper()
		code, _ := adminRequest(
			t,
			h,
			token,
			http.MethodPost,
			"/internal/agent/files/fetch",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     "oversize",
				"arguments":        map[string]any{"external_id": id},
			},
		)
		require.Contains(t, allowed, code)
		server := httptest.NewServer(h.endpoint.InternalHandler())
		defer server.Close()
		req, requestErr := http.NewRequest(http.MethodGet, server.URL+"/internal/files/"+id.String()+"/content", nil)
		require.NoError(t, requestErr)
		req.Header.Set("Authorization", "Bearer "+token)
		response, requestErr := server.Client().Do(req)
		require.NoError(t, requestErr)
		response.Body.Close()
		require.Contains(t, allowed, response.StatusCode)
		p.mu.Lock()
		gets := p.fileGets
		p.mu.Unlock()
		require.Zero(t, gets)
	}
	assertReads(owner, http.StatusRequestEntityTooLarge)
	assertReads(foreign, http.StatusNotFound)
	_, err := h.store.SetUserRevoked(context.Background(), ownerID, "fixture")
	require.NoError(t, err)
	// In-flight cache invalidation can reject identity before the local account check.
	assertReads(owner, http.StatusForbidden, http.StatusUnauthorized)
}

func TestFileAtDeclaredReadLimitStillUsesBoundedReader(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileBytes = []byte("%PDF-1.4\nsmall actual body")
	h := setup(t, p)
	_, owner := registrationUser(t, h, 456, false)
	for index, size := range []int64{0, files.MaxReadBytes} {
		id := sizedFileID(t, h, int64(982+index), size)
		code, _ := adminRequest(
			t,
			h,
			owner,
			http.MethodPost,
			"/internal/agent/files/fetch",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     strconv.Itoa(index),
				"arguments":        map[string]any{"external_id": id},
			},
		)
		require.Equal(t, http.StatusOK, code)
	}
	p.mu.Lock()
	gets := p.fileGets
	p.mu.Unlock()
	require.Equal(t, 2, gets)
}

func TestUnknownFileSizeUsesCloudRefusalWithoutExposingForeignMetadata(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileTooLarge = true
	h := setup(t, p)
	ownerID, owner := registrationUser(t, h, 456, false)
	_, foreign := registrationUser(t, h, 9952, false)
	id := sizedFileID(t, h, 983, 0)
	require.Zero(t, fileInfo(t, h, owner, id).SizeBytes)
	server := httptest.NewServer(h.endpoint.InternalHandler())
	t.Cleanup(server.Close)
	assertReads := func(token string, expectedGets int, allowed ...int) {
		t.Helper()
		code, _ := adminRequest(t, h, token, http.MethodPost, "/internal/agent/files/fetch", map[string]any{
			"protocol_version": 1, "operation_id": "unknown-size",
			"arguments": map[string]any{"external_id": id},
		})
		require.Contains(t, allowed, code)
		req, err := http.NewRequest(http.MethodGet, server.URL+"/internal/files/"+id.String()+"/content", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(req)
		require.NoError(t, err)
		response.Body.Close()
		require.Contains(t, allowed, response.StatusCode)
		p.mu.Lock()
		gets := p.fileGets
		p.mu.Unlock()
		require.Equal(t, expectedGets, gets)
	}
	assertReads(foreign, 0, http.StatusNotFound)
	assertReads(owner, 2, http.StatusRequestEntityTooLarge)
	p.mu.Lock()
	p.fileGone = true
	p.mu.Unlock()
	assertReads(owner, 4, http.StatusServiceUnavailable)
	_, err := h.store.SetUserRevoked(t.Context(), ownerID, "fixture")
	require.NoError(t, err)
	// In-flight cache invalidation can reject identity before the local account check.
	assertReads(owner, 4, http.StatusForbidden, http.StatusUnauthorized)
}
