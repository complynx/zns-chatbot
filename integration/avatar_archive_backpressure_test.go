package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
)

// Hold an admitted archive upload at its body read, without touching avatar sources.
func holdAvatarArchiveValidator(t *testing.T, h *harness) func() {
	t.Helper()
	var adminTelegram int64
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT greatest(coalesce(max(tg_user_id),0),9200)+1 FROM shuttle_users").
			Scan(&adminTelegram),
	)
	_, admin := registrationUser(t, h, adminTelegram, true)
	raw, digest := receivingArchive(t, 123)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = &archiveReadSignal{ReadCloser: r.Body, started: started}
		h.endpoint.InternalHandler().ServeHTTP(w, r)
	}))
	reader, writer := io.Pipe()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(func() { cancel(); _ = reader.Close(); _ = writer.Close(); server.Close() })
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
		response, callErr := server.Client().Do(request)
		if callErr == nil {
			_, callErr = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				callErr = io.ErrUnexpectedEOF
			}
		}
		done <- callErr
	}()
	_, err = writer.Write(raw[:1])
	require.NoError(t, err)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("archive upload was not admitted")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_, err = writer.Write(raw[1:])
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			require.NoError(t, <-done)
		})
	}
}

func TestAvatarOrdinarySourcesRemainAvailableWhileArchiveValidatorBusy(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	uploaded := createPreviewDraft(t, f)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.mu.Unlock()
	taskID := submitFile(t, f.h, 29701, time.Now().UTC())
	task, err := f.h.store.GetTask(t.Context(), taskID)
	require.NoError(t, err)
	var request struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &request))
	require.Len(t, request.Attachments, 1)
	createCode, body := avatarDraftAgent(
		t,
		f,
		"create_draft",
		uuid.New(),
		map[string]any{"source_file_id": request.Attachments[0].ID},
	)
	require.Equal(t, http.StatusOK, createCode, string(body))
	telegram := avatarDraftAgentResult(t, body)
	f.h.stop(t)
	f.h.server = httptest.NewServer(f.h.endpoint.PublicHandler())
	release := holdAvatarArchiveValidator(t, f.h)
	for _, draft := range []db.AvatarDraft{uploaded, telegram} {
		code, raw, _ := avatarPreviewRead(
			f.h,
			"/app/avatars/drafts/"+draft.ID.String()+"/original",
			f.session.Token,
			false,
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, f.raw, raw)
		code, raw, _ = avatarPreviewRead(f.h, avatarPreviewURL(draft.ID, draft.Version), f.session.Token, false)
		require.Equal(t, http.StatusOK, code, string(raw))
		code, raw, _ = appCall(
			t,
			f.h,
			http.MethodPost,
			nativeAvatarFinalizePath(draft.ID),
			avatarFinalizeBody(t, uuid.New(), draft.Version),
			f.session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.NotEqual(t, uuid.Nil, nativeAvatarReceipt(t, raw).ArtifactID)
	}
	release()
}

type archiveAvatarResponse struct {
	code int
	raw  []byte
	err  error
}

type archiveAvatarRequest struct {
	url           string
	authorization string
	body          string
	native        bool
}

