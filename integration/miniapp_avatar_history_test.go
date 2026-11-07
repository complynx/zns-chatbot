package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
)

func TestMiniAppHistoryAvatarPreviewDoesNotConsumeDrafts(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.mu.Unlock()
	path := "/app/avatars/history/" + source.ArchiveID.String() + "/" + source.RecordID.String() + "/preview"
	expected, err := renderer.Render(t.Context(), f.raw, avatar.IdentityTransform())
	require.NoError(t, err)
	for range 6 {
		code, body, headers := avatarPreviewRead(f.h, path, f.session.Token, false)
		require.Equal(t, http.StatusOK, code, string(body))
		require.Equal(t, "image/jpeg", headers.Get("Content-Type"))
		require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
		require.Len(t, body, len(expected.JPEG))
		require.Equal(t, previewDigest(expected.JPEG), previewDigest(body))
	}
	for _, table := range []string{"shuttle_avatar_drafts", "shuttle_avatars", "shuttle_outbox", "shuttle_tasks"} {
		var count int
		require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestMiniAppHistoryAvatarPreviewDeniesForeignAndMissingSource(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	path := "/app/avatars/history/" + source.ArchiveID.String() + "/" + source.RecordID.String() + "/preview"
	registrationUser(t, f.h, 457, false)
	foreign := detailsAppSession(t, f.h, 457, 212)
	code, body, _ := appCall(t, f.h, http.MethodGet, path, "", foreign.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code, string(body))
	f.p.mu.Lock()
	gets := f.p.fileGets
	f.p.fileGone = true
	f.p.mu.Unlock()
	require.Zero(t, gets, "foreign reads do not contact Telegram")
	code, body, _ = appCall(t, f.h, http.MethodGet, path, "", f.session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(body))
	require.Contains(t, string(body), "source photo unavailable; send it again")
	require.NotContains(t, string(body), "PRIVATE_")
}

func TestMiniAppHistoryAvatarPreviewRechecksOwnerAfterTelegramIO(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	source := avatarHistoryFixture(t, f)
	f.p.mu.Lock()
	f.p.fileBytes = f.raw
	f.p.fileReadHook = func() {
		_, err := f.h.store.Pool().Exec(
			t.Context(),
			"UPDATE shuttle_users SET tg_user_id=999 WHERE id=$1",
			f.scope.UserID,
		)
		require.NoError(t, err)
	}
	f.p.mu.Unlock()
	path := "/app/avatars/history/" + source.ArchiveID.String() + "/" + source.RecordID.String() + "/preview"
	code, body, _ := appCall(t, f.h, http.MethodGet, path, "", f.session.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code, string(body))
	require.NotContains(t, string(body), "PRIVATE_")
}
