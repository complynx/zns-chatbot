package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/store"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func ingressRequest(t *testing.T, f *Fake, method, path string, body any, authorized bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	r := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(raw))
	r.Header.Set("X-Sandbox", "1")
	if authorized && f.delay != nil {
		r.Header.Set("X-R104-Control", f.delay.key)
	}
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, r)
	return w
}

func ingressCommand(t *testing.T, f *Fake, request registrationIngressRequest, status int) {
	t.Helper()
	w := ingressRequest(t, f, http.MethodPost, "/lab/registration-ingress", request, true)
	require.Equal(t, status, w.Code, w.Body.String())
}

func ingressInput(t *testing.T, f *Fake, user int64, text string) telegram.Update {
	t.Helper()
	w := ingressRequest(t, f, http.MethodPost, "/lab/input", map[string]any{"user": user, "text": text}, false)
	require.Equal(t, http.StatusOK, w.Code)
	var update telegram.Update
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &update))
	return update
}

func ingressPoll(t *testing.T, f *Fake, offset int64) []byte {
	t.Helper()
	w := ingressRequest(
		t,
		f,
		http.MethodPost,
		"/botsynthetic-token/getUpdates",
		map[string]int64{"offset": offset},
		false,
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w.Body.Bytes()
}

func TestRegistrationIngressControlGuards(t *testing.T) {
	t.Parallel()
	f, err := New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	w := ingressRequest(t, f, http.MethodGet, "/lab/registration-ingress?case=case-a", nil, false)
	require.Equal(t, http.StatusNotFound, w.Code)
	fixture := newModelControlFixture(t)
	f = fixture.fake
	w = ingressRequest(t, f, http.MethodPost, "/lab/registration-ingress", map[string]any{}, false)
	require.Equal(t, http.StatusForbidden, w.Code)
	for _, request := range []registrationIngressRequest{
		{Case: "case-a", Action: "arm", User: 404, HoldSeconds: 10},
		{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 11},
		{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 0},
		{Case: "missing", Action: "replay", User: 101, SHA256: "not-original"},
	} {
		ingressCommand(t, f, request, http.StatusConflict)
	}
	request := registrationIngressRequest{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 10}
	ingressCommand(t, f, request, http.StatusOK)
	ingressCommand(t, f, request, http.StatusConflict)
	request.Case = "concurrent"
	ingressCommand(t, f, request, http.StatusConflict)
	require.Len(t, f.registrationIngress.Cases, 1)
	require.Empty(t, f.updates)
	require.Zero(t, f.next)
}

func TestRegistrationIngressExactBatchAndOneShotReplay(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t).fake
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 10},
		http.StatusOK,
	)
	first := ingressInput(t, f, 101, "event-specific application intent")
	second := ingressInput(t, f, 202, "other actor enqueues normally")
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(ingressPoll(t, f, 0)))
	item := f.registrationIngress.Cases["case-a"]
	var expected bytes.Buffer
	require.NoError(
		t,
		json.NewEncoder(&expected).Encode(map[string]any{"ok": true, "result": []telegram.Update{first, second}}),
	)
	require.Equal(t, expected.Bytes(), item.Response)
	digest := sha256.Sum256(expected.Bytes())
	require.Equal(t, hex.EncodeToString(digest[:]), item.SHA256)
	require.Equal(t, "held", item.State)
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "case-a", Action: "release", User: 202, SHA256: item.SHA256},
		http.StatusConflict,
	)
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "case-a", Action: "release", User: 101, SHA256: "changed"},
		http.StatusConflict,
	)
	release := registrationIngressRequest{Case: "case-a", Action: "release", User: 101, SHA256: item.SHA256}
	ingressCommand(t, f, release, http.StatusOK)
	require.Equal(t, expected.Bytes(), ingressPoll(t, f, 0))
	ingressCommand(t, f, release, http.StatusOK)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(ingressPoll(t, f, second.ID+1)))
	beforeNext, beforeMessages := f.next, len(f.messages)
	replay := release
	replay.Action = "replay"
	ingressCommand(t, f, replay, http.StatusOK)
	ingressCommand(t, f, replay, http.StatusOK)
	require.Equal(t, expected.Bytes(), ingressPoll(t, f, second.ID+1))
	ingressCommand(t, f, replay, http.StatusOK)
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(ingressPoll(t, f, second.ID+1)))
	require.Equal(t, beforeNext, f.next)
	require.Len(t, f.messages, beforeMessages)
	require.Empty(t, f.updates)
	require.Equal(t, ingressReplayConsumed, f.registrationIngress.Cases["case-a"].Replay)
}

func TestRegistrationIngressReceiptValidationAndBounds(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t).fake
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 10},
		http.StatusOK,
	)
	ingressInput(t, f, 101, "original")
	ingressPoll(t, f, 0)
	original := f.registrationIngress
	corrupt := f.cloneIngressControl()
	item := corrupt.Cases["case-a"]
	item.Response = []byte("changed original")
	corrupt.Cases["case-a"] = item
	require.ErrorContains(t, validateRegistrationIngress(corrupt), "original response")
	require.NoError(t, validateRegistrationIngress(original))
	for i := range ingressControlCases + 1 {
		corrupt.Cases[strconv.Itoa(i)] = item
	}
	require.ErrorContains(t, validateRegistrationIngress(corrupt), "limit")
	item = original.Cases["case-a"]
	item.ArmedAt = time.Now().Add(-2 * time.Second)
	item.Deadline = item.ArmedAt.Add(time.Second)
	original.Cases["case-a"] = item
	require.Equal(t, item.Response, ingressPoll(t, f, 0), "finite provider hold expires without changing business time")
}

