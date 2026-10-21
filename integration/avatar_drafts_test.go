package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/migrations"
)

type avatarDraftFixture struct {
	h       *harness
	p       *peers
	session appSession
	scope   db.ProfileScope
	owner   string
	raw     []byte
}

func avatarDraftPhoto(t *testing.T, large bool) []byte {
	t.Helper()
	side := 40
	if large {
		side = 240
	}
	photo := image.NewNRGBA(image.Rect(0, 0, side, side))
	rng := rand.New(rand.NewPCG(1, 2))
	for y := range side {
		for x := range side {
			v := rng.Uint32()
			photo.SetNRGBA(
				x,
				y,
				color.NRGBA{R: uint8(v & 255), G: uint8((v >> 8) & 255), B: uint8((v >> 16) & 255), A: 255},
			)
		}
	}
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, photo))
	return data.Bytes()
}
func avatarDraftSetup(t *testing.T, processor avatar.Processor) avatarDraftFixture {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.loomDown = true
	p.avatarProcessor = processor
	if processor == nil {
		p.avatarProcessor = avatarExecutable(t)
	}
	h := setup(t, p)
	id, owner := registrationUser(t, h, 456, false)
	session := detailsAppSession(t, h, 456, 211)
	return avatarDraftFixture{h, p, session, ownerProfileScope(t, h, id), owner, avatarDraftPhoto(t, false)}
}
func avatarUploadHTTP(h *harness, session, origin string, operation uuid.UUID, raw []byte) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/app/avatars/drafts/upload", bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Origin", origin)
	req.Header.Set("X-Zinusya-Operation-Id", operation.String())
	req.Header.Set("Authorization", "ZinusyaSession "+session)
	response, err := h.server.Client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	return response.StatusCode, body, err
}
func avatarUpload(t *testing.T, f avatarDraftFixture, operation uuid.UUID, raw []byte) (int, []byte) {
	t.Helper()
	code, body, err := avatarUploadHTTP(f.h, f.session.Token, appTestOrigin, operation, raw)
	require.NoError(t, err)
	return code, body
}
func avatarDraftMetadata(t *testing.T, raw []byte) db.AvatarDraft {
	t.Helper()
	var d db.AvatarDraft
	require.NoError(t, json.Unmarshal(raw, &d))
	require.NotEqual(t, uuid.Nil, d.ID)
	return d
}
func avatarDraftAgent(t *testing.T, f avatarDraftFixture, method string, operation uuid.UUID, args any) (int, []byte) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	if method == "create_draft" || method == "save_draft" || method == "finalize_draft" {
		body["idempotency_key"] = operation
	}
	return adminRequest(t, f.h, f.owner, http.MethodPost, "/internal/agent/avatars/"+method, body)
}
func avatarDraftAgentResult(t *testing.T, raw []byte) db.AvatarDraft {
	t.Helper()
	var result struct {
		Status string         `json:"status"`
		Result db.AvatarDraft `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	return result.Result
}
func avatarDraftSaveBody(t *testing.T, op uuid.UUID, version int64, transform avatar.Transform) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"operation_id": op, "expected_version": version, "transform": transform})
	require.NoError(t, err)
	return string(raw)
}

func TestAvatarDraftNativeAgentHandoffsAndHistoricalReplayWithoutLoom(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	f.raw = avatarDraftPhoto(t, true)
	require.Greater(t, len(f.raw), 64<<10)
	operation := uuid.New()
	code, created := avatarUpload(t, f, operation, f.raw)
	require.Equal(t, http.StatusOK, code, string(created))
	d := avatarDraftMetadata(t, created)
	require.Equal(t, int64(1), d.Version)
	edited := avatar.Transform{ZoomPPM: 2 * avatar.FramePPM, AngleMilli: avatar.HalfTurnMilli, OffsetXPPM: 100}
	save := uuid.New()
	args := map[string]any{"draft_id": d.ID, "expected_version": 1, "transform": edited}
	code, raw := avatarDraftAgent(t, f, "save_draft", save, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	saved := avatarDraftAgentResult(t, raw)
	require.Equal(t, int64(2), saved.Version)
	require.Equal(t, -avatar.HalfTurnMilli, saved.Transform.AngleMilli)
	code, raw, _ = appCall(t, f.h, http.MethodGet, "/app/avatars/drafts/"+d.ID.String(), "", f.session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, saved, avatarDraftMetadata(t, raw))
	nativeSave := uuid.New()
	body := avatarDraftSaveBody(t, nativeSave, 2, avatar.IdentityTransform())
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts/"+d.ID.String(),
		body,
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(3), avatarDraftMetadata(t, raw).Version)
	f.h.stop(t)
	f.h.start(t)
	code, replayed := avatarUpload(t, f, operation, f.raw)
	require.Equal(t, http.StatusOK, code, string(replayed))
	require.JSONEq(t, string(created), string(replayed))
	code, raw = avatarDraftAgent(t, f, "save_draft", save, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, saved, avatarDraftAgentResult(t, raw))
	code, raw = avatarDraftAgent(t, f, "draft", uuid.New(), map[string]any{"draft_id": d.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, int64(3), avatarDraftAgentResult(t, raw).Version)
	changed := append(append([]byte{}, f.raw...), 1)
	code, raw = avatarUpload(t, f, operation, changed)
	require.Equal(t, http.StatusConflict, code, string(raw))
	var count int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&count))
	require.Zero(t, count, "Draft work does not create fake agent tasks")
	f.p.mu.Lock()
	runs, calls := len(f.p.runs), f.p.loomUnavailableCalls
	f.p.mu.Unlock()
	require.Zero(t, runs)
	require.Zero(t, calls)
}

func TestAvatarDraftImportPinsDigestAndReplaysAfterTelegramLoss(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.mu.Unlock()
	taskID := submitFile(t, f.h, 29101, time.Now().UTC().Truncate(time.Second))
	task, err := f.h.store.GetTask(t.Context(), taskID)
	require.NoError(t, err)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	operation := uuid.New()
	args := map[string]any{"source_file_id": intent.Attachments[0].ID}
	code, raw := avatarDraftAgent(t, f, "create_draft", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	d := avatarDraftAgentResult(t, raw)
	f.p.mu.Lock()
	f.p.fileGone = true
	gets := f.p.fileGets
	f.p.mu.Unlock()
	code, raw = avatarDraftAgent(t, f, "create_draft", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, d, avatarDraftAgentResult(t, raw))
	req, err := http.NewRequest(http.MethodGet, f.h.server.URL+"/app/avatars/drafts/"+d.ID.String()+"/original", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "ZinusyaSession "+f.session.Token)
	response, err := f.h.server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	original, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
	require.Contains(t, string(original), "send it again")
	require.Equal(t, "private, no-store", response.Header.Get("Cache-Control"))
	f.p.mu.Lock()
	require.Equal(t, gets+1, f.p.fileGets)
	f.p.mu.Unlock()
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts/"+d.ID.String(),
		avatarDraftSaveBody(t, uuid.New(), 1, avatar.Transform{ZoomPPM: avatar.FramePPM, OffsetYPPM: 20}),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw = avatarDraftAgent(t, f, "fetch_original", uuid.New(), map[string]any{"draft_id": d.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "/internal/avatar-drafts/"+d.ID.String()+"/original")
	require.NotContains(t, string(raw), "private-telegram-file")
}

func TestAvatarDraftOwnerIsolationStaleWritesAndNativeGuards(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	operation := uuid.New()
	code, raw := avatarUpload(t, f, operation, f.raw)
	require.Equal(t, http.StatusOK, code, string(raw))
	d := avatarDraftMetadata(t, raw)
	otherID, other := registrationUser(t, f.h, 99123, false)
	otherScope := ownerProfileScope(t, f.h, otherID)
	_, err := f.h.store.GetAvatarDraft(t.Context(), otherScope, d.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, _, err = f.h.store.AvatarDraftOriginal(t.Context(), otherScope, d.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.h.store.AvatarDraftOperation(t.Context(), otherScope, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	code, raw = adminRequest(
		t,
		f.h,
		other,
		http.MethodPost,
		"/internal/agent/avatars/draft",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"arguments":        map[string]any{"draft_id": d.ID},
		},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw, err = avatarUploadHTTP(f.h, f.session.Token, "https://foreign.invalid", uuid.New(), f.raw)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	body := avatarDraftSaveBody(t, uuid.New(), 1, avatar.IdentityTransform())
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts/"+d.ID.String(),
		body,
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts/"+d.ID.String(),
		avatarDraftSaveBody(t, uuid.New(), 1, avatar.IdentityTransform()),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts/"+d.ID.String(),
		avatarDraftSaveBody(t, uuid.New(), 2, avatar.IdentityTransform())+strings.Repeat(" ", 65<<10),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, raw = avatarUpload(t, f, uuid.New(), make([]byte, avatar.MaxInputBytes+1))
	require.Equal(t, http.StatusRequestEntityTooLarge, code, string(raw))
	code, raw = avatarUpload(t, f, uuid.New(), []byte("not an image"))
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	current, readErr := f.h.store.GetAvatarDraft(t.Context(), f.scope, d.ID)
	require.NoError(t, readErr)
	require.Equal(t, int64(2), current.Version, "Rejected over-limit JSON must not update the draft")
	code, _, _ = appCall(t, f.h, http.MethodPost, "/app/session/logout", "{}", f.session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code)
	code, raw = avatarUpload(t, f, operation, f.raw)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}

func TestAvatarDraftConcurrentSavesAndCreatesRespectVersionsAndCount(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	proof, err := renderer.Render(t.Context(), f.raw, avatar.IdentityTransform())
	require.NoError(t, err)
	digest := sha256.Sum256(f.raw)
	uploadHash := hex.EncodeToString(digest[:])
	type answer struct {
		d   db.AvatarDraft
		err error
	}
	results := make(chan answer, 6)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			d, writeErr := f.h.store.CreateAvatarDraft(
				t.Context(),
				f.scope,
				db.AvatarDraftCreate{OperationID: uuid.New(), UploadSHA256: uploadHash},
				f.raw,
				proof,
			)
			results <- answer{d, writeErr}
		})
	}
	wg.Wait()
	close(results)
	var d db.AvatarDraft
	var accepted, rejected int
	for result := range results {
		if result.err == nil {
			accepted++
			d = result.d
		} else {
			require.ErrorIs(t, result.err, db.ErrAvatarDraftQuota)
			rejected++
		}
	}
	require.Equal(t, 5, accepted)
	require.Equal(t, 1, rejected)
	saves := make(chan error, 2)
	for offset := range int64(2) {
		wg.Go(func() {
			_, saveErr := f.h.store.SaveAvatarDraft(
				t.Context(),
				f.scope,
				db.AvatarDraftSave{
					OperationID:     uuid.New(),
					DraftID:         d.ID,
					ExpectedVersion: 1,
					Transform:       avatar.Transform{ZoomPPM: avatar.FramePPM, OffsetXPPM: offset},
				},
			)
			saves <- saveErr
		})
	}
	wg.Wait()
	close(saves)
	accepted, rejected = 0, 0
	for saveErr := range saves {
		if saveErr == nil {
			accepted++
		} else {
			require.ErrorIs(t, saveErr, db.ErrConflict)
			rejected++
		}
	}
	require.Equal(t, 1, accepted)
	require.Equal(t, 1, rejected)
}

func TestAvatarDraftByteQuotaAndFailedValidationLeaveNoPartialReceipt(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	raw := append(append([]byte{}, f.raw...), make([]byte, (13<<20)-len(f.raw))...)
	proof, err := renderer.Render(t.Context(), raw, avatar.IdentityTransform())
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	for range 3 {
		_, err = f.h.store.CreateAvatarDraft(
			t.Context(),
			f.scope,
			db.AvatarDraftCreate{OperationID: uuid.New(), UploadSHA256: hex.EncodeToString(digest[:])},
			raw,
			proof,
		)
		require.NoError(t, err)
	}
	operation := uuid.New()
	_, err = f.h.store.CreateAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftCreate{OperationID: operation, UploadSHA256: hex.EncodeToString(digest[:])},
		raw,
		proof,
	)
	require.ErrorIs(t, err, db.ErrAvatarDraftQuota)
	_, err = f.h.store.AvatarDraftOperation(t.Context(), f.scope, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	proof.SourceSHA256 = "wrong"
	operation = uuid.New()
	_, err = f.h.store.CreateAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftCreate{OperationID: operation, UploadSHA256: hex.EncodeToString(digest[:])},
		raw,
		proof,
	)
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	_, err = f.h.store.AvatarDraftOperation(t.Context(), f.scope, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestAvatarDraftSessionExpiryDuringProcessingPreventsCommit(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	worker := avatarExecutable(t)
	processor := avatarProcessorFunc(
		func(ctx context.Context, raw []byte, transform avatar.Transform) (avatar.Rendered, error) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return avatar.Rendered{}, ctx.Err()
			}
			return worker.Render(ctx, raw, transform)
		},
	)
	f := avatarDraftSetup(t, processor)
	type reply struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan reply, 1)
	go func() {
		code, raw, err := avatarUploadHTTP(f.h, f.session.Token, appTestOrigin, uuid.New(), f.raw)
		done <- reply{code, raw, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Renderer was not reached")
	}
	_, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1", f.session.UserID)
	require.NoError(t, err)
	close(release)
	result := <-done
	require.NoError(t, result.err)
	require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
	for _, table := range []string{"shuttle_avatar_drafts", "shuttle_avatar_draft_operations"} {
		var count int
		require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
}

func TestAvatarDraftNativeImportSharesAgentReceiptAndManifestSchemas(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.mu.Unlock()
	taskID := submitFile(t, f.h, 29102, time.Now().UTC().Truncate(time.Second))
	task, err := f.h.store.GetTask(t.Context(), taskID)
	require.NoError(t, err)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	operation := uuid.New()
	body, err := json.Marshal(map[string]any{"operation_id": operation, "source_file_id": intent.Attachments[0].ID})
	require.NoError(t, err)
	code, raw, _ := appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts",
		string(body),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	original := avatarDraftMetadata(t, raw)
	code, raw = avatarDraftAgent(
		t,
		f,
		"create_draft",
		operation,
		map[string]any{"source_file_id": intent.Attachments[0].ID},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, original, avatarDraftAgentResult(t, raw))
	schemas := avatarOutputSchemas(t, avatarFixture{h: f.h, owner: f.owner})
	validateAvatarOutput(t, schemas["create_draft"], raw)
	code, raw = avatarDraftAgent(t, f, "drafts", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["drafts"], raw)
	require.Contains(t, string(raw), original.ID.String())
	code, raw = avatarDraftAgent(t, f, "draft_operation", uuid.New(), map[string]any{"operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["draft_operation"], raw)
	require.Equal(t, original, avatarDraftAgentResult(t, raw))
	actions, err := f.h.store.RecentNativeAvatarDraftActions(t.Context(), f.scope, taskID)
	require.NoError(t, err)
	require.Empty(t, actions, "A task cannot see later native actions as its past context")
	later := submitFile(t, f.h, 29103, time.Now().UTC().Truncate(time.Second))
	actions, err = f.h.store.RecentNativeAvatarDraftActions(t.Context(), f.scope, later)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, operation, actions[0].OperationID)
}

func TestAvatarDraftMigrationAllowsEmptyRollbackAndProtectsOriginals(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	raw, err := migrations.Files.ReadFile("00055_avatar_drafts.sql")
	require.NoError(t, err)
	up, down, ok := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, ok)
	extensionUp, extensionDown := avatarFinalizationMigration(t)
	sourceUp, sourceDown := avatarDraftSourceMigration(t)
	up = up + "\n" + extensionUp + "\n" + sourceUp
	down = sourceDown + "\n" + extensionDown + "\n" + down
	_, err = f.h.store.Pool().Exec(t.Context(), down)
	require.NoError(t, err)
	var gone bool
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(t.Context(), "SELECT to_regclass('shuttle_avatar_drafts') IS NULL").Scan(&gone),
	)
	require.True(t, gone)
	_, err = f.h.store.Pool().Exec(t.Context(), up)
	require.NoError(t, err)
	operation := uuid.New()
	code, created := avatarUpload(t, f, operation, f.raw)
	require.Equal(t, http.StatusOK, code, string(created))
	draft := avatarDraftMetadata(t, created)
	_, err = f.h.store.Pool().Exec(t.Context(), down)
	require.ErrorContains(t, err, "export avatar originals and operation history before rollback")
	current, original, err := f.h.store.AvatarDraftOriginal(t.Context(), f.scope, draft.ID)
	require.NoError(t, err)
	require.Equal(t, draft, current)
	require.Equal(t, f.raw, original)
	replay, err := f.h.store.AvatarDraftOperation(t.Context(), f.scope, operation)
	require.NoError(t, err)
	require.Equal(t, draft, replay)
}

func TestAvatarDraftRollbackRejectsUncommittedOriginal(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	migration, err := migrations.Files.ReadFile("00055_avatar_drafts.sql")
	require.NoError(t, err)
	_, down, ok := strings.Cut(string(migration), "-- +goose Down")
	require.True(t, ok)
	writer, err := f.h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(t.Context()) }()
	id := uuid.New()
	digest := sha256.Sum256(f.raw)
	frame, err := hex.DecodeString(avatar.FrameSHA256)
	require.NoError(t, err)
	transform, err := json.Marshal(avatar.IdentityTransform())
	require.NoError(t, err)
	_, err = writer.Exec(
		t.Context(),
		`INSERT INTO shuttle_avatar_drafts(id,user_id,original,source_sha256,mime_type,frame_sha256,transform,version,source_size_bytes) VALUES($1,$2,$3,$4,'image/png',$5,$6,1,octet_length($3::bytea))`,
		id,
		f.scope.UserID,
		f.raw,
		digest[:],
		frame,
		transform,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = f.h.store.Pool().Exec(ctx, down)
	var lockError *pgconn.PgError
	require.ErrorAs(t, err, &lockError)
	require.Equal(t, "55P03", lockError.Code, "Rollback must refuse active writers before any destructive statement")
	require.NoError(t, writer.Commit(t.Context()))
	d, original, err := f.h.store.AvatarDraftOriginal(t.Context(), f.scope, id)
	require.NoError(t, err)
	require.Equal(t, id, d.ID)
	require.Equal(t, f.raw, original)
}

func TestAvatarDraftNativeImportPreservesOversizeRejectionBeforeTelegramIO(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := sizedFileID(t, f.h, 29104, files.MaxReadBytes+1)
	operation := uuid.New()
	body, err := json.Marshal(map[string]any{"operation_id": operation, "source_file_id": source})
	require.NoError(t, err)
	code, raw, _ := appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/avatars/drafts",
		string(body),
		f.session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusRequestEntityTooLarge, code, string(raw))
	agentOperation := uuid.New()
	code, raw = avatarDraftAgent(t, f, "create_draft", agentOperation, map[string]any{"source_file_id": source})
	require.Equal(t, http.StatusRequestEntityTooLarge, code, string(raw))
	_, agentErr := f.h.store.AvatarDraftOperation(t.Context(), f.scope, agentOperation)
	require.ErrorIs(t, agentErr, pgx.ErrNoRows)

	f.p.mu.Lock()
	gets := f.p.fileGets
	f.p.mu.Unlock()
	require.Zero(t, gets)
	drafts, err := f.h.store.ListAvatarDrafts(t.Context(), f.scope)
	require.NoError(t, err)
	require.Empty(t, drafts)
	_, err = f.h.store.AvatarDraftOperation(t.Context(), f.scope, operation)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
