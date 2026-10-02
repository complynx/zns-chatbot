package sandbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	for _, raw := range []string{
		`{"case":"invalid","action":"arm","user":101,"hold_seconds":10,"unknown":true}`,
		`{"case":"invalid","action":"arm","user":101,"hold_seconds":10}{}`,
		strings.Repeat(" ", delayControlBodyLimit) + `{}`,
	} {
		r := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/lab/registration-ingress",
			strings.NewReader(raw),
		)
		r.Header.Set("X-Sandbox", "1")
		r.Header.Set("X-R104-Control", f.delay.key)
		w = httptest.NewRecorder()
		f.Handler().ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Nil(t, f.registrationIngress)
	}
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
