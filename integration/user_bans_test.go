package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBanBeforeOnboardingRejectsTelegramTextAndMediaWithoutEffects(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	ban, err := h.store.BanTelegramUser(t.Context(), 123, 456, "operator:test", "reviewed test restriction")
	require.NoError(t, err)
	for n, content := range []string{
		`"text":"/start"`,
		`"text":"Hello"`,
		`"voice":{"file_id":"voice","file_unique_id":"unique-voice","duration":1}`,
		`"sticker":{"file_id":"sticker","file_unique_id":"unique-sticker","width":64,"height":64}`,
	} {
		raw := fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"date":1740000000,`+
			`"from":{"id":456,"language_code":"en"},"chat":{"id":456,"type":"private"},%s}}`,
			98000+n, 98000+n, content)
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodPost,
			h.server.URL+"/telegram/webhook", strings.NewReader(raw))
		require.NoError(t, requestErr)
		request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
		response, sendErr := h.server.Client().Do(request)
		require.NoError(t, sendErr)
		body, readErr := io.ReadAll(response.Body)
		require.NoError(t, response.Body.Close())
		require.NoError(t, readErr)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.JSONEq(t, `{"ignored":true,"reason":"access_revoked"}`, string(body), "payload: %s", content)
	}
	var tasks, files, symbols int
	err = h.store.Pool().QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM shuttle_tasks), (SELECT count(*) FROM shuttle_files),
		(SELECT count(*) FROM shuttle_symbols)`).Scan(&tasks, &files, &symbols)
	require.NoError(t, err)
	require.Zero(t, tasks)
	require.Zero(t, files)
	require.Zero(t, symbols)
	owner, err := h.store.GetUser(t.Context(), ban.UserID)
	require.NoError(t, err)
	require.True(t, owner.Revoked)
	require.Empty(t, owner.Subject)
	require.Empty(t, owner.Organization)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.users)
	require.Empty(t, p.runs)
	require.Empty(t, p.sent)
}
