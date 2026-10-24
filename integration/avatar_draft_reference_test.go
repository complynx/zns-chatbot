package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
	"github.com/complynx/zns-chatbot/internal/db"
)

func avatarHistoryFixture(t *testing.T, f avatarDraftFixture) db.AvatarHistorySource {
	t.Helper()
	admin, _ := registrationUser(t, f.h, 700, true)
	scope := historyScope(t, f.h, admin)
	archive, digest := historyBundle(t, "draft-history-source", map[string][]map[string]any{
		"users": {historyUser(456)},
		"files": {{"_id": "avatar", "bot_id": 123, "user_id": 456, "mime_type": "image/png",
			"tg_file_id": "PRIVATE_HISTORICAL_FILE", "tg_file_unique_id": "PRIVATE_HISTORICAL_UNIQUE"}},
	})
	saved := saveHistoryBundle(t, f.h.store, scope, archive, digest)
	_, err := f.h.store.ApplyHistory(
		t.Context(),
		scope,
		prepareHistory(t, f.h.store, scope, saved),
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	source := db.AvatarHistorySource{ArchiveID: saved.ArchiveID}
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), `SELECT id FROM shuttle_legacy_history_records WHERE source_role='files'`).
			Scan(&source.RecordID),
	)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.mu.Unlock()
	return source
}

