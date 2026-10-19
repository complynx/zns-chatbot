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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
)

type avatarProcessorFunc func(context.Context, []byte, avatar.Transform) (avatar.Rendered, error)

func (f avatarProcessorFunc) Render(
	ctx context.Context,
	raw []byte,
	transform avatar.Transform,
) (avatar.Rendered, error) {
	return f(ctx, raw, transform)
}

type avatarFixture struct {
	rendered avatar.Rendered
	h        *harness
	p        *peers
	ownerID  uuid.UUID
	owner    string
	taskID   uuid.UUID
	runID    string
	source   uuid.UUID
}

func setupAvatar(t *testing.T, processor avatar.Processor) avatarFixture {
	t.Helper()
	p := newPeers(t)
	p.steering = true
	p.avatarProcessor = processor
	photo := image.NewRGBA(image.Rect(0, 0, 40, 60))
	for y := range 60 {
		for x := range 40 {
			photo.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 3), B: 90, A: 255})
		}
	}
	var source bytes.Buffer
	require.NoError(t, png.Encode(&source, photo))
	p.fileBytes = source.Bytes()
	rendered, renderErr := renderer.Render(context.Background(), p.fileBytes, avatar.IdentityTransform())
	require.NoError(t, renderErr)
	if processor == nil {
		processor = cachedAvatarProcessor(rendered)
	}
	p.avatarProcessor = processor
	h := setup(t, p)
	ownerID, owner := registrationUser(t, h, 456, false)
	id := submitFile(t, h, 19101, time.Now().UTC().Truncate(time.Second))
	var task db.Task
	require.Eventually(t, func() bool {
		var err error
		task, err = h.store.GetTask(context.Background(), id)
		return err == nil && task.State == "running" && task.RunID != ""
	}, 15*time.Second, 25*time.Millisecond)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	require.NotEmpty(t, task.RunID)
	return avatarFixture{rendered: rendered, h: h, p: p, ownerID: ownerID, owner: owner, taskID: id, runID: task.RunID,
		source: intent.Attachments[0].ID}
}

func cachedAvatarProcessor(result avatar.Rendered) avatar.Processor {
	return avatarProcessorFunc(
		func(context.Context, []byte, avatar.Transform) (avatar.Rendered, error) { return result, nil },
	)
}

func avatarRequest(t *testing.T, f avatarFixture, token, method string, operation uuid.UUID, args any) (int, []byte) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	if method == "generate" {
		body["run_id"], body["idempotency_key"] = f.runID, operation
		body["confirmation"] = map[string]any{"mode": "none"}
	}
	return adminRequest(t, f.h, token, http.MethodPost, "/internal/agent/avatars/"+method, body)
}

