package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func receivingArchive(t *testing.T, bot int64) ([]byte, string) {
	t.Helper()
	return receivingArchiveData(t, bot, "")
}

func receivingArchiveData(t *testing.T, bot int64, padding string) ([]byte, string) {
	t.Helper()
	root := t.TempDir()
	raw, err := json.Marshal(
		map[string]any{"_id": "u", "bot_id": bot, "user_id": 42, "name": "PRIVATE_ARCHIVE_NAME", "padding": padding},
	)
	require.NoError(t, err)
	raw = append(raw, '\n')
	digest := sha256.Sum256(raw)
	require.NoError(t, os.WriteFile(filepath.Join(root, "users.ndjson"), raw, 0o600))
	manifest := legacyimport.Manifest{Version: 1, SourceInstance: "synthetic-api", BotID: bot,
		DatePolicy: "preserve_source", Collections: []legacyimport.Collection{{Role: "users", Name: "users",
			Path: "users.ndjson", SHA256: hex.EncodeToString(digest[:])}}}
	raw, err = json.Marshal(manifest)
	require.NoError(t, err)
	manifestPath := filepath.Join(root, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))
	archivePath := filepath.Join(root, "archive.tar.gz")
	result, err := legacyimport.CreateBundle(manifestPath, archivePath)
	require.NoError(t, err)
	require.NotNil(t, result.Archive)
	raw, err = os.ReadFile(archivePath)
	require.NoError(t, err)
	return raw, result.Archive.SHA256
}

func archiveUpload(t *testing.T, h *harness, token, digest string, raw []byte) (int, []byte) {
	t.Helper()
	server := httptest.NewServer(h.endpoint.InternalHandler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/internal/admin/legacy/validate", bytes.NewReader(raw))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/gzip")
	request.Header.Set("X-Content-Sha256", digest)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, result
}