func TestAvatarDraftHistorySharesNativeAndAgentStateWithoutOriginalStorage(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	operation := uuid.New()
	body, err := json.Marshal(map[string]any{"operation_id": operation, "history_source": source})
	require.NoError(t, err)
	code, created, _ := appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts",
		string(body),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(created))
	draft := avatarDraftMetadata(t, created)
	require.Equal(t, &source, draft.HistorySource)
	var absent bool
	var size int64
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), `SELECT original IS NULL,source_size_bytes FROM shuttle_avatar_drafts WHERE id=$1`, draft.ID).
			Scan(&absent, &size),
	)
	require.True(t, absent)
	require.EqualValues(t, len(f.raw), size)
	code, raw := avatarDraftAgent(t, f, "save_draft", uuid.New(), map[string]any{
		"draft_id": draft.ID, "expected_version": 1, "transform": avatar.Transform{ZoomPPM: 2 * avatar.FramePPM},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	edited := avatarDraftAgentResult(t, raw)
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/drafts/"+draft.ID.String()+"/original",
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, f.raw, raw)
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/drafts/"+draft.ID.String()+"/preview/2",
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	finalOperation := uuid.New()
	args := map[string]any{"draft_id": draft.ID, "expected_version": edited.Version}
	code, raw = avatarDraftAgent(t, f, "finalize_draft", finalOperation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	var final struct {
		Result db.AvatarDraftReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &final))
	require.NotEqual(t, uuid.Nil, final.Result.ArtifactID)
	f.p.mu.Lock()
	f.p.fileGone = true
	gets := f.p.fileGets
	f.p.mu.Unlock()
	code, replayed := avatarDraftAgent(t, f, "finalize_draft", finalOperation, args)
	require.Equal(t, http.StatusOK, code, string(replayed))
	var replay struct {
		Result      db.AvatarDraftReceipt `json:"result"`
		OperationID string                `json:"operation_id"`
	}
	require.NoError(t, json.Unmarshal(replayed, &replay))
	require.Equal(t, final.Result, replay.Result)
	require.Equal(t, finalOperation.String(), replay.OperationID)
	f.p.mu.Lock()
	require.Equal(t, gets, f.p.fileGets)
	f.p.mu.Unlock()
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/drafts/"+draft.ID.String()+"/original",
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	require.Contains(t, string(raw), "send it again")
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/drafts/"+draft.ID.String()+"/preview/2",
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	require.Contains(t, string(raw), "send it again")
	for _, path := range []string{"/internal/avatar-drafts/" + draft.ID.String() + "/original", "/internal/avatar-drafts/" + draft.ID.String() + "/preview/2"} {
		code, raw = adminRequest(t, f.h, f.owner, http.MethodGet, path, nil)
		require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
		require.Contains(t, string(raw), "send it again")
	}
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/finalized/"+final.Result.ArtifactID.String()+"/content",
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotEmpty(t, raw)
	var tasks int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_tasks`).Scan(&tasks))
	require.Zero(t, tasks, "historical selection must not manufacture an agent task")
}

func TestAvatarDraftHistoryReplayBindsSourceAndSurvivesTelegramLoss(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	operation := uuid.New()
	args := map[string]any{"history_source": source}
	code, created := avatarDraftAgent(t, f, "create_draft", operation, args)
	require.Equal(t, http.StatusOK, code, string(created))
	draft := avatarDraftAgentResult(t, created)
	require.NotEqual(t, uuid.Nil, draft.ID)
	f.p.mu.Lock()
	f.p.fileGone = true
	gets := f.p.fileGets
	f.p.mu.Unlock()
	code, replayed := avatarDraftAgent(t, f, "create_draft", operation, args)
	require.Equal(t, http.StatusOK, code, string(replayed))
	require.Equal(t, draft, avatarDraftAgentResult(t, replayed))
	changed := source
	changed.ArchiveID = uuid.New()
	code, rejected := avatarDraftAgent(t, f, "create_draft", operation, map[string]any{"history_source": changed})
	require.Equal(t, http.StatusOK, code, string(rejected))
	require.Contains(t, string(rejected), `"status":"rejected"`)
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Equal(t, gets, f.p.fileGets)
}

func TestAvatarDraftHistoryRejectsBindingChangeDuringDownload(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	hookResult := make(chan error, 1)
	f.p.mu.Lock()
	f.p.fileReadHook = func() {
		_, err := f.h.store.Pool().
			Exec(t.Context(), `UPDATE shuttle_users SET tg_user_id=457 WHERE id=$1`, f.scope.UserID)
		hookResult <- err
	}
	f.p.mu.Unlock()
	code, raw := avatarDraftAgent(t, f, "create_draft", uuid.New(), map[string]any{"history_source": source})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	select {
	case hookErr := <-hookResult:
		require.NoError(t, hookErr)
	default:
		t.Fatal("Telegram download did not reach the ownership-change hook")
	}
	var drafts, operations int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_avatar_drafts),(SELECT count(*) FRO" +
"M shuttle_avatar_draft_operations)").
			Scan(&drafts, &operations),
	)
	require.Zero(t, drafts)
	require.Zero(t, operations)
}

func TestAvatarDraftHistoryRejectsChangedPhotoBeforePreview(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	code, raw := avatarDraftAgent(t, f, "create_draft", uuid.New(), map[string]any{"history_source": source})
	require.Equal(t, http.StatusOK, code, string(raw))
	draft := avatarDraftAgentResult(t, raw)
	changedPhoto := avatarDraftPhoto(t, true)
	f.p.mu.Lock()
	f.p.fileBytes = changedPhoto
	f.p.mu.Unlock()
	code, raw = avatarDraftAgent(
		t,
		f,
		"preview_draft",
		uuid.New(),
		map[string]any{"draft_id": draft.ID, "expected_version": 1},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	avatarIntentCount(t, f, 0)
}

func TestAvatarDraftHistorySingleConnectionAtomicLifecycle(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	dsn, err := url.Parse(f.h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	limited, err := db.Open(t.Context(), dsn.String())
	require.NoError(t, err)
	defer limited.Close()
	require.EqualValues(t, 1, limited.Pool().Config().MaxConns)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	rendered, err := renderer.Render(ctx, f.raw, avatar.IdentityTransform())
	require.NoError(t, err)
	draft, err := limited.CreateAvatarDraft(
		ctx,
		f.scope,
		db.AvatarDraftCreate{OperationID: uuid.New(), HistorySource: &source},
		f.raw,
		rendered,
	)
	require.NoError(t, err)
	input, err := limited.AvatarDraftSource(ctx, f.scope, draft.ID)
	require.NoError(t, err)
	require.Empty(t, input.Original)
	require.NotEmpty(t, input.TelegramID)
	receipt, err := limited.CommitAvatarDraft(
		ctx,
		f.scope,
		db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: draft.ID, ExpectedVersion: 1},
		db.AvatarDraftRender{Original: f.raw, Output: rendered},
	)
	require.NoError(t, err)
	require.Equal(t, draft.ID, receipt.DraftID)
}

func TestHistoryAvatarCandidatesDiscoverOwnedFilesAcrossAgentAndNative(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	first := avatarHistoryFixture(t, f)
	adminID, adminToken := registrationUser(t, f.h, 701, true)
	scope := historyScope(t, f.h, adminID)
	records := make([]map[string]any, 0, 22)
	for i := range 21 {
		records = append(records, map[string]any{"_id": fmt.Sprintf("candidate-%d", i), "bot_id": 123, "user_id": 456,
			"mime_type": "image/png", "tg_file_id": "PRIVATE_CANDIDATE", "tg_file_unique_id": "PRIVATE_UNIQUE"})
	}
	records = append(records, map[string]any{"_id": "foreign", "bot_id": 123, "user_id": 457,
		"mime_type": "application/pdf", "tg_file_id": "PRIVATE_FOREIGN", "tg_file_unique_id": "PRIVATE_FOREIGN_UNIQUE"})
	foreignUser := historyUser(457)
	foreignUser["_id"] = "foreign-user"
	archive, digest := historyBundle(t, "avatar-candidates", map[string][]map[string]any{
		"users": {historyUser(456), foreignUser}, "files": records,
	})
	saved := saveHistoryBundle(t, f.h.store, scope, archive, digest)
	_, err := f.h.store.ApplyHistory(
		t.Context(),
		scope,
		prepareHistory(t, f.h.store, scope, saved),
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	history, err := f.h.store.OwnerHistory(t.Context(), f.scope, uuid.Nil, 20)
	require.NoError(t, err)
	for _, item := range history.Items {
		require.NotEqual(t, "files", item.SourceRole)
	}
	code, body := adminRequest(
		t,
		f.h,
		f.owner,
		http.MethodPost,
		"/internal/agent/history/avatar_candidates",
		map[string]any{
			"protocol_version": 1, "operation_id": "discover-imported-files", "arguments": map[string]any{"limit": 20},
		},
	)
	require.Equal(t, http.StatusOK, code, string(body))
	require.NotContains(t, string(body), "PRIVATE_")
	var agent struct {
		Result db.HistoryAvatarCandidates `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &agent))
	require.Len(t, agent.Result.Items, 20)
	require.True(t, agent.Result.HasMore)
	code, body, headers := appCall(t, f.h, http.MethodGet, "/app/avatars/history", "", f.session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(body))
	require.Contains(t, headers.Get("Cache-Control"), "no-store")
	var native db.HistoryAvatarCandidates
	require.NoError(t, json.Unmarshal(body, &native))
	require.Equal(t, agent.Result, native)
	code, body, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/history?after="+native.NextID.String(),
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(body))
	var second db.HistoryAvatarCandidates
	require.NoError(t, json.Unmarshal(body, &second))
	require.Len(t, second.Items, 2)
	require.False(t, second.HasMore)
	all := append([]db.AvatarHistorySource(nil), native.Items...)
	all = append(all, second.Items...)
	require.Contains(t, all, first)
	require.Len(t, all, 22)
	for _, item := range second.Items {
		require.NotContains(t, native.Items, item)
	}
	code, body = adminRequest(
		t,
		f.h,
		adminToken,
		http.MethodPost,
		"/internal/agent/history/avatar_candidates",
		map[string]any{
			"protocol_version": 1, "operation_id": "admin-no-bypass", "arguments": map[string]any{},
		},
	)
	require.Equal(t, http.StatusOK, code, string(body))
	require.NoError(t, json.Unmarshal(body, &agent))
	require.Empty(t, agent.Result.Items)
	code, body, _ = appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/history?after=bad",
		"",
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(body))
	f.p.mu.Lock()
	gets := f.p.fileGets
	f.p.mu.Unlock()
	require.Zero(t, gets, "discovery never downloads Telegram files")
}

