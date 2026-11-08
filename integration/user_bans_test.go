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
	ban, err := h.store.BanTelegramUser(t.Context(), 123, 456, "operator:test", "reviewed test restriction", "")
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
	require.Len(t, p.sent, 4)
	for _, sent := range p.sent {
		require.Equal(t, int64(456), sent.ChatID)
		require.Equal(t, "You cannot perform this action.", sent.Text)
		require.Empty(t, sent.Entities)
	}
}

func TestBanReplyShowsOnlyOptionalPublicReasonAndAcknowledgesCallbacks(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	const internalReason = "PRIVATE operator investigation"
	const publicReason = "Обратись к организатору <b>без разметки</b>."
	_, err := h.store.BanTelegramUser(t.Context(), 123, 456, "operator:test", internalReason, publicReason)
	require.NoError(t, err)
	for _, raw := range []string{
		`{"update_id":98010,"message":{"message_id":98010,"from":{"id":456,"language_code":"ru"},` +
			`"chat":{"id":456,"type":"private"},"text":"/start"}}`,
		`{"update_id":98011,"callback_query":{"id":"banned-callback","from":{"id":456,"language_code":"ru"},` +
			`"message":{"message_id":98010,"chat":{"id":456,"type":"private"}},"data":"old-button"}}`,
	} {
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
		require.JSONEq(t, `{"ignored":true,"reason":"access_revoked"}`, string(body))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 2)
	for _, sent := range p.sent {
		require.Equal(t, "Вы не можете выполнить это действие.\n\n"+publicReason, sent.Text)
		require.NotContains(t, sent.Text, internalReason)
		require.Empty(t, sent.Entities)
	}
	require.Empty(t, p.users)
	require.Empty(t, p.runs)
	require.Equal(t, []string{"banned-callback"}, p.callbackAcks)
}

func TestBanReplyLostAcknowledgmentDoesNotStartWorkOrRetrySending(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	_, err := h.store.BanTelegramUser(t.Context(), 123, 456, "operator:test", "private", "")
	require.NoError(t, err)
	raw := `{"update_id":98020,"message":{"message_id":98020,"from":{"id":456},` +
		`"chat":{"id":456,"type":"private"},"text":"/start"}}`
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		h.server.URL+"/telegram/webhook", strings.NewReader(raw))
	require.NoError(t, err)
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
	response, err := h.server.Client().Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 1)
	require.Empty(t, p.users)
	require.Empty(t, p.runs)
}