func TestArchiveUploadIsAdminOnlyBotScopedAndReadOnly(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, ordinary := registrationUser(t, h, 9001, false)
	_, admin := registrationUser(t, h, 9002, true)
	raw, digest := receivingArchive(t, 123)
	code, data := archiveUpload(t, h, ordinary, digest, raw)
	require.Equal(t, http.StatusForbidden, code, string(data))
	code, data = archiveUpload(t, h, admin, digest, raw)
	require.Equal(t, http.StatusOK, code, string(data))
	var result legacyimport.BundleValidation
	require.NoError(t, json.Unmarshal(data, &result))
	require.True(t, result.Inspection.ValidationPassed)
	require.False(t, result.Inspection.ImportPerformed)
	require.False(t, result.Inspection.CompleteTelegramHistory)
	require.NotContains(t, string(data), "PRIVATE_ARCHIVE_NAME")
	var users, tasks int
	require.NoError(t, h.store.Pool().QueryRow(context.Background(), "SELECT count(*) FROM shuttle_users").Scan(&users))
	require.NoError(t, h.store.Pool().QueryRow(context.Background(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Equal(t, 2, users, "validation must not create the archive user")
	require.Zero(t, tasks, "validation must not queue imports or replies")
	code, _ = archiveUpload(t, h, admin, strings.Repeat("0", 64), raw)
	require.Equal(t, http.StatusBadRequest, code)
	code, _ = archiveUpload(t, h, admin, digest, []byte("PRIVATE_CORRUPTION"))
	require.Equal(t, http.StatusBadRequest, code)
	foreign, foreignDigest := receivingArchive(t, 124)
	code, _ = archiveUpload(t, h, admin, foreignDigest, foreign)
	require.Equal(t, http.StatusConflict, code)
}

func TestAgentArchiveValidationUsesOwnedFileAndRechecksRevocation(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	raw, digest := receivingArchive(t, 123)
	p.fileBytes = raw
	h := setup(t, p)
	ownerID, admin := registrationUser(t, h, 456, true)
	_, otherAdmin := registrationUser(t, h, 9002, true)
	_, ordinary := registrationUser(t, h, 9003, false)
	taskID := submitFile(t, h, 98101, time.Now().UTC())
	task, err := h.store.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	// Isolate the validator's local revocation check from worker token invalidation.
	h.stop(t)
	args := map[string]any{"source_file_id": intent.Attachments[0].ID, "sha256": digest}
	code, data := agentAdminRead(t, h, ordinary, "validate_archive", args)
	require.Equal(t, http.StatusForbidden, code, string(data))
	code, data = agentAdminRead(t, h, otherAdmin, "validate_archive", args)
	require.Equal(t, http.StatusNotFound, code, string(data))
	p.mu.Lock()
	reads := p.fileGets
	p.mu.Unlock()
	require.Zero(t, reads, "denied reads must not contact Telegram")
	code, data = agentAdminRead(t, h, admin, "validate_archive", args)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"import_performed":false`)
	require.NotContains(t, string(data), "PRIVATE_ARCHIVE_NAME")
	p.mu.Lock()
	p.fileReadHook = func() { _, _ = h.store.SetUserRevoked(context.Background(), ownerID, "fixture") }
	p.mu.Unlock()
	code, data = agentAdminRead(t, h, admin, "validate_archive", args)
	require.Equal(t, http.StatusForbidden, code, string(data))
	require.NotContains(t, string(data), digest)
}

func TestArchiveToolIsDiscoverableOnlyToAdministrator(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, ordinary := registrationUser(t, h, 9101, false)
	_, admin := registrationUser(t, h, 9102, true)
	for _, check := range []struct {
		token   string
		visible bool
	}{{ordinary, false}, {admin, true}} {
		code, data := adminRequest(t, h, check.token, http.MethodPost, "/internal/agent/administration/discover", nil)
		require.Equal(t, http.StatusOK, code, string(data))
		require.Equal(t, check.visible, bytes.Contains(data, []byte(`"id":"validate_archive"`)))
	}
	code, _ := agentAdminRead(
		t,
		h,
		admin,
		"validate_archive",
		map[string]any{"source_file_id": uuid.New(), "sha256": "bad"},
	)
	require.Equal(t, http.StatusUnprocessableEntity, code)
}

type archiveReadSignal struct {
	io.ReadCloser

	started chan struct{}
	once    sync.Once
}

func (r *archiveReadSignal) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.once.Do(func() { close(r.started) })
	}
	return n, err
}

func TestArchiveUploadBackpressureReleasesAfterRequestFinishes(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 9201, true)
	raw, digest := receivingArchive(t, 123)
	bodyRead := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = &archiveReadSignal{ReadCloser: r.Body, started: bodyRead}
		h.endpoint.InternalHandler().ServeHTTP(w, r)
	}))
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		server.URL+"/internal/admin/legacy/validate",
		reader,
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+admin)
	request.Header.Set("Content-Type", "application/gzip")
	request.Header.Set("X-Content-Sha256", digest)
	done := make(chan error, 1)
	go func() {
		response, requestErr := server.Client().Do(request)
		if requestErr == nil {
			_, requestErr = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				requestErr = io.ErrUnexpectedEOF
			}
		}
		done <- requestErr
	}()
	_, err = writer.Write(raw[:1])
	require.NoError(t, err)
	// A transport consuming the pipe is not evidence that the handler has admitted
	// the upload. Wait for actual server-side body consumption before probing it.
	select {
	case <-bodyRead:
	case requestErr := <-done:
		t.Fatalf("stream finished before body consumption: %v", requestErr)
	case <-ctx.Done():
		t.Fatal("stream did not begin body consumption before its request deadline")
	}
	code, data := archiveUpload(t, h, admin, digest, raw)
	require.Equal(t, http.StatusServiceUnavailable, code, string(data))
	// The archive route must extend the shorter server write timeout too.
	time.Sleep(100 * time.Millisecond)
	_, err = writer.Write(raw[1:])
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, <-done)
	code, data = archiveUpload(t, h, admin, digest, raw)
	require.Equal(t, http.StatusOK, code, string(data))
}
