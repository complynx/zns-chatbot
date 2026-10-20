package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
)

// Keep the real public HTTP handler available while River workers are stopped.
func avatarFinalizationAPISetup(t *testing.T, processor avatar.Processor) (avatarDraftFixture, db.AvatarDraft) {
	t.Helper()
	f := avatarDraftSetup(t, processor)
	d := createPreviewDraft(t, f)
	f.h.stop(t)
	f.h.server = httptest.NewServer(f.h.endpoint.PublicHandler())
	return f, d
}
func nativeAvatarFinalizePath(id uuid.UUID) string {
	return "/app/avatars/drafts/" + id.String() + "/finalize"
}
func avatarFinalizeBody(t *testing.T, op uuid.UUID, version int64) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"operation_id": op, "expected_version": version})
	require.NoError(t, err)
	return string(raw)
}
func nativeAvatarReceipt(t *testing.T, raw []byte) db.AvatarDraftReceipt {
	t.Helper()
	var result db.AvatarDraftReceipt
	require.NoError(t, json.Unmarshal(raw, &result))
	require.NotEqual(t, uuid.Nil, result.ArtifactID)
	return result
}
func agentAvatarReceipt(t *testing.T, raw []byte) db.AvatarDraftReceipt {
	t.Helper()
	var result struct {
		Status string                `json:"status"`
		Result db.AvatarDraftReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	return result.Result
}

func avatarFinalizeAgent(
	t *testing.T,
	f avatarDraftFixture,
	op uuid.UUID,
	draft uuid.UUID,
	version int64,
) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		f.h,
		f.owner,
		http.MethodPost,
		"/internal/agent/avatars/finalize_draft",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     op,
			"idempotency_key":  op,
			"arguments":        map[string]any{"draft_id": draft, "expected_version": version},
		},
	)
}
func TestAvatarFinalizationAPINativeThenAgentReadsAndDeliveryWithoutLoom(t *testing.T) {
	t.Parallel()
	f, d := avatarFinalizationAPISetup(t, nil)
	op := uuid.New()
	before := submitFile(t, f.h, 30701, time.Now().UTC().Truncate(time.Second))
	code, raw, _ := appCall(
		t,
		f.h,
		http.MethodPost,
		nativeAvatarFinalizePath(d.ID),
		avatarFinalizeBody(t, op, 1),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := nativeAvatarReceipt(t, raw)
	avatarIntentCount(t, f, 1)
	// Finalizing itself creates no reactive task.
	var tasks int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks WHERE user_id=$1", f.scope.UserID).
			Scan(&tasks),
	)
	require.Equal(t, 1, tasks)
	code, raw = avatarDraftAgent(t, f, "finalization_operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, receipt, agentAvatarReceipt(t, raw))
	schemas := avatarOutputSchemas(t, avatarFixture{h: f.h, owner: f.owner})
	validateAvatarOutput(t, schemas["finalization_operation"], raw)
	code, raw = avatarDraftAgent(t, f, "finalized", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["finalized"], raw)
	listCode, listRaw := avatarDraftAgent(t, f, "finalized_list", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusOK, listCode, string(listRaw))
	validateAvatarOutput(t, schemas["finalized_list"], listRaw)
	require.Contains(t, string(listRaw), receipt.ArtifactID.String())
	listCode, listRaw, _ = appCall(t, f.h, http.MethodGet, "/app/avatars/finalized", "", f.session.Token, "")
	require.Equal(t, http.StatusOK, listCode, string(listRaw))
	require.Contains(t, string(listRaw), receipt.ArtifactID.String())
	require.Contains(t, string(raw), `"delivery_state":"pending"`)
	for _, key := range []string{"telegram_user_id", "attempt_id", "previous_receipt"} {
		require.NotContains(t, string(raw), key)
	}
	code, raw = avatarDraftAgent(t, f, "fetch_finalized", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["fetch_finalized"], raw)
	var descriptor struct {
		Artifacts []struct {
			DownloadPath string `json:"download_path"`
			SHA256       string `json:"sha256"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(raw, &descriptor))
	require.Len(t, descriptor.Artifacts, 1)
	code, jpeg, headers := avatarPreviewRead(f.h, descriptor.Artifacts[0].DownloadPath, f.owner, true)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, receipt.SHA256, previewDigest(jpeg))
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Contains(t, headers.Get("Content-Disposition"), "zinusya-avatar.jpg")
	code, native, headers := avatarPreviewRead(
		f.h,
		"/app/avatars/finalized/"+receipt.ArtifactID.String()+"/content",
		f.session.Token,
		false,
	)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, jpeg, native)
	require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
	// Native finalization is available only as historical context for a later user task.
	actions, err := f.h.store.RecentNativeAvatarDraftActions(t.Context(), f.scope, before)
	require.NoError(t, err)
	for _, a := range actions {
		require.NotEqual(t, "finalize", a.Kind)
	}
	later := submitFile(t, f.h, 30702, time.Now().UTC().Truncate(time.Second))
	actions, err = f.h.store.RecentNativeAvatarDraftActions(t.Context(), f.scope, later)
	require.NoError(t, err)
	require.LessOrEqual(t, len(actions), 5)
	require.Equal(t, "finalize", actions[0].Kind)
	require.Equal(t, op, actions[0].OperationID)
	require.NotNil(t, actions[0].ArtifactID)
	require.Equal(t, receipt.ArtifactID, *actions[0].ArtifactID)
	require.Equal(t, receipt.DeliveryID, *actions[0].DeliveryID)
	f.h.stop(t)
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivered")
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/finalized/"+receipt.ArtifactID.String(),
		"",
		f.session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"delivery_state":"delivered"`)
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, sent)
}
func TestAvatarFinalizationAPIAgentThenNativeReplaySharesImmutableReceipt(t *testing.T) {
	t.Parallel()
	f, d := avatarFinalizationAPISetup(t, nil)
	op := uuid.New()
	code, raw := avatarFinalizeAgent(t, f, op, d.ID, 1)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := agentAvatarReceipt(t, raw)
	schemas := avatarOutputSchemas(t, avatarFixture{h: f.h, owner: f.owner})
	validateAvatarOutput(t, schemas["finalize_draft"], raw)
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		nativeAvatarFinalizePath(d.ID),
		avatarFinalizeBody(t, op, 1),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, receipt, nativeAvatarReceipt(t, raw))
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/finalizations/operations/"+op.String(),
		"",
		f.session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, receipt, nativeAvatarReceipt(t, raw))
	avatarIntentCount(t, f, 1)
	var native bool
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT native FROM shuttle_avatars WHERE id=$1", receipt.ArtifactID).
			Scan(&native),
	)
	require.False(t, native, "A cross-path replay keeps the original admission provenance")
	// The old operation API never parses a new receipt as legacy origin.
	code, raw = avatarDraftAgent(t, f, "operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
}
func TestAvatarFinalizationAPIReplaySkipsRendererAfterLaterDraftEdit(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var unavailable atomic.Bool
	worker := avatarExecutable(t)
	processor := avatarProcessorFunc(
		func(ctx context.Context, raw []byte, tr avatar.Transform) (avatar.Rendered, error) {
			calls.Add(1)
			if unavailable.Load() {
				return avatar.Rendered{}, avatar.ErrProcessorUnavailable
			}
			return worker.Render(ctx, raw, tr)
		},
	)
	f, d := avatarFinalizationAPISetup(t, processor)
	op := uuid.New()
	code, raw := avatarFinalizeAgent(t, f, op, d.ID, 1)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := agentAvatarReceipt(t, raw)
	renderCalls := calls.Load()
	_, err := f.h.store.SaveAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftSave{
			OperationID:     uuid.New(),
			DraftID:         d.ID,
			ExpectedVersion: 1,
			Transform:       avatar.Transform{ZoomPPM: 2 * avatar.FramePPM},
		},
	)
	require.NoError(t, err)
	unavailable.Store(true)
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		nativeAvatarFinalizePath(d.ID),
		avatarFinalizeBody(t, op, 1),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, receipt, nativeAvatarReceipt(t, raw))
	require.Equal(t, renderCalls, calls.Load())
	code, raw = avatarFinalizeAgent(t, f, op, d.ID, 2)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.Equal(t, renderCalls, calls.Load())
	code, raw = avatarFinalizeAgent(t, f, uuid.New(), d.ID, 1)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.Equal(t, renderCalls, calls.Load())
	avatarIntentCount(t, f, 1)
}
func TestAvatarFinalizationAPIForeignOwnerAndNativeOriginGuards(t *testing.T) {
	t.Parallel()
	f, d := avatarFinalizationAPISetup(t, nil)
	op := uuid.New()
	code, raw := avatarFinalizeAgent(t, f, op, d.ID, 1)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := agentAvatarReceipt(t, raw)
	_, other := registrationUser(t, f.h, 777, false)
	foreign := f
	foreign.owner = other
	foreign.session = detailsAppSession(t, f.h, 777, 213)
	code, raw = avatarDraftAgent(t, foreign, "finalized_list", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), receipt.ArtifactID.String())
	for _, method := range []string{"finalized", "fetch_finalized"} {
		code, raw = avatarDraftAgent(t, foreign, method, uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	code, raw = avatarDraftAgent(t, foreign, "finalization_operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	for _, path := range []string{"/app/avatars/finalized/" + receipt.ArtifactID.String(), "/app/avatars/finalizations/operations/" + op.String(), "/app/avatars/finalized/" + receipt.ArtifactID.String() + "/content"} {
		code, raw, _ = appCall(t, f.h, http.MethodGet, path, "", foreign.session.Token, "")
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		nativeAvatarFinalizePath(d.ID),
		avatarFinalizeBody(t, uuid.New(), 1),
		f.session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		nativeAvatarFinalizePath(d.ID),
		avatarFinalizeBody(t, uuid.New(), 0),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	avatarIntentCount(t, f, 1)
}
func TestAvatarFinalizationAPIRechecksDraftAndNativeSessionDuringRender(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"edit", "expired"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			var armed atomic.Bool
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			worker := avatarExecutable(t)
			processor := avatarProcessorFunc(
				func(ctx context.Context, raw []byte, tr avatar.Transform) (avatar.Rendered, error) {
					if armed.Swap(false) {
						close(entered)
						select {
						case <-release:
						case <-ctx.Done():
							return avatar.Rendered{}, ctx.Err()
						}
					}
					return worker.Render(ctx, raw, tr)
				},
			)
			f, d := avatarFinalizationAPISetup(t, processor)
			armed.Store(true)
			body := avatarFinalizeBody(t, uuid.New(), 1)
			type answer struct {
				code int
				err  error
			}
			done := make(chan answer, 1)
			go func() {
				code, _, _, err := appHTTPCall(
					f.h,
					http.MethodPost,
					nativeAvatarFinalizePath(d.ID),
					body,
					f.session.Token,
					appTestOrigin,
				)
				done <- answer{code, err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("finalization renderer not reached")
			}
			expected := http.StatusConflict
			if kind == "edit" {
				_, err := f.h.store.SaveAvatarDraft(
					t.Context(),
					f.scope,
					db.AvatarDraftSave{
						OperationID:     uuid.New(),
						DraftID:         d.ID,
						ExpectedVersion: 1,
						Transform:       avatar.Transform{ZoomPPM: 2 * avatar.FramePPM},
					},
				)
				require.NoError(t, err)
			} else {
				_, err := f.h.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1", f.scope.UserID)
				require.NoError(t, err)
				expected = http.StatusUnauthorized
			}
			unblock()
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.Equal(t, expected, result.code)
			case <-time.After(5 * time.Second):
				t.Fatal("finalization did not finish")
			}
			avatarIntentCount(t, f, 0)
		})
	}
}
func TestAvatarFinalizationAPIConcurrentNativeAgentCreatesOneDocument(t *testing.T) {
	t.Parallel()
	f, d := avatarFinalizationAPISetup(t, nil)
	op := uuid.New()
	nativeBody := avatarFinalizeBody(t, op, 1)
	agentBody, err := json.Marshal(
		map[string]any{
			"protocol_version": 1,
			"operation_id":     op,
			"idempotency_key":  op,
			"arguments":        map[string]any{"draft_id": d.ID, "expected_version": 1},
		},
	)
	require.NoError(t, err)
	type answer struct {
		native bool
		code   int
		raw    []byte
		err    error
	}
	done := make(chan answer, 2)
	go func() {
		code, raw, _, requestErr := appHTTPCall(
			f.h,
			http.MethodPost,
			nativeAvatarFinalizePath(d.ID),
			nativeBody,
			f.session.Token,
			appTestOrigin,
		)
		done <- answer{true, code, raw, requestErr}
	}()
	go func() {
		r := httptest.NewRequest(http.MethodPost, "/internal/agent/avatars/finalize_draft", bytes.NewReader(agentBody))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+f.owner)
		w := httptest.NewRecorder()
		f.h.endpoint.InternalHandler().ServeHTTP(w, r)
		done <- answer{false, w.Code, w.Body.Bytes(), nil}
	}()
	var receipts []db.AvatarDraftReceipt
	for range 2 {
		result := <-done
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.code, string(result.raw))
		if result.native {
			receipts = append(receipts, nativeAvatarReceipt(t, result.raw))
		} else {
			receipts = append(receipts, agentAvatarReceipt(t, result.raw))
		}
	}
	require.Equal(t, receipts[0], receipts[1])
	avatarIntentCount(t, f, 1)
	code, raw, _ := appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/finalized/"+receipts[0].ArtifactID.String(),
		"",
		f.session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "attempt_id")
}

