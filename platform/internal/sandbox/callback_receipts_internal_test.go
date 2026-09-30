package sandbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func callbackRequest(fake *Fake, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	fake.Handler().ServeHTTP(response, request)
	return response
}

func callbackRead(t *testing.T, fake *Fake, user int64, actor string) []callbackReceipt {
	t.Helper()
	response := callbackRequest(
		fake,
		http.MethodGet,
		"/lab/callback-receipts?user="+strconv.FormatInt(user, 10),
		"",
		map[string]string{"X-Sandbox": "1", "X-Sandbox-Actor": actor},
	)
	require.Equal(t, http.StatusOK, response.Code)
	var state struct {
		Receipts []callbackReceipt `json:"receipts"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	return state.Receipts
}

func queuedCallback(t *testing.T, fake *Fake, user int64) telegram.Callback {
	t.Helper()
	sent := callbackRequest(
		fake,
		http.MethodPost,
		"/botTOKEN/sendMessage",
		fmt.Sprintf(`{"chat_id":%d,"text":"private marker"}`, user),
		nil,
	)
	require.Equal(t, http.StatusOK, sent.Code)
	var envelope struct {
		Result telegram.Message `json:"result"`
	}
	require.NoError(t, json.Unmarshal(sent.Body.Bytes(), &envelope))
	input := callbackRequest(
		fake,
		http.MethodPost,
		"/lab/input",
		fmt.Sprintf(`{"user":%d,"data":"private-callback-data","message_id":%d}`, user, envelope.Result.ID),
		map[string]string{"X-Sandbox": "1"},
	)
	require.Equal(t, http.StatusOK, input.Code)
	var update telegram.Update
	require.NoError(t, json.Unmarshal(input.Body.Bytes(), &update))
	require.NotNil(t, update.Callback)
	return *update.Callback
}

func deliveredCallback(t *testing.T, fake *Fake, user int64) telegram.Callback {
	t.Helper()
	callback := queuedCallback(t, fake, user)
	delivered := callbackRequest(fake, http.MethodPost, "/botTOKEN/getUpdates", `{}`, nil)
	require.Equal(t, http.StatusOK, delivered.Code)
	return callback
}

type earlyCallbackWriter struct {
	*httptest.ResponseRecorder

	duringWrite func()
	once        sync.Once
}

func (w *earlyCallbackWriter) Write(body []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(body)
	w.once.Do(w.duringWrite)
	return n, err
}

func TestCallbackReceiptsAnswerBeforeGetUpdatesWriteReturns(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "TOKEN"}
	callback := queuedCallback(t, fake, 101)
	var answered *httptest.ResponseRecorder
	writer := &earlyCallbackWriter{ResponseRecorder: httptest.NewRecorder()}
	writer.duringWrite = func() {
		// A separate handler can answer as soon as batch bytes are published,
		// while the getUpdates response Write is still running.
		responses := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			responses <- callbackRequest(fake, http.MethodPost, "/botTOKEN/answerCallbackQuery",
				fmt.Sprintf(`{"callback_query_id":%q}`, callback.ID), nil)
		}()
		answered = <-responses
	}
	fake.Handler().
		ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/botTOKEN/getUpdates", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusOK, writer.Code)
	require.Contains(t, writer.Body.String(), callback.ID)
	require.NotNil(t, answered)
	require.Equal(t, http.StatusOK, answered.Code)
	require.JSONEq(t, `{"ok":true,"result":true}`, answered.Body.String())
	receipts := callbackRead(t, fake, 101, "alice")
	require.Len(t, receipts, 1)
	require.Equal(t, callback.ID, receipts[0].CallbackID)
}

func TestCallbackReceiptsObserveActualEndpointAndOrigin(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "TOKEN"}
	alice := deliveredCallback(t, fake, 101)
	bob := deliveredCallback(t, fake, 202)
	require.Empty(t, callbackRead(t, fake, 101, "alice"))
	edited := callbackRequest(
		fake,
		http.MethodPost,
		"/botTOKEN/editMessageText",
		fmt.Sprintf(`{"chat_id":101,"message_id":%d,"text":"changed"}`, alice.Message.ID),
		nil,
	)
	require.Equal(t, http.StatusOK, edited.Code)
	require.Empty(t, callbackRead(t, fake, 101, "alice"))
	for _, callback := range []telegram.Callback{alice, bob} {
		answered := callbackRequest(
			fake,
			http.MethodPost,
			"/botTOKEN/answerCallbackQuery",
			fmt.Sprintf(`{"callback_query_id":%q,"text":"not exposed","user_id":999}`, callback.ID),
			nil,
		)
		require.Equal(t, http.StatusOK, answered.Code)
		require.JSONEq(t, `{"ok":true,"result":true}`, answered.Body.String())
	}
	for _, test := range []struct {
		user     int64
		actor    string
		callback telegram.Callback
	}{{101, "alice", alice}, {202, "bob", bob}} {
		receipts := callbackRead(t, fake, test.user, test.actor)
		require.Len(t, receipts, 1)
		receipt := receipts[0]
		require.Equal(t, test.callback.ID, receipt.CallbackID)
		require.Equal(t, test.user, receipt.UserID)
		require.Equal(t, test.callback.Message.ID, receipt.MessageID)
		require.Equal(t, int64(fakeBotID), receipt.BotID)
		require.Equal(t, http.StatusOK, receipt.Status)
		require.True(t, receipt.Result)
		require.False(t, receipt.ObservedAt.IsZero())
	}
}

func TestCallbackReceiptsScopeAndInvalidCalls(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "TOKEN"}
	callback := deliveredCallback(t, fake, 101)
	wrongToken := callbackRequest(
		fake,
		http.MethodPost,
		"/botWRONG/answerCallbackQuery",
		fmt.Sprintf(`{"callback_query_id":%q}`, callback.ID),
		nil,
	)
	require.Equal(t, http.StatusUnauthorized, wrongToken.Code)
	for _, body := range []string{`{}`, `{`, `{"callback_query_id":"unknown"}`, `{"callback_query_id":"` + strings.Repeat("x", callbackIdentifierLimit+1) + `"}`, fmt.Sprintf(`{"callback_query_id":%q} {}`, callback.ID), `{"callback_query_id":123}`, fmt.Sprintf(`{"callback_query_id":%q,"ignored":"%s"}`, callback.ID, strings.Repeat("x", callbackObservationBodyLimit))} {
		response := callbackRequest(fake, http.MethodPost, "/botTOKEN/answerCallbackQuery", body, nil)
		require.Equal(t, http.StatusOK, response.Code)
		require.JSONEq(t, `{"ok":true,"result":true}`, response.Body.String())
	}
	require.Empty(t, callbackRead(t, fake, 101, "alice"))
	validBody := fmt.Sprintf(`{"callback_query_id":%q}`, callback.ID)
	for _, suffix := range []string{"", "!"} {
		oversized := validBody + strings.Repeat(" ", callbackObservationBodyLimit+1) + suffix
		response := callbackRequest(fake, http.MethodPost, "/botTOKEN/answerCallbackQuery", oversized, nil)
		require.Equal(t, http.StatusOK, response.Code)
		require.JSONEq(t, `{"ok":true,"result":true}`, response.Body.String())
		require.Empty(t, callbackRead(t, fake, 101, "alice"))
	}
	boundedBody := validBody + strings.Repeat(" ", callbackObservationBodyLimit-len(validBody))
	oversizedBoundary := callbackRequest(fake, http.MethodPost, "/botTOKEN/answerCallbackQuery", boundedBody+" ", nil)
	require.Equal(t, http.StatusOK, oversizedBoundary.Code)
	require.Empty(t, callbackRead(t, fake, 101, "alice"))
	boundary := callbackRequest(fake, http.MethodPost, "/botTOKEN/answerCallbackQuery", boundedBody, nil)
	require.Equal(t, http.StatusOK, boundary.Code)
	require.Len(t, callbackRead(t, fake, 101, "alice"), 1)
	for _, test := range []struct {
		query, actor string
		status       int
	}{{"101", "bob", 403}, {"101", "", 403}, {"202", "alice", 403}, {"999", "alice", 400}, {"+101", "alice", 400}, {"101&user=202", "alice", 400}} {
		response := callbackRequest(
			fake,
			http.MethodGet,
			"/lab/callback-receipts?user="+test.query,
			"",
			map[string]string{"X-Sandbox": "1", "X-Sandbox-Actor": test.actor},
		)
		require.Equal(t, test.status, response.Code)
		require.NotContains(t, response.Body.String(), callback.ID)
	}
	noOperator := callbackRequest(
		fake,
		http.MethodGet,
		"/lab/callback-receipts?user=101",
		"",
		map[string]string{"X-Sandbox-Actor": "alice"},
	)
	require.Equal(t, http.StatusForbidden, noOperator.Code)
}

func TestCallbackReceiptsBoundedRetentionAndConcurrentCalls(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "TOKEN"}
	callback := deliveredCallback(t, fake, 101)
	var workers sync.WaitGroup
	for range callbackReceiptLimit + 1 {
		workers.Go(func() {
			response := callbackRequest(
				fake,
				http.MethodPost,
				"/botTOKEN/answerCallbackQuery",
				fmt.Sprintf(`{"callback_query_id":%q}`, callback.ID),
				nil,
			)
			if response.Code != http.StatusOK {
				t.Errorf("ack status: %d", response.Code)
			}
		})
	}
	workers.Wait()
	response := callbackRequest(
		fake,
		http.MethodGet,
		"/lab/callback-receipts?user=101",
		"",
		map[string]string{"X-Sandbox": "1", "X-Sandbox-Actor": "alice"},
	)
	require.Equal(t, http.StatusOK, response.Code)
	var state struct {
		Total     uint64            `json:"total_observed"`
		Truncated bool              `json:"truncated"`
		Scope     string            `json:"scope"`
		Receipts  []callbackReceipt `json:"receipts"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	require.Equal(t, uint64(callbackReceiptLimit+1), state.Total)
	require.True(t, state.Truncated)
	require.Len(t, state.Receipts, callbackReceiptLimit)
	require.Equal(t, uint64(2), state.Receipts[0].Sequence)
	require.Equal(t, uint64(callbackReceiptLimit+1), state.Receipts[len(state.Receipts)-1].Sequence)
	require.Equal(t, "current_provider_process_response_generated", state.Scope)
	require.Less(t, response.Body.Len(), 32768)
	require.NotContains(t, response.Body.String(), "private marker")
	require.NotContains(t, response.Body.String(), "private-callback-data")
	require.NotContains(t, response.Body.String(), "TOKEN")
	require.Empty(t, callbackRead(t, fake, 202, "bob"))
	fresh := &Fake{Token: "TOKEN"}
	require.Empty(t, callbackRead(t, fresh, 101, "alice"))
}

func TestCallbackReceiptsDeliveredBindingBound(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "TOKEN"}
	first := deliveredCallback(t, fake, 101)
	for range callbackReceiptLimit {
		deliveredCallback(t, fake, 101)
	}
	response := callbackRequest(
		fake,
		http.MethodPost,
		"/botTOKEN/answerCallbackQuery",
		fmt.Sprintf(`{"callback_query_id":%q}`, first.ID),
		nil,
	)
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, callbackRead(t, fake, 101, "alice"))
	fake.mu.Lock()
	bindings := len(fake.callbackEvidence.Users[0].Bindings)
	truncated := fake.callbackEvidence.Users[0].Truncated
	fake.mu.Unlock()
	require.Equal(t, callbackReceiptLimit, bindings)
	require.True(t, truncated)
}