func generatedAvatar(t *testing.T, raw []byte) db.AvatarReceipt {
	t.Helper()
	var result struct {
		Status string           `json:"status"`
		Result db.AvatarReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	require.NotEqual(t, uuid.Nil, result.Result.ArtifactID)
	return result.Result
}

func avatarView(t *testing.T, f avatarFixture, receipt db.AvatarReceipt) db.AvatarView {
	t.Helper()
	code, raw := avatarRequest(t, f, f.owner, "get", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var result struct {
		Result db.AvatarView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, receipt, result.Result.Avatar)
	return result.Result
}

func awaitAvatarDelivery(t *testing.T, f avatarFixture, receipt db.AvatarReceipt, state string) {
	t.Helper()
	require.Eventually(t, func() bool {
		view, err := f.h.store.AvatarStatus(
			context.Background(),
			ownerProfileScope(t, f.h, f.ownerID),
			receipt.ArtifactID,
		)
		return err == nil && view.DeliveryState == state
	}, 10*time.Second, 25*time.Millisecond)
	require.Equal(t, state, avatarView(t, f, receipt).DeliveryState)
}

func avatarOutputSchemas(t *testing.T, f avatarFixture) map[string]*huma.Schema {
	t.Helper()
	code, raw := adminRequest(t, f.h, f.owner, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID     string      `json:"id"`
				Output huma.Schema `json:"output_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	schemas := make(map[string]*huma.Schema)
	for _, module := range manifest.Modules {
		if module.ID == "zinusya_avatars" {
			for _, method := range module.Methods {
				schema := method.Output
				schema.PrecomputeMessages()
				schemas[method.ID] = &schema
			}
		}
	}
	for _, method := range []string{"generate", "get", "fetch", "operation", "create_draft", "save_draft", "draft", "drafts", "draft_operation", "fetch_original"} {
		require.Contains(t, schemas, method)
	}
	return schemas
}

func validateAvatarOutput(t *testing.T, schema *huma.Schema, raw []byte) {
	t.Helper()
	var envelope struct {
		Result any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.NotNil(t, schema)
	validation := &huma.ValidateResult{}
	huma.Validate(huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer), schema,
		huma.NewPathBuffer(nil, 0), huma.ModeReadFromServer, envelope.Result, validation)
	require.Empty(t, validation.Errors, "Avatar receipts must match the contract consumed by Loom")
}

func TestAvatarExplicitGenerationDeliversPrivateFramedDocumentAndSurvivesSourceLoss(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	schemas := avatarOutputSchemas(t, f)
	f.p.mu.Lock()
	documentsBefore, getsBefore := len(f.p.documentSent), f.p.fileGets
	f.p.mu.Unlock()
	require.Zero(t, documentsBefore, "Receiving an unrelated document must not generate an avatar")
	require.Zero(t, getsBefore, "Only the explicit tool call may download the source")
	op := uuid.New()
	args := map[string]any{"source_file_id": f.source}
	code, raw := avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["generate"], raw)
	receipt := generatedAvatar(t, raw)
	require.Equal(t, f.taskID, receipt.TaskID)
	require.Equal(t, f.source, receipt.SourceFileID)
	require.Equal(t, avatar.FrameSHA256, receipt.FrameSHA256)
	require.Equal(t, avatar.ProcessorVersion, receipt.ProcessorVersion)
	require.Equal(t, avatar.OutputSize, receipt.Width)
	require.Equal(t, avatar.OutputSize, receipt.Height)
	awaitAvatarDelivery(t, f, receipt, "delivered")
	f.p.mu.Lock()
	run := f.p.runs[f.taskID.String()]
	run.Status, run.Result = "completed", "Your framed avatar is ready."
	run.Version++
	f.p.runs[f.taskID.String()] = run
	f.p.steering = false
	f.p.mu.Unlock()
	f.h.await(t, f.taskID, "completed")
	f.h.awaitReplies(t, 1)
	require.Eventually(t, func() bool {
		rows, readErr := f.h.store.ListOutbox(context.Background(), f.taskID)
		return readErr == nil && len(rows) == 2 && rows[1].State == "delivered"
	}, 10*time.Second, 25*time.Millisecond)
	outbox, err := f.h.store.ListOutbox(context.Background(), f.taskID)
	require.NoError(t, err)
	require.Len(t, outbox, 2, "The avatar and normal agent reply share one durable ordered outbox")
	require.Equal(t, receipt.DeliveryID, outbox[0].ID)
	require.Less(t, outbox[0].ChunkSeq, outbox[1].ChunkSeq)
	require.Equal(t, "delivered", outbox[1].State)
	f.h.stop(t)
	f.p.mu.Lock()
	documentCount := len(f.p.documentSent)
	document := f.p.documentSent[0]
	gets := f.p.fileGets
	f.p.fileGone = true
	f.p.mu.Unlock()
	require.Equal(t, 1, documentCount)
	require.Equal(t, int64(456), document.ChatID)
	require.Equal(t, "zinusya-avatar.jpg", document.Filename)
	config, format, err := image.DecodeConfig(bytes.NewReader(document.Content))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, avatar.OutputSize, config.Width)
	require.Equal(t, avatar.OutputSize, config.Height)
	hash := sha256.Sum256(document.Content)
	require.Equal(t, receipt.SHA256, hex.EncodeToString(hash[:]))
	require.Equal(t, int64(len(document.Content)), receipt.SizeBytes)
	f.h.start(t)
	code, raw = avatarRequest(t, f, f.owner, "generate", uuid.New(), args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"rejected"`, "A closed run cannot create a new avatar")
	code, raw = avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, receipt, generatedAvatar(t, raw))
	code, raw = avatarRequest(t, f, f.owner, "operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["operation"], raw)
	require.Equal(t, receipt, generatedAvatar(t, raw))
	code, raw = avatarRequest(t, f, f.owner, "get", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["get"], raw)
	code, raw = avatarRequest(t, f, f.owner, "fetch", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	validateAvatarOutput(t, schemas["fetch"], raw)
	var fetched struct {
		Artifacts []struct {
			Path string `json:"download_path"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(raw, &fetched))
	require.Len(t, fetched.Artifacts, 1)
	server := httptest.NewServer(f.h.endpoint.InternalHandler())
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+fetched.Artifacts[0].Path, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+f.owner)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "private, no-store", response.Header.Get("Cache-Control"))
	require.Equal(t, document.Content, content)
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Equal(t, gets, f.p.fileGets, "A committed replay must not need the unavailable Telegram source")
	require.Len(t, f.p.documentSent, 1)
}

func TestAvatarWorkerFailureCreatesNoReceiptOrDeliveryAndCanBeRetried(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, avatarProcessorFunc(func(context.Context, []byte, avatar.Transform) (avatar.Rendered, error) {
		return avatar.Rendered{}, avatar.ErrProcessorUnavailable
	}))
	op := uuid.New()
	code, raw := avatarRequest(t, f, f.owner, "generate", op, map[string]any{"source_file_id": f.source})
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	code, raw = avatarRequest(t, f, f.owner, "operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"not_found"`)
	f.h.stop(t)
	f.p.mu.Lock()
	documentCount := len(f.p.documentSent)
	f.p.avatarProcessor = cachedAvatarProcessor(f.rendered)
	f.p.mu.Unlock()
	require.Zero(t, documentCount)
	f.h.start(t)
	code, raw = avatarRequest(t, f, f.owner, "generate", op, map[string]any{"source_file_id": f.source})
	require.Equal(t, http.StatusOK, code, string(raw))
	awaitAvatarDelivery(t, f, generatedAvatar(t, raw), "delivered")
}

func TestAvatarSourceRunAndReceiptRemainOwnerScoped(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	_, other := registrationUser(t, f.h, 19102, true)
	op := uuid.New()
	args := map[string]any{"source_file_id": f.source}
	code, raw := avatarRequest(t, f, other, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"rejected"`)
	wrongRun := f
	wrongRun.runID = "another-users-run"
	code, raw = avatarRequest(t, wrongRun, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"rejected"`)
	code, raw = avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := generatedAvatar(t, raw)
	awaitAvatarDelivery(t, f, receipt, "delivered")
	code, raw = avatarRequest(t, f, other, "operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"not_found"`)
	require.NotContains(t, string(raw), receipt.ArtifactID.String())
	code, _ = avatarRequest(t, f, other, "get", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusNotFound, code)
	code, _ = avatarRequest(t, f, other, "fetch", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusNotFound, code)
	code, raw = avatarRequest(t, wrongRun, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"rejected"`, "An operation cannot be reused with another run")
	_, err := f.h.store.SetUserRevoked(context.Background(), f.ownerID, "fixture")
	require.NoError(t, err)
	code, _ = avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusForbidden, code)
	code, _ = avatarRequest(t, f, f.owner, "operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusForbidden, code)
}

func TestAvatarLostTelegramReceiptStaysUnknownWithoutDuplicateAfterRestart(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	f.p.mu.Lock()
	f.p.loseDocumentSend = true
	f.p.mu.Unlock()
	op := uuid.New()
	args := map[string]any{"source_file_id": f.source}
	code, raw := avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := generatedAvatar(t, raw)
	awaitAvatarDelivery(t, f, receipt, "delivery_unknown")
	f.h.stop(t)
	f.h.start(t)
	code, raw = avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, receipt, generatedAvatar(t, raw))
	// Completion after restart must observe the uncertainty and retain the reply suffix.
	f.p.mu.Lock()
	run := f.p.runs[f.taskID.String()]
	run.Status, run.Result = "completed", "The avatar document was prepared."
	run.Version++
	f.p.runs[f.taskID.String()] = run
	f.p.steering = false
	f.p.mu.Unlock()
	f.h.await(t, f.taskID, "delivery_unknown")
	require.Equal(t, "delivery_unknown", avatarView(t, f, receipt).DeliveryState)
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Len(t, f.p.documentSent, 1, "An accepted document with a lost response must not be uploaded again")
}

func TestAvatarToolWaitsForRunBindingBeforeGenerating(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupAvatar(t, nil)
	f.h.stop(t)
	// Model the interval after Loom starts but before its create response is saved.
	_, err := f.h.store.Pool().Exec(ctx,
		`UPDATE shuttle_tasks SET state='creating_run',run_id='' WHERE id=$1`, f.taskID)
	require.NoError(t, err)
	_, err = f.h.store.Pool().Exec(ctx,
		`UPDATE shuttle_conversations SET active_run_id='' WHERE active_task_id=$1`, f.taskID)
	require.NoError(t, err)
	op := uuid.New()
	args := map[string]any{"source_file_id": f.source}
	code, raw := avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	f.p.mu.Lock()
	gets := f.p.fileGets
	f.p.mu.Unlock()
	require.Zero(t, gets, "A pending run binding must not download or render yet")
	_, err = f.h.store.Pool().Exec(ctx,
		`UPDATE shuttle_tasks SET state='running',run_id=$2 WHERE id=$1`, f.taskID, f.runID)
	require.NoError(t, err)
	_, err = f.h.store.Pool().Exec(ctx,
		`UPDATE shuttle_conversations SET active_run_id=$2 WHERE active_task_id=$1`, f.taskID, f.runID)
	require.NoError(t, err)
	code, raw = avatarRequest(t, f, f.owner, "generate", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := generatedAvatar(t, raw)
	f.h.start(t)
	awaitAvatarDelivery(t, f, receipt, "delivered")
}