func TestRegistrationIngressDurableOriginalAndProductDedup(t *testing.T) {
	t.Parallel()
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
	f, err := New(t.Context(), db, "synthetic-token")
	require.NoError(t, err)
	f.delay = newModelControlFixture(t).fake.delay
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "case-a", Action: "arm", User: 101, HoldSeconds: 10},
		http.StatusOK,
	)
	update := ingressInput(t, f, 101, "specific event request")
	ingressPoll(t, f, 0)
	item := f.registrationIngress.Cases["case-a"]
	restored, err := New(t.Context(), db, "synthetic-token")
	require.NoError(t, err)
	restored.delay = f.delay
	reloaded := restored.registrationIngress.Cases["case-a"]
	require.True(t, item.Deadline.Equal(reloaded.Deadline))
	require.True(t, item.ArmedAt.Equal(reloaded.ArmedAt))
	require.True(t, item.CapturedAt.Equal(reloaded.CapturedAt))
	item.Deadline, item.ArmedAt, item.CapturedAt = reloaded.Deadline, reloaded.ArmedAt, reloaded.CapturedAt
	require.Equal(t, item, reloaded)
	release := registrationIngressRequest{Case: "case-a", Action: "release", User: 101, SHA256: item.SHA256}
	ingressCommand(t, restored, release, http.StatusOK)
	require.Equal(t, item.Response, ingressPoll(t, restored, 0))
	ref := registrationingress.Reference{BotID: 999, UpdateID: update.ID}
	for range 2 {
		tx, txErr := db.Begin(t.Context())
		require.NoError(t, txErr)
		require.NoError(t, registrationingress.SaveTelegram(t.Context(), tx, ref, 101))
		require.NoError(t, tx.Commit(t.Context()))
	}
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.registration_ingress WHERE bot_id=999 AND request_key=$1`, strconv.FormatInt(update.ID, 10)).
			Scan(&count),
	)
	require.Equal(t, 1, count)
	ingressPoll(t, restored, update.ID+1)
	release.Action = "replay"
	ingressCommand(t, restored, release, http.StatusOK)
	restoredAgain, err := New(t.Context(), db, "synthetic-token")
	require.NoError(t, err)
	restoredAgain.delay = f.delay
	require.Equal(t, item.Response, ingressPoll(t, restoredAgain, update.ID+1))
	restarted, err := New(t.Context(), db, "synthetic-token")
	require.NoError(t, err)
	restarted.delay = f.delay
	require.JSONEq(t, `{"ok":true,"result":[]}`, string(ingressPoll(t, restarted, update.ID+1)))
	_, err = db.Exec(
		t.Context(),
		`UPDATE bot.fake_state SET data=jsonb_set(data,'{RegistrationIngress,cases,case-a,sha256}',to_jsonb('changed'::text))`,
	)
	require.NoError(t, err)
	_, err = New(t.Context(), db, "synthetic-token")
	require.ErrorContains(t, err, "original response")
}

func TestRegistrationIngressNativeCallbackAndExpiredArm(t *testing.T) {
	t.Parallel()
	f := newModelControlFixture(t).fake
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "expired", Action: "arm", User: 101, HoldSeconds: 1},
		http.StatusOK,
	)
	item := f.registrationIngress.Cases["expired"]
	item.ArmedAt = time.Now().Add(-2 * time.Second)
	item.Deadline = item.ArmedAt.Add(time.Second)
	f.registrationIngress.Cases["expired"] = item
	ingressPoll(t, f, 0)
	require.Equal(t, "expired", f.registrationIngress.Cases["expired"].State)
	w := ingressRequest(t, f, http.MethodPost, "/botsynthetic-token/sendMessage", map[string]any{
		"chat_id": 101, "text": "saved event card", "reply_markup": map[string]any{
			"inline_keyboard": [][]map[string]string{
				{{"text": "Original", "callback_data": "saved-event-token"}},
			},
		}}, false)
	require.Equal(t, http.StatusOK, w.Code)
	var sent struct {
		Result telegram.Message `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sent))
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "native", Action: "arm", User: 101, HoldSeconds: 10},
		http.StatusOK,
	)
	w = ingressRequest(t, f, http.MethodPost, "/lab/input", map[string]any{
		"user": 101, "data": "saved-event-token", "message_id": sent.Result.ID}, false)
	require.Equal(t, http.StatusOK, w.Code)
	var original telegram.Update
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &original))
	require.NotNil(t, original.Callback)
	require.Equal(t, "saved-event-token", original.Callback.Data)
	ingressPoll(t, f, 0)
	item = f.registrationIngress.Cases["native"]
	var expected bytes.Buffer
	require.NoError(
		t,
		json.NewEncoder(&expected).Encode(map[string]any{"ok": true, "result": []telegram.Update{original}}),
	)
	require.Equal(t, expected.Bytes(), item.Response)
	ingressCommand(
		t,
		f,
		registrationIngressRequest{Case: "native", Action: "release", User: 101, SHA256: item.SHA256},
		http.StatusOK,
	)
	require.Equal(t, expected.Bytes(), ingressPoll(t, f, 0))
}
