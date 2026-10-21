package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
)

type refreshDescriptor struct {
	ExternalID   string `json:"external_id"`
	FetchMethod  string `json:"fetch_method"`
	DownloadPath string `json:"download_path"`
	MIME         string `json:"mime_type"`
	Size         int64  `json:"size_bytes"`
	SHA          string `json:"sha256"`
}

func imageDescriptor(t *testing.T, raw []byte) refreshDescriptor {
	t.Helper()
	var out struct {
		Artifacts []refreshDescriptor `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Len(t, out.Artifacts, 1, string(raw))
	return out.Artifacts[0]
}
func refreshImage(t *testing.T, h *harness, token, module string, d refreshDescriptor) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/"+module+"/"+d.FetchMethod,
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.NewString(),
			"arguments":        map[string]any{"external_id": d.ExternalID},
		},
	)
}
func requireImageRefresh(t *testing.T, h *harness, token, module string, raw []byte) refreshDescriptor {
	t.Helper()
	d := imageDescriptor(t, raw)
	code, refreshed := refreshImage(t, h, token, module, d)
	require.Equal(t, http.StatusOK, code, string(refreshed))
	require.Equal(t, d, imageDescriptor(t, refreshed))
	code, original := adminRequest(t, h, token, http.MethodGet, d.DownloadPath, nil)
	require.Equal(t, http.StatusOK, code, string(original))
	code, again := adminRequest(t, h, token, http.MethodGet, imageDescriptor(t, refreshed).DownloadPath, nil)
	require.Equal(t, http.StatusOK, code, string(again))
	require.Equal(t, original, again)
	require.Equal(t, d.SHA, previewDigest(again))
	require.Equal(t, d.Size, int64(len(again)))
	return d
}
func TestAvatarFileRefreshOriginalPreviewAndFinalized(t *testing.T) {
	t.Parallel()
	f, d := avatarFinalizationAPISetup(t, nil)
	code, raw := avatarDraftAgent(t, f, "fetch_original", uuid.New(), map[string]any{"draft_id": d.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	original := requireImageRefresh(t, f.h, f.owner, "avatars", raw)
	code, raw = avatarDraftAgent(
		t,
		f,
		"preview_draft",
		uuid.New(),
		map[string]any{"draft_id": d.ID, "expected_version": d.Version},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	preview := requireImageRefresh(t, f.h, f.owner, "avatars", raw)
	_, err := f.h.store.SaveAvatarDraft(
		t.Context(),
		f.scope,
		db.AvatarDraftSave{
			OperationID:     uuid.New(),
			DraftID:         d.ID,
			ExpectedVersion: d.Version,
			Transform:       avatar.Transform{ZoomPPM: 1500000},
		},
	)
	require.NoError(t, err)
	code, raw = refreshImage(t, f.h, f.owner, "avatars", preview)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw = refreshImage(t, f.h, f.owner, "avatars", original)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, original, imageDescriptor(t, raw))
	code, raw = avatarFinalizeAgent(t, f, uuid.New(), d.ID, 2)
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := agentAvatarReceipt(t, raw)
	code, raw = avatarDraftAgent(t, f, "fetch_finalized", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	finalized := requireImageRefresh(t, f.h, f.owner, "avatars", raw)
	_, foreign := registrationUser(t, f.h, 99298, false)
	for _, ref := range []refreshDescriptor{original, preview, finalized} {
		code, raw = refreshImage(t, f.h, foreign, "avatars", ref)
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	for _, locator := range []string{"avatar-original:" + uuid.Nil.String(), "avatar-preview:" + d.ID.String() + ":0", "avatar-preview:" + d.ID.String() + ":01", finalized.ExternalID + ":extra"} {
		code, raw = refreshImage(
			t,
			f.h,
			f.owner,
			"avatars",
			refreshDescriptor{FetchMethod: finalized.FetchMethod, ExternalID: locator},
		)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
}
func TestAvatarFileRefreshLegacyGenerated(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	code, raw := avatarRequest(t, f, f.owner, "generate", uuid.New(), map[string]any{"source_file_id": f.source})
	require.Equal(t, http.StatusOK, code, string(raw))
	receipt := generatedAvatar(t, raw)
	code, raw = avatarRequest(t, f, f.owner, "fetch", uuid.New(), map[string]any{"artifact_id": receipt.ArtifactID})
	require.Equal(t, http.StatusOK, code, string(raw))
	requireImageRefresh(t, f.h, f.owner, "avatars", raw)
}
