package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func delayTestFake(t *testing.T, text, mode string) (*Fake, *editDelay) {
	t.Helper()
	journal, err := os.OpenFile(filepath.Join(t.TempDir(), "events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	digest := sha256.Sum256([]byte(text))
	d := &editDelay{
		ctx:         ctx,
		state:       "armed",
		release:     make(chan struct{}),
		journal:     newDelayJournal(ctx, journal),
		dataSlots:   make(chan struct{}, 4),
		invalidated: make(chan struct{}),
		key: strings.Repeat(
			"k",
			24,
		),
		arm: delayArm{ChatID: 101, MessageID: 7, TextSHA256: hex.EncodeToString(digest[:]), Mode: mode},
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-d.journal.ended:
		case <-time.After(time.Second):
			t.Error("journal worker did not stop")
		}
	})
	f := &Fake{
		Token:    "TOKEN",
		delay:    d,
		messages: []telegram.Message{{ID: 7, Chat: telegram.Chat{ID: 101, Type: "private"}, Text: "old"}},
	}
	return f, d
}

func delayTestRequest(f *Fake, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.Handler().ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func delayTestState(d *editDelay) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func delayTestWait(t *testing.T, d *editDelay, state string) {
	t.Helper()
	require.Eventually(t, func() bool { return delayTestState(d) == state }, time.Second, time.Millisecond)
}

func delayTestRelease(t *testing.T, d *editDelay) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/control/release", nil)
	r.Header.Set("X-R104-Control", d.key)
	w := httptest.NewRecorder()
	d.control(w, r)
	require.Equal(t, http.StatusOK, w.Code)
}

// All rejects use the actual Go fake handler, without a mocked upstream decoder.
func TestEditDelayPreservesProviderRejectsAndArm(t *testing.T) {
	t.Parallel()
	valid := `{"chat_id":101,"message_id":7,"text":"replacement"}`
	cases := []struct{ name, method, path, body string }{
		{"get", http.MethodGet, "/botTOKEN/editMessageText", valid},
		{"extra segment", http.MethodPost, "/botTOKEN/extra/editMessageText", valid},
		{"file prefix", http.MethodPost, "/file/botTOKEN/editMessageText", valid},
		{"wrong token", http.MethodPost, "/botWRONG/editMessageText", valid},
		{
			"unknown",
			http.MethodPost,
			"/botTOKEN/editMessageText",
			`{"chat_id":101,"message_id":7,"text":"replacement","unknown":true}`,
		},
		{
			"duplicate wrong type",
			http.MethodPost,
			"/botTOKEN/editMessageText",
			`{"chat_id":101,"message_id":"bad","message_id":7,"text":"replacement"}`,
		},
		{
			"nested unknown",
			http.MethodPost,
			"/botTOKEN/editMessageText",
			`{"chat_id":101,"message_id":7,"text":"replacement","reply_markup":{"unknown":true}}`,
		},
		{"trailing", http.MethodPost, "/botTOKEN/editMessageText", valid + ` {}`},
		{"bom", http.MethodPost, "/botTOKEN/editMessageText", "\xef\xbb\xbf" + valid},
		{
			"nan",
			http.MethodPost,
			"/botTOKEN/editMessageText",
			`{"chat_id":101,"message_id":7,"text":"replacement","message_thread_id":NaN}`,
		},
		{"size", http.MethodPost, "/botTOKEN/editMessageText", valid + strings.Repeat(" ", 65537-len(valid))},
		{"malformed", http.MethodPost, "/botTOKEN/editMessageText", valid[:len(valid)-1]},
		{
			"invalid thread",
			http.MethodPost,
			"/botTOKEN/editMessageText",
			`{"chat_id":101,"message_id":7,"text":"replacement","message_thread_id":1}`,
		},
		{
			"invalid parse mode",
			http.MethodPost,
			"/botTOKEN/editMessageText",
			`{"chat_id":101,"message_id":7,"text":"replacement","parse_mode":"unsupported"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, _ := delayTestFake(t, "replacement", "before_apply")
			base.delay = nil
			candidate, d := delayTestFake(t, "replacement", "before_apply")
			want := delayTestRequest(base, tc.method, tc.path, tc.body)
			got := delayTestRequest(candidate, tc.method, tc.path, tc.body)
			require.Equal(t, want.Code, got.Code)
			require.Equal(t, want.Body.String(), got.Body.String())
			require.Equal(t, "armed", delayTestState(d))
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- delayTestRequest(candidate, http.MethodPost, "/botTOKEN/editMessageText?case=positive", valid)
			}()
			delayTestWait(t, d, "held_before_apply")
			require.Equal(t, 0, candidate.edits)
			delayTestRelease(t, d)
			select {
			case result := <-done:
				require.Equal(t, http.StatusOK, result.Code)
			case <-time.After(time.Second):
				t.Fatal("held handler did not finish")
			}
			require.Equal(t, 1, candidate.edits)
			require.Equal(t, "completed", delayTestState(d))
		})
	}
}

func TestEditDelayUsesGoReplacementCharacterAndTypedWire(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, text, body string }{
		{"surrogate", "\ufffd", `{"chat_id":101,"message_id":7,"text":"\ud800"}`},
		{"invalid utf8", "\ufffd", "{\"chat_id\":101,\"message_id\":7,\"text\":\"\xff\"}"},
		{
			"duplicate null",
			"replacement",
			`{"chat_id":101,"MESSAGE_ID":7,"message_id":null,"text":"replacement","text":null}`,
		},
		{"folded field", "replacement", `{"chat_id":101,"me\u017f\u017fage_id":7,"text":"replacement"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, d := delayTestFake(t, tc.text, "before_apply")
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- delayTestRequest(f, http.MethodPost, "/botTOKEN/editMessageText", tc.body) }()
			delayTestWait(t, d, "held_before_apply")
			delayTestRelease(t, d)
			select {
			case got := <-done:
				require.Equal(t, http.StatusOK, got.Code)
				require.Equal(t, tc.text, f.messages[0].Text)
			case <-time.After(time.Second):
				t.Fatal("matching Go wire did not complete")
			}
		})
	}
}

