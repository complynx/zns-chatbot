package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func submitNativePhoto(t *testing.T, h *harness, updateID int64) uuid.UUID {
	t.Helper()
	return submitNativeImage(t, h, updateID, false)
}

func submitNativeImage(t *testing.T, h *harness, updateID int64, document bool) uuid.UUID {
	t.Helper()
	message := &telegram.Message{
		ID: updateID, Date: time.Now().Unix(), From: &telegram.User{ID: 456, LanguageCode: "ru"},
		Chat:  telegram.Chat{ID: 456, Type: "private"},
		Photo: []telegram.MediaFile{{FileID: "private-native-photo", UniqueID: "private-photo-unique"}},
	}
	if document {
		message.Document = &telegram.MediaFile{FileID: "private-native-document", UniqueID: "private-document-unique",
			MIMEType: "image/png", Filename: "portrait.png"}
		message.Photo = nil
	}
	body, err := json.Marshal(telegram.Update{ID: updateID, Message: message})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/telegram/webhook", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	response, err := h.server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	var accepted struct {
		TaskID uuid.UUID `json:"task_id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&accepted))
	return accepted.TaskID
}

func TestNativeImageDocumentFramesAndDeliversWithoutLoom(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.miniAppOrigin = appTestOrigin
	p.avatarProcessor = avatarExecutable(t)
	p.fileBytes = avatarDraftPhoto(t, false)
	h := setup(t, p)
	ownerID, _ := registrationUser(t, h, 456, false)
	id := submitNativeImage(t, h, 29311, true)
	require.Equal(t, id, submitNativeImage(t, h, 29311, true))
	h.await(t, id, "completed")
	task, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "structured", task.Route)
	require.Empty(t, task.RunID)
	require.Empty(t, task.AuthSessionID)
	var snapshot struct {
		Avatar db.AvatarDraftReceipt `json:"native_avatar"`
	}
	require.NoError(t, json.Unmarshal(task.Snapshot, &snapshot))
	f := avatarDraftFixture{h: h, p: p, scope: ownerProfileScope(t, h, ownerID)}
	notice := awaitDraftAvatarDelivery(t, f, snapshot.Avatar.DeliveryID, "delivered")
	var delivered struct {
		CropButtonOffered bool `json:"crop_button_offered"`
	}
	require.NoError(t, json.Unmarshal(notice.Receipt, &delivered))
	require.True(t, delivered.CropButtonOffered)
	require.Equal(t, previewDigest(p.fileBytes), snapshot.Avatar.SourceSHA256)
	p.mu.Lock()
	documents := append([]telegram.SendDocumentRequest(nil), p.documentSent...)
	creates, unavailable := len(p.createKeys), p.loomUnavailableCalls
	p.mu.Unlock()
	require.Zero(t, creates)
	require.Zero(t, unavailable)
	require.Len(t, documents, 1)
	require.Equal(t, snapshot.Avatar.SHA256, previewDigest(documents[0].Content))
	require.NotNil(t, documents[0].ReplyMarkup)
	button := documents[0].ReplyMarkup.InlineKeyboard[0][0]
	require.Equal(t, "Кадрировать", button.Text)
	require.Equal(t, appTestOrigin+"/app/#avatar-crop?draft="+snapshot.Avatar.DraftID.String(), button.WebApp.URL)
}

func TestNativePhotoFramesDeliversAndRecordsCropWithoutLoom(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.miniAppOrigin = appTestOrigin
	p.avatarProcessor = avatarExecutable(t)
	photo, _, err := image.Decode(bytes.NewReader(avatarDraftPhoto(t, false)))
	require.NoError(t, err)
	var source bytes.Buffer
	require.NoError(t, jpeg.Encode(&source, photo, nil))
	p.fileBytes = source.Bytes()
	h := setup(t, p)
	ownerID, _ := registrationUser(t, h, 456, false)
	_, foreignToken := registrationUser(t, h, 89220, false)
	id := submitNativePhoto(t, h, 29301)
	require.Equal(t, id, submitNativePhoto(t, h, 29301))
	h.await(t, id, "completed")
	task, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "structured", task.Route)
	require.Empty(t, task.RunID)
	require.Empty(t, task.AuthSessionID)
	var snapshot struct {
		Avatar db.AvatarDraftReceipt `json:"native_avatar"`
	}
	require.NoError(t, json.Unmarshal(task.Snapshot, &snapshot))
	receipt := snapshot.Avatar
	require.NotEqual(t, uuid.Nil, receipt.ArtifactID)
	require.Equal(t, avatar.FrameSHA256, receipt.FrameSHA256)
	require.Equal(t, previewDigest(source.Bytes()), receipt.SourceSHA256)
	scope := ownerProfileScope(t, h, ownerID)
	f := avatarDraftFixture{h: h, p: p, scope: scope}
	notice := awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivered")
	var delivered struct {
		CropButtonOffered bool `json:"crop_button_offered"`
	}
	require.NoError(t, json.Unmarshal(notice.Receipt, &delivered))
	require.True(t, delivered.CropButtonOffered)
	p.mu.Lock()
	documents := append([]telegram.SendDocumentRequest(nil), p.documentSent...)
	creates, unavailable := len(p.createKeys), p.loomUnavailableCalls
	p.mu.Unlock()
	require.Zero(t, creates)
	require.Zero(t, unavailable)
	require.Len(t, documents, 1)
	require.Equal(t, int64(456), documents[0].ChatID)
	require.Equal(t, receipt.SHA256, previewDigest(documents[0].Content))
	config, format, err := image.DecodeConfig(bytes.NewReader(documents[0].Content))
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.Equal(t, avatar.OutputSize, config.Width)
	require.Equal(t, avatar.OutputSize, config.Height)
	require.NotNil(t, documents[0].ReplyMarkup)
	button := documents[0].ReplyMarkup.InlineKeyboard[0][0]
	require.Equal(t, "Кадрировать", button.Text)
	require.Equal(t, appTestOrigin+"/app/#avatar-crop?draft="+receipt.DraftID.String(), button.WebApp.URL)
	session := detailsAppSession(t, h, 456, 231)
	path := "/app/avatars/drafts/" + receipt.DraftID.String() + "/open"
	code, _, _ := appCall(t, h, http.MethodGet, path, "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code)
	var afterGet json.RawMessage
	require.NoError(t, h.store.Pool().QueryRow(t.Context(),
		"SELECT receipt FROM shuttle_avatar_deliveries WHERE id=$1", receipt.DeliveryID).Scan(&afterGet))
	require.JSONEq(t, string(notice.Receipt), string(afterGet), "GET must not record crop entry")
	op := uuid.New()
	body := `{"operation_id":"` + op.String() + `"}`
	code, raw, _ := appCall(t, h, http.MethodPost, path, body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var opened db.AvatarCropOpen
	require.NoError(t, json.Unmarshal(raw, &opened))
	require.Equal(t, receipt.DraftID, opened.DraftID)
	require.False(t, opened.OpenedAt.IsZero())
	code, replay, _ := appCall(t, h, http.MethodPost, path, body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(replay))
	require.JSONEq(t, string(raw), string(replay))
	code, _, _ = appCall(t, h, http.MethodPost, path, body, session.Token, "https://foreign.invalid")
	require.Equal(t, http.StatusForbidden, code)
	foreignSession := detailsAppSession(t, h, 89220, 232)
	code, _, _ = appCall(t, h, http.MethodPost, path, body, foreignSession.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = adminRequest(
		t,
		h,
		foreignToken,
		http.MethodPost,
		"/internal/agent/avatars/draft",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "foreign",
			"arguments":        map[string]any{"draft_id": receipt.DraftID},
		},
	)
	require.Equal(t, http.StatusNotFound, code)
	current, _, err := h.store.GetFinalizedAvatar(t.Context(), scope, receipt.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, receipt, current, "UI entry must not rewrite the artifact receipt")
	for _, table := range []string{
		"shuttle_tasks",
		"shuttle_avatars",
		"shuttle_avatar_drafts",
		"shuttle_avatar_deliveries",
	} {
		var count int
		require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Equal(t, 1, count, table)
	}
	// Read facts for a later request without enqueuing or starting a model run.
	later, _, err := h.store.Intake(t.Context(), db.IntakeRequest{BotID: 123, UpdateID: 29303,
		TelegramUserID: 456, ChatID: 456, Payload: json.RawMessage(`{"kind":"message","text":"what happened?"}`)}, nil)
	require.NoError(t, err)
	actions, err := h.store.RecentNativeAvatarDraftActions(t.Context(), scope, later.ID)
	require.NoError(t, err)
	require.NotEmpty(t, actions)
	require.Equal(t, "crop_open", actions[0].Kind)
	require.NotNil(t, actions[0].CropOpenedAt)
	require.True(t, actions[0].CropButtonOffered)
	require.Equal(t, "delivered", actions[0].DeliveryState)
	p.mu.Lock()
	creates, unavailable, sent := len(p.createKeys), p.loomUnavailableCalls, len(p.documentSent)
	p.mu.Unlock()
	require.Zero(t, creates)
	require.Zero(t, unavailable)
	require.Equal(t, 1, sent)
}

func TestNativePhotoDefersToActiveUserQuestion(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	active := h.submit(t, 29311)
	h.await(t, active, "awaiting_user")
	id := submitNativePhoto(t, h, 29312)
	require.Eventually(t, func() bool {
		task, err := h.store.GetTask(t.Context(), id)
		return err == nil && task.Route == "input" && task.TargetTaskID == active
	}, 10*time.Second, 20*time.Millisecond)
	var count int
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatar_drafts").Scan(&count))
	require.Zero(t, count)
}

func TestNativePhotoTransientSourceReadRetriesSameWorkflow(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.fileBytes = avatarDraftPhoto(t, false)
	p.fileErrorCode = http.StatusServiceUnavailable
	processor := avatarExecutable(t)
	var renders atomic.Int32
	p.avatarProcessor = avatarProcessorFunc(
		func(ctx context.Context, raw []byte, transform avatar.Transform) (avatar.Rendered, error) {
			renders.Add(1)
			return processor.Render(ctx, raw, transform)
		},
	)
	h := setup(t, p)
	_, _ = registrationUser(t, h, 456, false)
	id := submitNativePhoto(t, h, 29331)
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.fileGets > 0
	}, 10*time.Second, 20*time.Millisecond)
	task, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.NotEqual(t, "failed", task.State)
	require.Zero(t, renders.Load())
	p.mu.Lock()
	p.fileErrorCode = 0
	p.mu.Unlock()
	h.await(t, id, "completed")
	require.Equal(t, id, submitNativePhoto(t, h, 29331))
	require.EqualValues(t, 1, renders.Load())
	for _, table := range []string{"shuttle_avatar_drafts", "shuttle_avatars", "shuttle_avatar_deliveries"} {
		var count int
		require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Equal(t, 1, count, table)
	}
}
