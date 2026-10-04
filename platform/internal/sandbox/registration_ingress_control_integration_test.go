package sandbox_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
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
	User              int64     `json:"user"`
	State             string    `json:"state"`
	Deadline          time.Time `json:"deadline"`
	ArmedAt           time.Time `json:"armed_at"`
	CapturedAt        time.Time `json:"captured_at"`
	CustodyFinishedAt time.Time `json:"custody_finished_at"`
	Response          []byte    `json:"response"`
	SHA256            string    `json:"sha256"`
	Replay            string    `json:"replay"`
	ReplayArmedAt     time.Time `json:"replay_armed_at"`
	ReplayDeadline    time.Time `json:"replay_deadline"`
	ReplayFinishedAt  time.Time `json:"replay_finished_at"`
	DeliveryOrder     string    `json:"delivery_order,omitempty"`
	ReplayOrder       string    `json:"replay_order,omitempty"`
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
	return nativeIngressReceiptFor(t, f, "case-a")
}

func nativeIngressReceiptFor(t *testing.T, f *sandbox.Fake, caseName string) ingressPublicReceipt {
	t.Helper()
	w := nativeIngressRequest(t, f, http.MethodGet, "/lab/registration-ingress?case="+caseName, nil)
	var receipt ingressPublicReceipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
	return receipt
}

func assertNativeReplayExpiry(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	f, stop := startNativeIngress(t, db)
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "bounded", Action: "arm", User: 202, HoldSeconds: 10})
	w := nativeIngressRequest(
		t,
		f,
		http.MethodPost,
		"/lab/input",
		map[string]any{"user": 202, "text": "expiry original"},
	)
	var update telegram.Update
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &update))
	nativeIngressPoll(t, f, update.ID)
	item := nativeIngressReceiptFor(t, f, "bounded")
	command := ingressPublicCommand{Case: "bounded", Action: "release", User: 202, SHA256: item.SHA256}
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	require.Equal(t, item.Response, nativeIngressPoll(t, f, update.ID))
	command.Action = "replay"
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	pending := nativeIngressReceiptFor(t, f, "bounded")
	require.Equal(t, "pending", pending.Replay)
	require.Equal(t, 10*time.Second, pending.ReplayDeadline.Sub(pending.ReplayArmedAt))
	require.True(t, pending.ReplayFinishedAt.IsZero())
	stop()
	restarted, stopRestarted := startNativeIngress(t, db)
	time.Sleep(time.Until(pending.ReplayDeadline) + 20*time.Millisecond)
	expired := assertReplayExpiryReadback(t, db, restarted, pending)
	nativeIngressRequest(t, restarted, http.MethodPost, "/lab/registration-ingress", command)
	require.Equal(t, "replay_expired", nativeIngressReceiptFor(t, restarted, "bounded").Replay)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, restarted, update.ID+1)))
	nativeIngressRequest(t, restarted, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "successor", Action: "arm", User: 303, HoldSeconds: 1})
	stopRestarted()
	terminal, stopTerminal := startNativeIngress(t, db)
	readback := nativeIngressReceiptFor(t, terminal, "bounded")
	require.Equal(t, "replay_expired", readback.Replay)
	require.True(t, expired.ReplayFinishedAt.Equal(readback.ReplayFinishedAt))
	nativeIngressRequest(t, terminal, http.MethodPost, "/lab/registration-ingress", command)
	require.Equal(t, "replay_expired", nativeIngressReceiptFor(t, terminal, "bounded").Replay)
	stopTerminal()
	assertNativeReplayRestoreValidation(t, db, expired)
}