func TestEditDelaySurvivesCallerCancellationAndSelectsOnce(t *testing.T) {
	t.Parallel()
	f, d := delayTestFake(t, "replacement", "before_apply")
	body := `{"chat_id":101,"message_id":7,"text":"replacement"}`
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/botTOKEN/editMessageText", strings.NewReader(body)).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); f.Handler().ServeHTTP(httptest.NewRecorder(), r) }()
	delayTestWait(t, d, "held_before_apply")
	cancel()
	require.Equal(t, "held_before_apply", delayTestState(d))
	// A concurrent edit remains the ordinary fake call. The one-shot selector cannot hold it.
	other := delayTestRequest(f, http.MethodPost, "/botTOKEN/editMessageText", body)
	require.Equal(t, http.StatusOK, other.Code)
	delayTestRelease(t, d)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation killed provider work")
	}
	require.Equal(t, 2, f.edits)
	require.Equal(t, "completed", delayTestState(d))
	require.Equal(t, "release_accepted", d.events[1].Kind)
	require.Equal(t, "forward_started", d.events[2].Kind)
}

func TestEditDelayLosesOnlyActualMatchedSuccess(t *testing.T) {
	t.Parallel()
	for _, blocked := range []bool{false, true} {
		t.Run(strconv.FormatBool(blocked), func(t *testing.T) {
			t.Parallel()
			f, d := delayTestFake(t, "replacement", "after_apply_loss")
			if blocked {
				f.blocked = map[int64]bool{101: true}
			}
			done := make(chan any, 1)
			response := httptest.NewRecorder()
			go func() {
				defer func() { done <- recover() }()
				f.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/botTOKEN/editMessageText",
					strings.NewReader(`{"chat_id":101,"message_id":7,"text":"replacement"}`)))
			}()
			if blocked {
				select {
				case panicValue := <-done:
					require.Nil(t, panicValue)
				case <-time.After(time.Second):
					t.Fatal("real rejection was held")
				}
				require.Equal(t, http.StatusForbidden, response.Code)
				require.Equal(t, 0, f.edits)
				require.Equal(t, "unresolved_response", delayTestState(d))
			} else {
				delayTestWait(t, d, "applied_response_held")
				require.Equal(t, 1, f.edits)
				require.Empty(t, response.Body.String())
				delayTestRelease(t, d)
				select {
				case panicValue := <-done:
					require.Equal(t, http.ErrAbortHandler, panicValue)
				case <-time.After(time.Second):
					t.Fatal("loss did not finish")
				}
				require.Equal(t, "response_lost", delayTestState(d))
			}
			raw, err := json.Marshal(d.events)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "replacement")
			require.NotContains(t, string(raw), "TOKEN")
		})
	}
}
