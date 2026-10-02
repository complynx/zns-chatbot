package sandbox_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/store"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const nativeIngressToken = "synthetic-token"

type ingressPublicCommand struct {
	Case        string `json:"case"`
	Action      string `json:"action"`
	User        int64  `json:"user"`
	HoldSeconds int    `json:"hold_seconds"`
	SHA256      string `json:"sha256"`
}

type ingressPublicReceipt struct {
	User       int64     `json:"user"`
	State      string    `json:"state"`
	Deadline   time.Time `json:"deadline"`
	ArmedAt    time.Time `json:"armed_at"`
	CapturedAt time.Time `json:"captured_at"`
	Response   []byte    `json:"response"`
	SHA256     string    `json:"sha256"`
	Replay     string    `json:"replay"`
}

func nativeIngressRequest(t *testing.T, f *sandbox.Fake, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	r := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(raw))
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", os.Getenv("R104_CONTROL_KEY"))
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w
}

func nativeIngressReceipt(t *testing.T, f *sandbox.Fake) ingressPublicReceipt {
	t.Helper()
	w := nativeIngressRequest(t, f, http.MethodGet, "/lab/registration-ingress?case=case-a", nil)
	var receipt ingressPublicReceipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	return receipt
}

func nativeIngressPoll(t *testing.T, f *sandbox.Fake, offset int64) []byte {
	t.Helper()
	return nativeIngressRequest(
		t,
		f,
		http.MethodPost,
		"/botsynthetic-token/getUpdates",
		map[string]int64{"offset": offset},
	).Body.Bytes()
}

func startNativeIngress(t *testing.T, db *pgxpool.Pool) (*sandbox.Fake, func()) {
	t.Helper()
	t.Setenv("R104_JOURNAL", filepath.Join(t.TempDir(), "provider.jsonl"))
	ctx, cancel := context.WithCancel(t.Context())
	f, err := sandbox.New(ctx, db, nativeIngressToken)
	require.NoError(t, err)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			require.Eventually(t, func() bool {
				connection, dialErr := net.DialTimeout("tcp", "127.0.0.1:8090", 50*time.Millisecond)
				if dialErr != nil {
					return true
				}
				_ = connection.Close()
				return false
			}, time.Second, 10*time.Millisecond)
		})
	}
	t.Cleanup(stop)
	return f, stop
}

// The opt-in environment and native control listener are process-wide, so this test is serial.
func TestRegistrationIngressDurableOriginalAndProductDedup(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for owned PostgreSQL integration")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "sandbox_ingress_" + rand.Text()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		require.NoError(t, dropErr)
	})
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, store.Migrate(t.Context(), db))
	t.Setenv("R104_CONTROL_KEY", strings.Repeat("s", 32))
	f, stop := startNativeIngress(t, db)
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 10})
	w := nativeIngressRequest(
		t,
		f,
		http.MethodPost,
		"/lab/input",
		map[string]any{"user": 101, "text": "specific event request"},
	)
	var update telegram.Update
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &update))
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, f, 0)))
	item := nativeIngressReceipt(t, f)
	require.Equal(t, "held", item.State)
	var canonical bytes.Buffer
	require.NoError(
		t,
		json.NewEncoder(&canonical).Encode(map[string]any{"ok": true, "result": []telegram.Update{update}}),
	)
	require.Equal(t, canonical.Bytes(), item.Response)
	digest := sha256.Sum256(item.Response)
	require.Equal(t, hex.EncodeToString(digest[:]), item.SHA256)
	stop()
	restored, stopRestored := startNativeIngress(t, db)
	reloaded := nativeIngressReceipt(t, restored)
	require.True(t, item.Deadline.Equal(reloaded.Deadline))
	require.True(t, item.ArmedAt.Equal(reloaded.ArmedAt))
	require.True(t, item.CapturedAt.Equal(reloaded.CapturedAt))
	item.Deadline, item.ArmedAt, item.CapturedAt = reloaded.Deadline, reloaded.ArmedAt, reloaded.CapturedAt
	require.Equal(t, item, reloaded)
	release := ingressPublicCommand{Case: "case-a", Action: "release", User: 101, SHA256: item.SHA256}
	nativeIngressRequest(t, restored, http.MethodPost, "/lab/registration-ingress", release)
	require.Equal(t, item.Response, nativeIngressPoll(t, restored, 0))
	ref := registrationingress.Reference{BotID: 999, UpdateID: update.ID}
	for range 2 {
		tx, txErr := db.Begin(t.Context())
		require.NoError(t, txErr)
		require.NoError(t, registrationingress.SaveTelegram(t.Context(), tx, ref, 101))
		require.NoError(t, tx.Commit(t.Context()))
	}
	var count int
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT count(*) FROM core.registration_ingress WHERE bot_id=999 AND request_key=$1`,
		strconv.FormatInt(update.ID, 10)).Scan(&count))
	require.Equal(t, 1, count)
	nativeIngressPoll(t, restored, update.ID+1)
	release.Action = "replay"
	nativeIngressRequest(t, restored, http.MethodPost, "/lab/registration-ingress", release)
	stopRestored()
	restoredAgain, stopAgain := startNativeIngress(t, db)
	require.Equal(t, item.Response, nativeIngressPoll(t, restoredAgain, update.ID+1))
	require.Equal(t, "replay_consumed", nativeIngressReceipt(t, restoredAgain).Replay)
	stopAgain()
	restarted, stopRestarted := startNativeIngress(t, db)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, restarted, update.ID+1)))
	_, err = db.Exec(
		t.Context(),
		`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,case-a,sha256}',to_jsonb('changed'::text))`,
	)
	require.NoError(t, err)
	stopRestarted()
	_, err = sandbox.New(t.Context(), db, nativeIngressToken)
	require.ErrorContains(t, err, "original response")
}
