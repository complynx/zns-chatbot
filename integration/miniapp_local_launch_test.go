package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestMiniAppLocalLaunchKeepsHTTPSButtonAndLocalSessionSeparate(t *testing.T) {
	t.Parallel()
	const origin = "http://localhost:18382"
	const launch = "https://complynx.net/testbot/18382/app/"
	p := newPeers(t)
	p.miniAppOrigin = origin
	p.miniAppLaunchURL = launch
	p.miniAppAllowLocalHTTP = true
	p.loomDown = true
	h := setup(t, p)
	body := appBody(appProof(456), 82)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, body, "", "https://complynx.net")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw, _ = appCall(t, h, http.MethodPost, appBootstrapPath, body, "", origin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	require.NotEmpty(t, session.Token)
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/me", "", session.Token, origin)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/me", "", session.Token, "http://localhost.evil.invalid:18382")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	id := h.submitText(t, 96300, "/orders")
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	form := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	loomCalls, runCount := p.loomUnavailableCalls, len(p.runs)
	p.mu.Unlock()
	require.Equal(t, launch+"#orders", form.InlineKeyboard[0][0].WebApp.URL)
	require.Zero(t, loomCalls)
	require.Zero(t, runCount)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/session/logout", `{}`, session.Token, origin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/me", "", session.Token, origin)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}