func assertReplayExpiryReadback(
	t *testing.T,
	db *pgxpool.Pool,
	restarted *sandbox.Fake,
	pending ingressPublicReceipt,
) ingressPublicReceipt {
	t.Helper()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(canceled, http.MethodGet, "/lab/registration-ingress?case=bounded", nil)
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", os.Getenv("R104_CONTROL_KEY"))
	failed := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(failed, r)
	require.Equal(t, http.StatusServiceUnavailable, failed.Code)
	var raw []byte
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT data #> '{RegistrationIngress,cases,bounded}' FROM bot.fake_state`).Scan(&raw))
	var stored ingressPublicReceipt
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.Equal(t, "pending", stored.Replay)
	require.True(t, stored.ReplayArmedAt.Equal(pending.ReplayArmedAt))
	require.True(t, stored.ReplayDeadline.Equal(pending.ReplayDeadline))
	require.True(t, stored.ReplayFinishedAt.IsZero())
	expired := nativeIngressReceiptFor(t, restarted, "bounded")
	require.Equal(t, "replay_expired", expired.Replay)
	require.Equal(t, pending.Response, expired.Response)
	require.True(t, expired.ReplayArmedAt.Equal(pending.ReplayArmedAt))
	require.True(t, expired.ReplayDeadline.Equal(pending.ReplayDeadline))
	require.False(t, expired.ReplayFinishedAt.Before(expired.ReplayDeadline))
	return expired
}

func assertNativeReplayRestoreValidation(t *testing.T, db *pgxpool.Pool, valid ingressPublicReceipt) {
	t.Helper()
	for _, mutate := range []func(*ingressPublicReceipt){
		func(item *ingressPublicReceipt) { item.ReplayArmedAt = item.CapturedAt.Add(-time.Nanosecond) },
		func(item *ingressPublicReceipt) { item.ReplayDeadline = item.ReplayArmedAt },
		func(item *ingressPublicReceipt) { item.ReplayDeadline = item.ReplayArmedAt.Add(11 * time.Second) },
		func(item *ingressPublicReceipt) { item.ReplayFinishedAt = item.ReplayDeadline.Add(-time.Nanosecond) },
	} {
		corrupt := valid
		mutate(&corrupt)
		raw, err := json.Marshal(corrupt)
		require.NoError(t, err)
		_, err = db.Exec(
			t.Context(),
			`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,bounded}',$1::jsonb)`,
			string(raw),
		)
		require.NoError(t, err)
		_, err = sandbox.New(t.Context(), db, nativeIngressToken)
		require.ErrorContains(t, err, "replay")
	}
	raw, err := json.Marshal(valid)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(),
		`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,bounded}',$1::jsonb)`, string(raw))
	require.NoError(t, err)
	restored, stop := startNativeIngress(t, db)
	require.Equal(t, "replay_expired", nativeIngressReceiptFor(t, restored, "bounded").Replay)
	stop()
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

type ingressIntakeObservation struct {
	count int
	err   error
}

type ingressIntakeObserver struct {
	db       *pgxpool.Pool
	started  chan ingressIntakeObservation
	finished chan error
}

func (o ingressIntakeObserver) Start(ctx context.Context, _ string) (context.Context, func(error)) {
	var value ingressIntakeObservation
	value.err = o.db.QueryRow(ctx, `SELECT count(*) FROM bot.telegram_inbox`).Scan(&value.count)
	select {
	case o.started <- value:
	case <-ctx.Done():
	}
	return ctx, func(err error) {
		select {
		case o.finished <- err:
		case <-ctx.Done():
		}
	}
}

type ingressCountingModel struct {
	calls atomic.Int32
}

func (m *ingressCountingModel) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	m.calls.Add(1)
	return (agent.Scripted{}).Plan(ctx, input)
}