func TestAvatarDraftReferencePreservesUploadsAndOldOperationReplay(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.mu.Unlock()
	taskID := submitFile(t, f.h, 29201, time.Now().UTC().Truncate(time.Second))
	task, err := f.h.store.GetTask(t.Context(), taskID)
	require.NoError(t, err)
	var request struct {
		Attachments []struct {
			ID uuid.UUID `json:"id"`
		} `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &request))
	require.Len(t, request.Attachments, 1)
	fileID := request.Attachments[0].ID
	f.h.stop(t)
	uploadID, telegramID, operation := uuid.New(), uuid.New(), uuid.New()
	digest := sha256.Sum256(f.raw)
	frame, err := hex.DecodeString(avatar.FrameSHA256)
	require.NoError(t, err)
	transform, err := json.Marshal(avatar.IdentityTransform())
	require.NoError(t, err)
	for _, row := range []struct {
		id     uuid.UUID
		source any
	}{{uploadID, nil}, {telegramID, fileID}} {
		var original any = f.raw
		if row.source != nil {
			original = nil
		}
		_, err = f.h.store.Pool().Exec(t.Context(),
			"INSERT INTO shuttle_avatar_drafts(id,user_id,source_file_id,original,sou" +
"rce_sha256,mime_type,frame_sha256,transform,version,source_size_bytes)\n " +
"VALUES($1,$2,$3,$4,$5,'image/png',$6,$7,1,$8)",
			row.id, f.scope.UserID, row.source, original, digest[:], frame, transform, len(f.raw))
		require.NoError(t, err)
	}
	oldReceipt := map[string]any{
		"id":             telegramID,
		"source_file_id": fileID,
		"source_sha256":  hex.EncodeToString(digest[:]),
		"mime_type":      "image/png",
		"size_bytes":     len(f.raw),
		"frame_sha256":   avatar.FrameSHA256,
		"transform":      avatar.IdentityTransform(),
		"version":        1,
		"created_at":     "2026-10-20T12:00:00Z",
		"updated_at":     "2026-10-20T12:00:00Z",
	}
	result, err := json.Marshal(oldReceipt)
	require.NoError(t, err)
	oldHash := sha256.Sum256(
		[]byte(
			`{"kind":"create_file","source_file_id":"` + fileID.String() + `","draft_id":"00000000-0000-0000-0000-000000000000"}`,
		),
	)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_avatar_draft_operations(user_id,operation_id,kind,re" +
"quest_hash,draft_id,result,native) VALUES($1,$2,'create_file',$3,$4,$5,f" +
"alse)", f.scope.UserID, operation, oldHash[:], telegramID, result)
	require.NoError(t, err)
	upload, raw, err := f.h.store.AvatarDraftOriginal(t.Context(), f.scope, uploadID)
	require.NoError(t, err)
	require.Equal(t, f.raw, raw)
	require.EqualValues(t, len(f.raw), upload.SizeBytes)
	reference, raw, err := f.h.store.AvatarDraftOriginal(t.Context(), f.scope, telegramID)
	require.NoError(t, err)
	require.Empty(t, raw)
	require.EqualValues(t, len(f.raw), reference.SizeBytes)
	require.Equal(t, &fileID, reference.SourceFileID)
	f.p.mu.Lock()
	f.p.fileGone = true
	gets := f.p.fileGets
	f.p.mu.Unlock()
	code, replayed := avatarDraftAgent(t, f, "create_draft", operation, map[string]any{"source_file_id": fileID})
	require.Equal(t, http.StatusOK, code, string(replayed))
	require.Contains(t, string(replayed), `"status":"succeeded"`)
	var receipt struct {
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(replayed, &receipt))
	require.JSONEq(t, string(result), string(receipt.Result))
	f.p.mu.Lock()
	require.Equal(t, gets, f.p.fileGets)
	f.p.mu.Unlock()
	_, raw, err = f.h.store.AvatarDraftOriginal(t.Context(), f.scope, uploadID)
	require.NoError(t, err)
	require.Equal(t, f.raw, raw, "old operation replay preserves uploaded originals")
}
