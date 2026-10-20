package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
	"github.com/complynx/zns-chatbot/internal/db"
)

func avatarPreviewRead(h *harness, path, token string, internal bool) (int, []byte, http.Header) {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	if internal {
		r.Header.Set("Authorization", "Bearer "+token)
		h.endpoint.InternalHandler().ServeHTTP(w, r)
	} else {
		if token != "" {
			r.Header.Set("Authorization", "ZinusyaSession "+token)
		}
		h.endpoint.PublicHandler().ServeHTTP(w, r)
	}
	return w.Code, w.Body.Bytes(), w.Header()
}
func avatarPreviewURL(id uuid.UUID, version int64) string {
	return "/app/avatars/drafts/" + id.String() + "/preview/" + strconv.FormatInt(version, 10)
}
func createPreviewDraft(t *testing.T, f avatarDraftFixture) db.AvatarDraft {
	t.Helper()
	code, raw := avatarUpload(t, f, uuid.New(), f.raw)
	require.Equal(t, http.StatusOK, code, string(raw))
	return avatarDraftMetadata(t, raw)
}
func previewDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func assertPreviewNoEffects(t *testing.T, f avatarDraftFixture, operations int) {
	t.Helper()
	for _, table := range []string{"shuttle_tasks", "shuttle_avatars", "shuttle_outbox"} {
		var count int
		require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	var count int
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatar_draft_operations").Scan(&count),
	)
	require.Equal(t, operations, count)
}
func TestAvatarPreviewNativeAgentDescriptorAndPinnedFrameAgree(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	d := createPreviewDraft(t, f)
	transform := avatar.Transform{ZoomPPM: 2 * avatar.FramePPM, OffsetXPPM: 100000, AngleMilli: 45000}
	saved, err := f.h.store.SaveAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftSave{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1, Transform: transform},
	)
	require.NoError(t, err)
	code, jpeg, headers := avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 2), f.session.Token, false)
	require.Equal(t, http.StatusOK, code, string(jpeg))
	require.Equal(t, "image/jpeg", headers.Get("Content-Type"))
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Equal(t, "2", headers.Get("X-Zinusya-Draft-Version"))
	require.Contains(t, headers.Get("Content-Disposition"), "attachment")
	expected, err := renderer.Render(t.Context(), f.raw, saved.Transform)
	require.NoError(t, err)
	require.Equal(t, expected.SHA256, previewDigest(jpeg))
	code, raw := avatarDraftAgent(
		t,
		f,
		"preview_draft",
		uuid.New(),
		map[string]any{"draft_id": d.ID, "expected_version": 2},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	schemas := avatarOutputSchemas(t, avatarFixture{h: f.h, owner: f.owner})
	validateAvatarOutput(t, schemas["preview_draft"], raw)
	var descriptor struct {
		Result struct {
			Version   int64            `json:"version"`
			SHA256    string           `json:"sha256"`
			Transform avatar.Transform `json:"transform"`
		} `json:"result"`
		Artifacts []struct {
			DownloadPath string `json:"download_path"`
			SHA256       string `json:"sha256"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(raw, &descriptor))
	require.Equal(t, int64(2), descriptor.Result.Version)
	require.Equal(t, transform, descriptor.Result.Transform)
	require.Equal(t, expected.SHA256, descriptor.Result.SHA256)
	require.Len(t, descriptor.Artifacts, 1)
	code, content, _ := avatarPreviewRead(f.h, descriptor.Artifacts[0].DownloadPath, f.owner, true)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, descriptor.Artifacts[0].SHA256, previewDigest(content))
	code, png, headers := avatarPreviewRead(f.h, "/app/avatars/frame/"+avatar.FrameSHA256, "", false)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "image/png", headers.Get("Content-Type"))
	require.Equal(t, avatar.FrameSHA256, previewDigest(png))
	require.Contains(t, headers.Get("Cache-Control"), "immutable")
	code, _, _ = avatarPreviewRead(f.h, "/app/avatars/frame/"+uuid.NewString(), "", false)
	require.Equal(t, http.StatusNotFound, code)
	assertPreviewNoEffects(t, f, 2)
}
func TestAvatarPreviewRejectsForeignStaleAndUnavailableFrame(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	d := createPreviewDraft(t, f)
	_, other := registrationUser(t, f.h, 99231, false)
	code, raw := adminRequest(
		t,
		f.h,
		other,
		http.MethodPost,
		"/internal/agent/avatars/preview_draft",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"arguments":        map[string]any{"draft_id": d.ID, "expected_version": 1},
		},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	foreign := detailsAppSession(t, f.h, 99231, 212)
	code, _, _ = avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 1), foreign.Token, false)
	require.Equal(t, http.StatusNotFound, code)
	_, err := f.h.store.SaveAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftSave{
			OperationID:     uuid.New(),
			DraftID:         d.ID,
			ExpectedVersion: 1,
			Transform:       avatar.IdentityTransform(),
		},
	)
	require.NoError(t, err)
	code, _, _ = avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 1), f.session.Token, false)
	require.Equal(t, http.StatusConflict, code)
	code, _, _ = avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 0), f.session.Token, false)
	require.Equal(t, http.StatusBadRequest, code)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_avatar_drafts SET frame_sha256=$2 WHERE id=$1", d.ID, bytes.Repeat([]byte{1}, sha256.Size))
	require.NoError(t, err)
	code, _, _ = avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 2), f.session.Token, false)
	require.Equal(t, http.StatusConflict, code, "A new frame must never silently replace the pinned one")
	assertPreviewNoEffects(t, f, 2)
}
func TestAvatarPreviewRechecksVersionAndSessionAfterRendering(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"edit", "expire"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			var armed atomic.Bool
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			processor := avatarProcessorFunc(
				func(ctx context.Context, raw []byte, transform avatar.Transform) (avatar.Rendered, error) {
					if armed.Swap(false) {
						close(entered)
						select {
						case <-release:
						case <-ctx.Done():
							return avatar.Rendered{}, ctx.Err()
						}
					}
					return renderer.Render(ctx, raw, transform)
				},
			)
			f := avatarDraftSetup(t, processor)
			d := createPreviewDraft(t, f)
			armed.Store(true)
			result := make(chan int, 1)
			go func() {
				code, _, _ := avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 1), f.session.Token, false)
				result <- code
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("Preview renderer not reached")
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
						Transform:       avatar.Transform{ZoomPPM: avatar.FramePPM, OffsetXPPM: 20},
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
			case code := <-result:
				require.Equal(t, expected, code)
			case <-time.After(5 * time.Second):
				t.Fatal("Preview did not complete")
			}
		})
	}
}
func TestAvatarPreviewRejectsMismatchedWorkerProvenance(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"transform", "source"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			var armed atomic.Bool
			processor := avatarProcessorFunc(
				func(ctx context.Context, raw []byte, transform avatar.Transform) (avatar.Rendered, error) {
					if armed.Load() && fault == "transform" {
						transform = avatar.IdentityTransform()
					}
					r, err := renderer.Render(ctx, raw, transform)
					if armed.Load() && fault == "source" {
						r.SourceSHA256 = "wrong"
					}
					return r, err
				},
			)
			f := avatarDraftSetup(t, processor)
			d := createPreviewDraft(t, f)
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
			armed.Store(true)
			code, _, _ := avatarPreviewRead(f.h, avatarPreviewURL(d.ID, 2), f.session.Token, false)
			require.Equal(t, http.StatusServiceUnavailable, code)
			assertPreviewNoEffects(t, f, 2)
		})
	}
}