// The normal poller parses both exact provider responses and owns the inbox transaction.
func assertNativeIngressProductDedup(t *testing.T, db *pgxpool.Pool, f *sandbox.Fake,
	update telegram.Update, original, replay []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var polls atomic.Int32
	provider := f.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsynthetic-token/getUpdates" {
			provider.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch polls.Add(1) {
		case 1:
			_, _ = w.Write(original)
		case 2:
			_, _ = w.Write(replay)
		default:
			cancel()
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		}
	}))
	t.Cleanup(server.Close)
	settings := delivery.Settings{BotID: 999, BotInterval: 50 * time.Millisecond,
		ChatInterval: time.Second, Fallback: 30 * time.Second,
		UncertaintyRetryBase: delivery.DefaultUncertaintyRetryBase}
	require.NoError(t, settings.Validate())
	services := appservices.NewServices(db, appservices.Options{Delivery: settings})
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	application := httptest.NewServer(api.Handler(services, signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(application.Close)
	client := appclient.Client{Base: application.URL, SandboxToken: signer.Token}
	observations := make(chan ingressIntakeObservation, 2)
	completed := make(chan error, 2)
	model := &ingressCountingModel{}
	consumer := &bot.Bot{
		DB:       db,
		API:      client,
		Delivery: settings,
		Model:    model,
		Host:     appclient.Host{Base: application.URL, Signer: signer, UserToken: client.UserToken},
		TG:       telegram.Client{Base: server.URL, Token: nativeIngressToken},
		Observer: ingressIntakeObserver{
			db:       db,
			started:  observations,
			finished: completed,
		},
		Logger: slog.New(slog.DiscardHandler),
	}
	err := consumer.Run(ctx)
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}
	require.GreaterOrEqual(t, polls.Load(), int32(3), "both exact responses reached the product poller")
	require.Len(t, observations, 1, "replay must not dispatch a second business operation")
	observation := <-observations
	require.NoError(t, observation.err)
	require.Equal(t, 1, observation.count, "intake committed the inbox before dispatch")
	require.Len(t, completed, 1)
	require.NoError(t, <-completed, "original business operation must succeed")
	require.Equal(t, int32(1), model.calls.Load())
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&count))
	require.Zero(t, count, "original business operation completed")
	require.NoError(t, db.QueryRow(
		t.Context(),
		`SELECT count(*) FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='input'`,
		update.ID,
	).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT count(*) FROM core.registration_ingress WHERE bot_id=999 AND request_key=$1`,
		strconv.FormatInt(update.ID, 10)).Scan(&count))
	require.Equal(t, 1, count)
}

func assertNativeIngressExpiry(t *testing.T, db *pgxpool.Pool, f *sandbox.Fake) {
	t.Helper()
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "uncaptured", Action: "arm", User: 101, HoldSeconds: 1})
	time.Sleep(time.Second + 20*time.Millisecond)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(canceled, http.MethodGet,
		"/lab/registration-ingress?case=uncaptured", nil)
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", os.Getenv("R104_CONTROL_KEY"))
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "failed expiry persistence must not acknowledge")
	var state string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT data #>> '{RegistrationIngress,cases,uncaptured,state}' FROM bot.fake_state`).Scan(&state))
	require.Equal(t, "armed", state)
	w = nativeIngressRequest(t, f, http.MethodGet, "/lab/registration-ingress?case=uncaptured", nil)
	var item ingressPublicReceipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &item))
	require.Equal(t, "expired", item.State, "save failure must roll back memory so the next read persists expiry")
	require.Empty(t, item.Response)
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT data #>> '{RegistrationIngress,cases,uncaptured,state}' FROM bot.fake_state`).Scan(&state))
	require.Equal(t, "expired", state)
}

// The opt-in environment and native control listener are process-wide, so this test is serial.
func newNativeIngressDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	require.NoError(t, store.Seed(t.Context(), db))
	return db
}

