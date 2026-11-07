package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func historyAvatarRequest(t *testing.T, f historyAvatarFixture, token, method string) (int, []byte) {
	t.Helper()
	return adminRequest(t, f.h, token, http.MethodPost, "/internal/agent/history/"+method, map[string]any{
		"protocol_version": 1, "operation_id": "read-historical-image",
		"arguments": map[string]any{"record_id": f.recordID, "archive_id": f.archiveID},
	})
}

func TestHistoryAvatarAPIDownloadsOnDemandAndReportsLostFile(t *testing.T) {
	t.Parallel()
	f := historyAvatarSetup(t)
	_, token := registrationUser(t, f.h, 42, false)
	photo := avatarDraftPhoto(t, false)
	p := f.h.peers
	p.mu.Lock()
	p.fileBytes = photo
	p.mu.Unlock()
	code, body := historyAvatarRequest(t, f, token, "avatar_info")
	require.Equal(t, http.StatusOK, code, string(body))
	require.NotContains(t, string(body), "PRIVATE_")
	p.mu.Lock()
	gets := p.fileGets
	p.mu.Unlock()
	require.Zero(t, gets, "metadata reads do not download photographs")
	code, body = historyAvatarRequest(t, f, token, "avatar_fetch")
	require.Equal(t, http.StatusOK, code, string(body))
	require.NotContains(t, string(body), "PRIVATE_")
	var result struct {
		Artifacts []struct {
			Path       string `json:"download_path"`
			Method     string `json:"fetch_method"`
			ExternalID string `json:"external_id"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	require.Len(t, result.Artifacts, 1)
	require.Equal(t, "avatar_refetch", result.Artifacts[0].Method)
	require.Equal(t, f.archiveID.String()+":"+f.recordID.String(), result.Artifacts[0].ExternalID)
	descriptor := requireImageRefresh(t, f.h, token, "history", body)
	_, foreign := registrationUser(t, f.h, 43, true)
	code, denied := refreshImage(t, f.h, foreign, "history", descriptor)
	require.Equal(t, http.StatusNotFound, code, string(denied))
	path := result.Artifacts[0].Path
	code, downloaded := adminRequest(t, f.h, token, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code, string(downloaded))
	require.Equal(t, photo, downloaded)
	p.mu.Lock()
	gets = p.fileGets
	p.fileGone = true
	p.mu.Unlock()
	require.Equal(t, 5, gets, "each content read refreshes the Telegram path")
	code, body = historyAvatarRequest(t, f, token, "avatar_fetch")
	require.Equal(t, http.StatusServiceUnavailable, code, string(body))
	require.NotContains(t, string(body), "artifacts")
	require.NotContains(t, string(body), "PRIVATE_")
	code, body = historyAvatarRequest(t, f, token, "avatar_info")
	require.Equal(t, http.StatusOK, code, string(body), "saved references do not assert availability")
}

func TestHistoryAvatarAPIDeniesForeignUserBeforeTelegramIO(t *testing.T) {
	t.Parallel()
	f := historyAvatarSetup(t)
	_, token := registrationUser(t, f.h, 43, true)
	code, body := historyAvatarRequest(t, f, token, "avatar_fetch")
	require.Equal(t, http.StatusNotFound, code, string(body))
	require.NotContains(t, string(body), "PRIVATE_")
	f.h.peers.mu.Lock()
	defer f.h.peers.mu.Unlock()
	require.Zero(t, f.h.peers.fileGets)
}

func TestHistoryAvatarAPIRereadsOwnerAfterTelegramIO(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"avatar_fetch", "content"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f := historyAvatarSetup(t)
			id, token := registrationUser(t, f.h, 42, false)
			p := f.h.peers
			photo := avatarDraftPhoto(t, false)
			p.mu.Lock()
			p.fileBytes = photo
			p.fileReadHook = func() {
				_, err := f.h.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET tg_user_id=44 WHERE id=$1`, id)
				require.NoError(t, err)
			}
			p.mu.Unlock()
			var code int
			var body []byte
			if method == "content" {
				path := "/internal/history-avatars/" + f.archiveID.String() + "/" + f.recordID.String() + "/content"
				code, body = adminRequest(t, f.h, token, http.MethodGet, path, nil)
			} else {
				code, body = historyAvatarRequest(t, f, token, method)
			}
			require.Equal(t, http.StatusNotFound, code, string(body))
			require.NotEqual(t, photo, body)
			require.NotContains(t, string(body), "artifacts")
			require.NotContains(t, string(body), "PRIVATE_")
		})
	}
}

func TestHistoryAvatarFileRefreshKeepsActualMIMEAndArchiveScope(t *testing.T) {
	t.Parallel()
	f := historyAvatarSetup(t)
	_, token := registrationUser(t, f.h, 42, false)
	f.h.peers.mu.Lock()
	f.h.peers.fileBytes = []byte("%PDF-1.7\nsynthetic historical bytes\n%%EOF")
	f.h.peers.mu.Unlock()
	code, raw := historyAvatarRequest(t, f, token, "avatar_fetch")
	require.Equal(t, http.StatusOK, code, string(raw))
	d := requireImageRefresh(t, f.h, token, "history", raw)
	require.Equal(t, "application/pdf", d.MIME)
	code, raw = adminRequest(t, f.h, token, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID    string   `json:"id"`
				MIMEs []string `json:"artifact_mime_types"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	found := false
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_history" {
			continue
		}
		for _, method := range module.Methods {
			if method.ID == d.FetchMethod {
				require.Contains(t, method.MIMEs, d.MIME)
				found = true
			}
		}
	}
	require.True(t, found, "advertised refresh method must authorize the imported MIME")
	wrong := d
	wrong.ExternalID = uuid.NewString() + ":" + f.recordID.String()
	code, raw = refreshImage(t, f.h, token, "history", wrong)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	f.h.peers.mu.Lock()
	f.h.peers.fileGone = true
	f.h.peers.mu.Unlock()
	code, raw = refreshImage(t, f.h, token, "history", d)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	require.NotContains(t, string(raw), "artifacts")
}
