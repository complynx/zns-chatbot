package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const browserOrigin = "https://browser.example"

func browserFixture(t *testing.T) (*fixture, *browserauth.Service) {
	t.Helper()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET username='Alice' WHERE id='alice'`)
	require.NoError(t, err)
	s, err := browserauth.New(
		f.db,
		f.b.Host.Signer,
		f.b.TG,
		browserOrigin,
		f.b.Host.BrowserAuthRecipient,
		f.b.API.AuthenticateTelegram,
	)
	require.NoError(t, err)
	f.b.BrowserAuth = s
	return f, s
}

func browserRequest(
	t *testing.T,
	h http.Handler,
	method, path, origin, body string,
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("X-Browser-Auth", "1")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func startBrowser(t *testing.T, s *browserauth.Service, cookies ...*http.Cookie) (string, *http.Cookie) {
	t.Helper()
	w := browserRequest(
		t,
		s.Handler(),
		http.MethodPost,
		"/miniapp/auth/start",
		browserOrigin,
		`{"username":"@alice"}`,
		cookies...)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var value map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &value))
	response := w.Result()
	defer response.Body.Close()
	require.Len(t, response.Cookies(), 1)
	return value["request"], response.Cookies()[0]
}

func authCallback(t *testing.T, f *fixture, id, decision string, updateID int64) {
	t.Helper()
	response, err := http.Get(f.fake.URL + "/lab/state?user=101")
	require.NoError(t, err)
	defer response.Body.Close()
	var state struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&state))
	for _, message := range state.Messages {
		if len(message.Markup.Rows) > 0 && strings.Contains(message.Markup.Rows[0][0].Data, id) {
			handle(t, f.b, aliceCallback(updateID, message.ID, "ba|"+id+"|"+decision))
			return
		}
	}
	t.Fatal("Telegram consent message not found")
}

func TestBrowserConsentBindingRestartAndRevocation(t *testing.T) {
	t.Parallel()
	f, s := browserFixture(t)
	first, cookie := startBrowser(t, s)
	second, _ := startBrowser(t, s, cookie)
	authCallback(t, f, first, "approve", 10001)
	poll := "/miniapp/auth/status?request=" + second
	w := browserRequest(t, s.Handler(), http.MethodGet, poll, browserOrigin, "", cookie)
	assert.JSONEq(t, `{"result":"pending"}`, w.Body.String())
	authCallback(t, f, second, "approve", 10002)
	restarted, err := browserauth.New(
		f.db,
		f.b.Host.Signer,
		f.b.TG,
		browserOrigin,
		f.b.Host.BrowserAuthRecipient,
		f.b.API.AuthenticateTelegram,
	)
	require.NoError(t, err)
	w = browserRequest(t, restarted.Handler(), http.MethodGet, poll, browserOrigin, "", cookie)
	require.JSONEq(t, `{"result":"approved"}`, w.Body.String())
	response := w.Result()
	defer response.Body.Close()
	require.Len(t, response.Cookies(), 1)
	session := response.Cookies()[0]
	assert.True(t, session.HttpOnly)
	assert.True(t, session.Secure)
	assert.Equal(t, http.SameSiteStrictMode, session.SameSite)
	assert.Equal(t, "/miniapp", session.Path)
	other := browserRequest(t, s.Handler(), http.MethodGet, poll, browserOrigin, "")
	assert.Equal(t, http.StatusUnauthorized, other.Code)
	again := browserRequest(t, s.Handler(), http.MethodGet, poll, browserOrigin, "", cookie)
	againResponse := again.Result()
	defer againResponse.Body.Close()
	assert.Equal(t, session.Value, againResponse.Cookies()[0].Value, "response retry must not extend session")
	r := httptest.NewRequest(http.MethodGet, "/miniapp/api/massage/timetable", nil)
	r.AddCookie(session)
	sender, err := restarted.ResolveSession(r)
	require.NoError(t, err)
	assert.EqualValues(t, 101, sender)
	links := identity.Links{DB: f.db, Issuer: "https://browser-identity.example", BotID: 123}
	require.NoError(t, links.Bind(t.Context(), "alice", 101, "browser-alice"))
	f.b.API.Links, f.b.API.Exchange = links, runtimeProvider{}
	linked, err := browserauth.New(
		f.db,
		f.b.Host.Signer,
		f.b.TG,
		browserOrigin,
		f.b.Host.BrowserAuthRecipient,
		f.b.API.AuthenticateTelegram,
	)
	require.NoError(t, err)
	_, err = linked.ResolveSession(r)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=false WHERE owner='alice'`)
	require.NoError(t, err)
	_, err = linked.ResolveSession(r)
	require.ErrorIs(t, err, browserauth.ErrIdentity, "existing cookie cannot retain a revoked identity")
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.browser_auth SET session_expires_at=now()-interval '1 second' WHERE id=$1`,
		second,
	)
	require.NoError(t, err)
	_, err = restarted.ResolveSession(r)
	require.ErrorIs(t, err, browserauth.ErrIdentity)
}

func TestBrowserDeclineExpiryCancelRateAndCSRF(t *testing.T) {
	t.Parallel()
	f, s := browserFixture(t)
	w := browserRequest(
		t,
		s.Handler(),
		http.MethodPost,
		"/miniapp/auth/start",
		"https://evil.example",
		`{"username":"alice"}`,
	)
	assert.Equal(t, http.StatusForbidden, w.Code)
	id, cookie := startBrowser(t, s)
	authCallback(t, f, id, "decline", 11001)
	w = browserRequest(t, s.Handler(), http.MethodGet, "/miniapp/auth/status?request="+id, browserOrigin, "", cookie)
	assert.JSONEq(t, `{"result":"declined"}`, w.Body.String())
	id, _ = startBrowser(t, s, cookie)
	_, err := f.db.Exec(t.Context(), `UPDATE bot.browser_auth SET expires_at=now()-interval '1 second' WHERE id=$1`, id)
	require.NoError(t, err)
	authCallback(t, f, id, "approve", 11002)
	w = browserRequest(t, s.Handler(), http.MethodGet, "/miniapp/auth/status?request="+id, browserOrigin, "", cookie)
	assert.JSONEq(t, `{"result":"expired"}`, w.Body.String())
	id, _ = startBrowser(t, s, cookie)
	w = browserRequest(t, s.Handler(), http.MethodPost, "/miniapp/auth/cancel?request="+id, browserOrigin, `{}`, cookie)
	require.Equal(t, http.StatusOK, w.Code)
	authCallback(t, f, id, "approve", 11003)
	w = browserRequest(t, s.Handler(), http.MethodGet, "/miniapp/auth/status?request="+id, browserOrigin, "", cookie)
	assert.JSONEq(t, `{"result":"cancelled"}`, w.Body.String())
	w = browserRequest(
		t,
		s.Handler(),
		http.MethodPost,
		"/miniapp/auth/start",
		browserOrigin,
		`{"username":"alice"}`,
		cookie,
	)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	gateway := (miniapp.Gateway{BrowserAuth: s, API: f.b.API, Token: "sandbox"}).Handler()
	w = browserRequest(t, gateway, http.MethodPost, "/miniapp/api/quote", "https://evil.example", `{}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLegacyBrowserLongPollOriginAndCookieContract(t *testing.T) {
	t.Parallel()
	f, s := browserFixture(t)
	const allowed = "https://www.mealty.example"
	require.NoError(t, s.ConfigureLegacyOrigins(allowed))
	w := browserRequest(t, s.LegacyHandler(), http.MethodGet, "/auth?check=1", allowed, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"result":"unauthorized"}`, w.Body.String())
	assert.Equal(t, allowed, w.Header().Get("Access-Control-Allow-Origin"))
	w = browserRequest(t, s.LegacyHandler(), http.MethodGet, "/auth?check=1", "https://evil.example", "")
	assert.Equal(t, http.StatusForbidden, w.Code)
	ended := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		ended <- browserRequest(t, s.LegacyHandler(), http.MethodGet, "/auth?username=alice", allowed, "")
	}()
	var id string
	require.Eventually(t, func() bool {
		return f.db.QueryRow(t.Context(), `SELECT id FROM bot.browser_auth WHERE state='pending'`).Scan(&id) == nil
	}, 5*time.Second, 10*time.Millisecond)
	// Observe delivery before invoking the same private callback path as Telegram.
	require.Eventually(t, func() bool {
		response, err := http.Get(f.fake.URL + "/lab/state?user=101")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		var state struct {
			Messages []telegram.Message `json:"messages"`
		}
		return json.NewDecoder(response.Body).Decode(&state) == nil && len(state.Messages) > 0
	}, 5*time.Second, 10*time.Millisecond)
	authCallback(t, f, id, "approve", 12001)
	select {
	case w = <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("legacy consent did not finish")
	}
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"result":"authorized"}`, w.Body.String())
	response := w.Result()
	defer response.Body.Close()
	for _, cookie := range response.Cookies() {
		assert.True(t, cookie.Secure)
		assert.True(t, cookie.HttpOnly)
		assert.Equal(t, http.SameSiteNoneMode, cookie.SameSite)
	}
	w = browserRequest(t, s.LegacyHandler(), http.MethodGet, "/auth?check=1", allowed, "", response.Cookies()...)
	assert.JSONEq(t, `{"result":"authorized"}`, w.Body.String())
}