func TestRegistrationIngressDurableOriginalAndProductDedup(t *testing.T) {
	db := newNativeIngressDB(t)
	var err error
	t.Setenv("R104_CONTROL_KEY", strings.Repeat("s", 32))
	f, stop := startNativeIngress(t, db)
	assertNativeIngressExpiry(t, db, f)
	stop()
	f, stop = startNativeIngress(t, db)
	w := nativeIngressRequest(t, f, http.MethodGet, "/lab/registration-ingress?case=uncaptured", nil)
	var expired ingressPublicReceipt
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &expired))
	require.Equal(t, "expired", expired.State)
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 10})
	w = nativeIngressRequest(
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
	original := nativeIngressPoll(t, restored, 0)
	require.Equal(t, item.Response, original)
	release.Action = "replay"
	nativeIngressRequest(t, restored, http.MethodPost, "/lab/registration-ingress", release)
	stopRestored()
	restoredAgain, stopAgain := startNativeIngress(t, db)
	require.Equal(t, item.Response, nativeIngressPoll(t, restoredAgain, 0))
	require.Equal(t, "pending", nativeIngressReceipt(t, restoredAgain).Replay)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(canceled, http.MethodPost,
		"/botsynthetic-token/getUpdates", strings.NewReader(`{"offset":`+strconv.FormatInt(update.ID+1, 10)+`}`))
	failed := httptest.NewRecorder()
	restoredAgain.Handler().ServeHTTP(failed, request)
	require.Equal(t, http.StatusServiceUnavailable, failed.Code)
	require.Equal(t, "pending", nativeIngressReceipt(t, restoredAgain).Replay)
	replayed := nativeIngressPoll(t, restoredAgain, update.ID+1)
	require.Equal(t, item.Response, replayed)
	require.Equal(t, "replay_consumed", nativeIngressReceipt(t, restoredAgain).Replay)
	assertNativeIngressProductDedup(t, db, restoredAgain, update, original, replayed)
	stopAgain()
	assertNativeReplayExpiry(t, db)
	restarted, stopRestarted := startNativeIngress(t, db)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, restarted, update.ID+1)))
	stopRestarted()
	for _, captured := range []time.Time{item.ArmedAt.Add(-time.Nanosecond), item.Deadline, item.Deadline.Add(time.Nanosecond)} {
		_, err = db.Exec(
			t.Context(),
			`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,case-a,captured_at}',to_jsonb($1::text))`,
			captured.Format(time.RFC3339Nano),
		)
		require.NoError(t, err)
		_, err = sandbox.New(t.Context(), db, nativeIngressToken)
		require.ErrorContains(t, err, "original response")
	}
	_, err = db.Exec(
		t.Context(),
		`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,case-a,captured_at}',to_jsonb($1::text))`,
		item.ArmedAt.Format(time.RFC3339Nano),
	)
	require.NoError(t, err)
	validBoundary, stopBoundary := startNativeIngress(t, db)
	require.True(t, nativeIngressReceipt(t, validBoundary).CapturedAt.Equal(item.ArmedAt))
	stopBoundary()
	_, err = db.Exec(
		t.Context(),
		`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,case-a,sha256}',to_jsonb('changed'::text))`,
	)
	require.NoError(t, err)
	_, err = sandbox.New(t.Context(), db, nativeIngressToken)
	require.ErrorContains(t, err, "original response")
}

func TestRegistrationIngressReversedFirstDelivery(t *testing.T) {
	db := newNativeIngressDB(t)
	t.Setenv("R104_CONTROL_KEY", strings.Repeat("s", 32))
	f, stop := startNativeIngress(t, db)
	t.Cleanup(stop)
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "first-order", Action: "arm", User: 101, HoldSeconds: 10})
	for _, user := range []int64{101, 202} {
		nativeIngressRequest(t, f, http.MethodPost, "/lab/input",
			map[string]any{"user": user, "text": "competing event-specific request"})
	}
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, f, 0)))
	original := nativeIngressReceiptFor(t, f, "first-order")
	var captured struct {
		Result []json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(original.Response, &captured))
	require.Len(t, captured.Result, 2)
	command := map[string]any{"case": "first-order", "action": "release", "user": 101,
		"sha256": original.SHA256, "order": "reverse"}
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	var delivered struct {
		Result []json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(nativeIngressPoll(t, f, 0), &delivered))
	require.Equal(t, []json.RawMessage{captured.Result[1], captured.Result[0]}, delivered.Result)
	readback := nativeIngressReceiptFor(t, f, "first-order")
	require.Equal(t, "reverse", readback.DeliveryOrder)
	require.Equal(t, original.Response, readback.Response)
	require.Equal(t, original.SHA256, readback.SHA256)
	require.True(t, original.Deadline.Equal(readback.Deadline))
	var last telegram.Update
	require.NoError(t, json.Unmarshal(captured.Result[1], &last))
	command["action"] = "replay"
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	stop()
	restored, stopRestored := startNativeIngress(t, db)
	t.Cleanup(stopRestored)
	require.Equal(t, "reverse", nativeIngressReceiptFor(t, restored, "first-order").DeliveryOrder)
	require.Equal(t, "reverse", nativeIngressReceiptFor(t, restored, "first-order").ReplayOrder)
	require.NoError(t, json.Unmarshal(nativeIngressPoll(t, restored, last.ID+1), &delivered))
	require.Equal(t, []json.RawMessage{captured.Result[1], captured.Result[0]}, delivered.Result)
	require.Equal(t, original.Response, nativeIngressReceiptFor(t, restored, "first-order").Response)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, restored, last.ID+1)))
}