func TestAvatarFinalizationAPIConcurrentReplayAfterRenderConflict(t *testing.T) {
	t.Parallel()
	var armed atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	worker := avatarExecutable(t)
	processor := avatarProcessorFunc(
		func(ctx context.Context, raw []byte, tr avatar.Transform) (avatar.Rendered, error) {
			if armed.Swap(false) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return avatar.Rendered{}, ctx.Err()
				}
			}
			return worker.Render(ctx, raw, tr)
		},
	)
	f, d := avatarFinalizationAPISetup(t, processor)
	op := uuid.New()
	body := avatarFinalizeBody(t, op, 1)
	armed.Store(true)
	type answer struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		code, raw, _, err := appHTTPCall(
			f.h,
			http.MethodPost,
			nativeAvatarFinalizePath(d.ID),
			body,
			f.session.Token,
			appTestOrigin,
		)
		done <- answer{code, raw, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("native finalization renderer not reached")
	}
	code, raw := avatarFinalizeAgent(t, f, op, d.ID, 1)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := agentAvatarReceipt(t, raw)
	_, err := f.h.store.SaveAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftSave{
			OperationID:     uuid.New(),
			DraftID:         d.ID,
			ExpectedVersion: 1,
			Transform:       avatar.Transform{ZoomPPM: 2 * avatar.FramePPM},
		},
	)
	require.NoError(t, err)
	avatarIntentCount(t, f, 1)
	unblock()
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.code, string(result.raw))
		require.Equal(t, receipt, nativeAvatarReceipt(t, result.raw))
	case <-time.After(5 * time.Second):
		t.Fatal("native finalization did not finish")
	}
	avatarIntentCount(t, f, 1)
}