func (a archiveAvatarRequest) call(parent context.Context) archiveAvatarResponse {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, strings.NewReader(a.body))
	if err != nil {
		return archiveAvatarResponse{err: err}
	}
	req.Header.Set("Authorization", a.authorization)
	req.Header.Set("Content-Type", "application/json")
	if a.native {
		req.Header.Set("Origin", appTestOrigin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return archiveAvatarResponse{err: err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	return archiveAvatarResponse{res.StatusCode, raw, err}
}

func archiveAvatarWrite(t *testing.T, f avatarDraftFixture, internal string, kind string, native bool,
	operation uuid.UUID, draft db.AvatarDraft, source db.AvatarHistorySource,
) archiveAvatarRequest {
	t.Helper()
	args := map[string]any{"history_source": source}
	method, path := "create_draft", "/app/avatars/drafts"
	if kind == "finalize" {
		args = map[string]any{"draft_id": draft.ID, "expected_version": draft.Version}
		method, path = "finalize_draft", nativeAvatarFinalizePath(draft.ID)
	}
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
	}
	request := archiveAvatarRequest{
		url:           internal + "/internal/agent/avatars/" + method,
		authorization: "Bearer " + f.owner,
		native:        native,
	}
	if native {
		request.url, request.authorization = f.h.server.URL+path, "ZinusyaSession "+f.session.Token
		body = map[string]any{"operation_id": operation, "history_source": source}
		if kind == "finalize" {
			body = map[string]any{"operation_id": operation, "expected_version": draft.Version}
		}
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	request.body = string(raw)
	return request
}

func TestAvatarHistoricalCommitsRespectArchiveBackpressureAndReplay(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, kind string
		native     bool
	}{
		{"create/agent", "create", false}, {"create/native", "create", true},
		{"finalize/agent", "finalize", false}, {"finalize/native", "finalize", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			checkAvatarHistoricalCommit(t, c.kind, c.native)
		})
	}
}

func checkAvatarHistoricalCommit(t *testing.T, kind string, native bool) {
	t.Helper()
	entered, resume := make(chan struct{}, 1), make(chan struct{})
	var active atomic.Bool
	worker := avatarExecutable(t)
	processor := avatarProcessorFunc(
		func(ctx context.Context, raw []byte, transform avatar.Transform) (avatar.Rendered, error) {
			result, err := worker.Render(ctx, raw, transform)
			if err == nil && active.Load() {
				select {
				case entered <- struct{}{}:
				case <-ctx.Done():
					return avatar.Rendered{}, ctx.Err()
				}
				select {
				case <-resume:
				case <-ctx.Done():
					return avatar.Rendered{}, ctx.Err()
				}
			}
			return result, err
		},
	)
	f := avatarDraftSetup(t, processor)
	source := avatarHistoryFixture(t, f)
	var draft db.AvatarDraft
	if kind == "finalize" {
		code, raw := avatarDraftAgent(t, f, "create_draft", uuid.New(), map[string]any{"history_source": source})
		require.Equal(t, http.StatusOK, code, string(raw))
		draft = avatarDraftAgentResult(t, raw)
	}
	f.h.stop(t)
	f.h.server = httptest.NewServer(f.h.endpoint.PublicHandler())
	internal := httptest.NewServer(f.h.endpoint.InternalHandler())
	defer internal.Close()
	request := archiveAvatarWrite(t, f, internal.URL, kind, native, uuid.New(), draft, source)
	active.Store(true)
	done := make(chan archiveAvatarResponse, 1)
	go func() { done <- request.call(t.Context()) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("avatar did not finish rendering")
	}
	release := holdAvatarArchiveValidator(t, f.h)
	active.Store(false)
	close(resume)
	rejected := <-done
	require.NoError(t, rejected.err)
	require.Equal(t, http.StatusServiceUnavailable, rejected.code, string(rejected.raw))
	var count int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatars").Scan(&count))
	require.Zero(t, count)
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatar_drafts").Scan(&count),
	)
	if kind == "create" {
		require.Zero(t, count)
	} else {
		require.Equal(t, 1, count)
	}
	release()
	accepted := request.call(t.Context())
	require.NoError(t, accepted.err)
	require.Equal(t, http.StatusOK, accepted.code, string(accepted.raw))
	release = holdAvatarArchiveValidator(t, f.h)
	replayed := request.call(t.Context())
	require.NoError(t, replayed.err)
	require.Equal(t, http.StatusOK, replayed.code, string(replayed.raw))
	require.JSONEq(t, string(accepted.raw), string(replayed.raw))
	release()
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatar_drafts").Scan(&count),
	)
	require.Equal(t, 1, count)
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatars").Scan(&count))
	if kind == "create" {
		require.Zero(t, count)
	} else {
		require.Equal(t, 1, count)
	}
}