func TestRegistrationIngressOrderChoicesAreImmutable(t *testing.T) {
	db := newNativeIngressDB(t)
	t.Setenv("R104_CONTROL_KEY", strings.Repeat("s", 32))
	f, stop := startNativeIngress(t, db)
	t.Cleanup(stop)
	reject := func(command map[string]any, status int) {
		raw, err := json.Marshal(command)
		require.NoError(t, err)
		r := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/lab/registration-ingress",
			bytes.NewReader(raw),
		)
		r.Header.Set("X-Sandbox", "1")
		r.Header.Set("X-R104-Control", os.Getenv("R104_CONTROL_KEY"))
		w := httptest.NewRecorder()
		f.Handler().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	reject(map[string]any{"case": "choices", "action": "arm", "user": 101, "hold_seconds": 10,
		"order": "original"}, http.StatusConflict)
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "choices", Action: "arm", User: 101, HoldSeconds: 10})
	for _, user := range []int64{101, 202} {
		nativeIngressRequest(
			t,
			f,
			http.MethodPost,
			"/lab/input",
			map[string]any{"user": user, "text": "ordered request"},
		)
	}
	nativeIngressPoll(t, f, 0)
	original := nativeIngressReceiptFor(t, f, "choices")
	command := map[string]any{"case": "choices", "action": "release", "user": 101, "sha256": original.SHA256}
	for _, order := range []any{nil, true, []string{"reverse"}, map[string]string{"order": "reverse"}, "", "unknown"} {
		command["order"] = order
		reject(command, http.StatusBadRequest)
		require.Equal(t, original, nativeIngressReceiptFor(t, f, "choices"))
	}
	command["order"] = "original"
	command["user"] = 202
	reject(command, http.StatusConflict)
	command["user"] = 101
	command["sha256"] = strings.Repeat("a", 64)
	reject(command, http.StatusConflict)
	command["sha256"] = original.SHA256
	require.Equal(t, original, nativeIngressReceiptFor(t, f, "choices"))
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	committed := nativeIngressReceiptFor(t, f, "choices")
	require.Equal(t, "original", committed.DeliveryOrder)
	command["order"] = "reverse"
	reject(command, http.StatusConflict)
	require.Equal(t, committed, nativeIngressReceiptFor(t, f, "choices"))
	command["order"] = "original"
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	require.Equal(t, committed, nativeIngressReceiptFor(t, f, "choices"))
	require.Equal(t, original.Response, nativeIngressPoll(t, f, 0))
	command["action"] = "replay"
	command["order"] = "reverse"
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	replay := nativeIngressReceiptFor(t, f, "choices")
	require.Equal(t, "original", replay.DeliveryOrder)
	require.Equal(t, "reverse", replay.ReplayOrder)
	command["order"] = "original"
	reject(command, http.StatusConflict)
	command["order"] = "reverse"
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	require.Equal(t, replay, nativeIngressReceiptFor(t, f, "choices"))
	var envelope struct {
		Result []json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(original.Response, &envelope))
	var last telegram.Update
	require.NoError(t, json.Unmarshal(envelope.Result[1], &last))
	var delivered struct {
		Result []json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(nativeIngressPoll(t, f, last.ID+1), &delivered))
	require.Equal(t, []json.RawMessage{envelope.Result[1], envelope.Result[0]}, delivered.Result)
	require.Equal(t, original.Response, nativeIngressReceiptFor(t, f, "choices").Response)
	consumed := nativeIngressReceiptFor(t, f, "choices")
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress", command)
	require.Equal(t, consumed, nativeIngressReceiptFor(t, f, "choices"))
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, f, last.ID+1)))
}

