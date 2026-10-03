package sandbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func providerFaultPublicRequest(t *testing.T, f *sandbox.Fake, ctx context.Context,
	method, path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", os.Getenv("R104_CONTROL_KEY"))
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	return w
}

func providerFaultPublicRead(t *testing.T, f *sandbox.Fake, key string) map[string]json.RawMessage {
	t.Helper()
	w := providerFaultPublicRequest(t, f, t.Context(), http.MethodGet, "/lab/provider-fault?case="+key, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var receipt map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	return receipt
}

func assertProviderFaultDocument(t *testing.T, f *sandbox.Fake) {
	t.Helper()
	var body bytes.Buffer
	upload := multipart.NewWriter(&body)
	require.NoError(t, upload.WriteField("chat_id", "101"))
	part, err := upload.CreateFormFile("document", "synthetic.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("private document canary"))
	require.NoError(t, err)
	require.NoError(t, upload.Close())
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/botsynthetic-token/sendDocument", &body)
	r.Header.Set("Content-Type", upload.FormDataContentType())
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
}

func TestProviderFaultPostgresRestartAndFailedSave(t *testing.T) {
	db := newNativeIngressDB(t)
	t.Setenv("R104_CONTROL_KEY", strings.Repeat("s", 32))
	f, stop := startNativeIngress(t, db)
	arm := `{"case":"restart","action":"arm","mode":"rate_limit","method":"all_delivery","count":3,"lifetime_seconds":600,"retry_after":2}`
	w := providerFaultPublicRequest(t, f, t.Context(), http.MethodPost, "/lab/provider-fault", arm)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	original := providerFaultPublicRead(t, f, "restart")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	w = providerFaultPublicRequest(t, f, canceled, http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":101,"text":"failed save"}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, original, providerFaultPublicRead(t, f, "restart"))
	w = providerFaultPublicRequest(t, f, t.Context(), http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":101,"text":"private canary"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	before := providerFaultPublicRead(t, f, "restart")
	stop()
	restarted, stopRestarted := startNativeIngress(t, db)
	after := providerFaultPublicRead(t, restarted, "restart")
	require.Equal(t, before, after, "restart preserves exact selector, deadline, used count and evidence")
	w = providerFaultPublicRequest(t, restarted, t.Context(), http.MethodPost, "/lab/provider-fault", arm)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, before, providerFaultPublicRead(t, restarted, "restart"), "replay cannot replenish the count")
	assertProviderFaultDocument(t, restarted)
	var files int
	require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM bot.fake_files").Scan(&files))
	require.Zero(t, files, "provider rejection must precede file mutation")
	w = providerFaultPublicRequest(t, restarted, t.Context(), http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":202,"text":"last rejection"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	w = providerFaultPublicRequest(t, restarted, t.Context(), http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":202,"text":"normal after exhaustion"}`)
	require.Equal(t, http.StatusOK, w.Code)
	exhausted := providerFaultPublicRead(t, restarted, "restart")
	require.JSONEq(t, `0`, string(exhausted["remaining"]))
	require.JSONEq(t, `"exhausted"`, string(exhausted["state"]))
	stopRestarted()
	restored, stopRestored := startNativeIngress(t, db)
	require.Equal(t, exhausted, providerFaultPublicRead(t, restored, "restart"))
	assertProviderFaultCredentialRestart(t, db, restored, stopRestored)
}

func assertProviderFaultCredentialRestart(t *testing.T, db *pgxpool.Pool,
	f *sandbox.Fake, stop func(),
) {
	t.Helper()
	arm := `{"case":"credential","action":"arm","mode":"credential","method":"all_delivery","count":2,"lifetime_seconds":600}`
	w := providerFaultPublicRequest(t, f, t.Context(), http.MethodPost, "/lab/provider-fault", arm)
	require.Equal(t, http.StatusOK, w.Code)
	w = providerFaultPublicRequest(t, f, t.Context(), http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":101,"text":"one"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	before := providerFaultPublicRead(t, f, "credential")
	stop()
	restarted, stopRestarted := startNativeIngress(t, db)
	require.Equal(t, before, providerFaultPublicRead(t, restarted, "credential"))
	w = providerFaultPublicRequest(t, restarted, t.Context(), http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":202,"text":"two"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	w = providerFaultPublicRequest(t, restarted, t.Context(), http.MethodPost, "/lab/provider-fault", `{"case":"expiry","action":"arm","mode":"rate_limit","method":"sendMessage","count":1,"lifetime_seconds":1}`)
	require.Equal(t, http.StatusOK, w.Code)
	expiry := providerFaultPublicRead(t, restarted, "expiry")
	var deadline time.Time
	require.NoError(t, json.Unmarshal(expiry["deadline"], &deadline))
	time.Sleep(time.Until(deadline) + 20*time.Millisecond)
	stopRestarted()
	expired, stopExpired := startNativeIngress(t, db)
	defer stopExpired()
	w = providerFaultPublicRequest(t, expired, t.Context(), http.MethodPost, "/botsynthetic-token/sendMessage", `{"chat_id":101,"text":"after expiry"}`)
	require.Equal(t, http.StatusOK, w.Code)
	observed := providerFaultPublicRead(t, expired, "expiry")
	require.JSONEq(t, `"expired"`, string(observed["state"]))
	require.JSONEq(t, `[]`, string(observed["consumptions"]))
}