func TestRegistrationIngressCapturedCustodyExpiresWithoutPoll(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(strconv.FormatBool(release), func(t *testing.T) {
			db := newNativeIngressDB(t)
			t.Setenv("R104_CONTROL_KEY", strings.Repeat("s", 32))
			f, stop := startNativeIngress(t, db)
			nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
				ingressPublicCommand{Case: "old", Action: "arm", User: 101, HoldSeconds: 1})
			w := nativeIngressRequest(t, f, http.MethodPost, "/lab/input", map[string]any{"user": 101, "text": "older"})
			var old telegram.Update
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &old))
			nativeIngressPoll(t, f, 0)
			original := nativeIngressReceiptFor(t, f, "old")
			if release {
				nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
					ingressPublicCommand{Case: "old", Action: "release", User: 101, SHA256: original.SHA256})
			}
			time.Sleep(time.Until(original.Deadline) + 20*time.Millisecond)
			assertCapturedExpiryRollback(t, db, f, release)
			stop()
			restarted, stopRestarted := startNativeIngress(t, db)
			defer stopRestarted()
			pending := nativeIngressReceiptFor(t, restarted, "old")
			require.Equal(t, "original_pending", pending.State)
			require.Equal(t, original.Response, pending.Response)
			require.Equal(t, original.SHA256, pending.SHA256)
			require.False(t, pending.CustodyFinishedAt.Before(original.Deadline))
			assertCapturedSuccessorOrder(t, restarted, old, original.Response)
		})
	}
}

func assertCapturedExpiryRollback(t *testing.T, db *pgxpool.Pool, f *sandbox.Fake, released bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/lab/registration-ingress?case=old", nil)
	r.Header.Set("X-Sandbox", "1")
	r.Header.Set("X-R104-Control", os.Getenv("R104_CONTROL_KEY"))
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	var state string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT data #>> '{RegistrationIngress,cases,old,state}' FROM bot.fake_state`).Scan(&state))
	expected := "held"
	if released {
		expected = "original_released"
	}
	require.Equal(t, expected, state)
}

func assertCapturedSuccessorOrder(t *testing.T, f *sandbox.Fake, old telegram.Update, original []byte) {
	t.Helper()
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "new", Action: "arm", User: 202, HoldSeconds: 10})
	w := nativeIngressRequest(t, f, http.MethodPost, "/lab/input", map[string]any{"user": 202, "text": "newer"})
	var newer telegram.Update
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &newer))
	require.Greater(t, newer.ID, old.ID)
	require.Equal(t, original, nativeIngressPoll(t, f, 0))
	require.Equal(t, "armed", nativeIngressReceiptFor(t, f, "new").State)
	var unacknowledged struct {
		Result []telegram.Update `json:"result"`
	}
	require.NoError(t, json.Unmarshal(nativeIngressPoll(t, f, 0), &unacknowledged))
	require.Equal(t, []telegram.Update{old, newer}, unacknowledged.Result)
	require.Equal(t, "armed", nativeIngressReceiptFor(t, f, "new").State)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, f, old.ID+1)))
	item := nativeIngressReceiptFor(t, f, "new")
	var canonical bytes.Buffer
	require.NoError(
		t,
		json.NewEncoder(&canonical).Encode(map[string]any{"ok": true, "result": []telegram.Update{newer}}),
	)
	require.Equal(t, canonical.Bytes(), item.Response)
	nativeIngressRequest(t, f, http.MethodPost, "/lab/registration-ingress",
		ingressPublicCommand{Case: "new", Action: "release", User: 202, SHA256: item.SHA256})
	require.Equal(t, item.Response, nativeIngressPoll(t, f, old.ID+1))
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(nativeIngressPoll(t, f, newer.ID+1)))
}
