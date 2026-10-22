package loom_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestCreateRunReplaysStableKeyAfterLostReceipt(t *testing.T) {
	t.Parallel()
	count := 0
	effects := 0
	var firstBody string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "/v1/runs", r.URL.Path)
		assert.Equal(t, "operation-1", r.Header.Get("Idempotency-Key"))
		assert.Equal(t, "Bearer service-secret", r.Header.Get("Authorization"))
		assert.Equal(t, "session-1", r.Header.Get("X-Auth-Session-Id"))
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.NotContains(t, body, "auth_session_id")
		assert.Equal(t, map[string]any{"expand_services": []any{"zinusya"}}, body["discovery"])
		encoded, err := json.Marshal(body)
		if !assert.NoError(t, err) {
			return
		}
		count++
		if count == 1 {
			effects++
			firstBody = string(encoded)
			conn, _, hijackErr := w.(http.Hijacker).Hijack()
			if !assert.NoError(t, hijackErr) {
				return
			}
			assert.NoError(t, conn.Close())
			return
		}
		assert.Equal(t, firstBody, string(encoded))
		fmt.Fprint(
			w,
			`{"run_id":"run-1","chat_id":"chat-1","status":"waiting_input","version":2,"question":{"question_id":"q-1","text":"Which event?","choices":[{"id":"event-1","label":"Autumn"}],"allow_text":true,"expires_at":"2030-01-01T00:00:00Z"}}`,
		)
	}))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	auth := loom.Auth{ServiceToken: "service-secret", SessionID: "session-1"}
	input := loom.CreateRunRequest{
		Input:     loom.Input{Text: "hello"},
		Discovery: &loom.Discovery{ExpandServices: []string{"zinusya"}},
	}
	_, err = client.CreateRun(context.Background(), auth, "operation-1", input)
	var failure *loom.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, loom.Unknown, failure.Outcome)
	require.True(t, failure.Retryable)
	require.NotContains(t, err.Error(), "service-secret")
	run, err := client.CreateRun(context.Background(), auth, "operation-1", input)
	require.NoError(t, err)
	require.Equal(t, "run-1", run.ID)
	require.Equal(t, "waiting_input", run.Status)
	require.Equal(t, "q-1", run.Question.ID)
	require.Equal(t, "event-1", run.Question.Choices[0].ID)
	mu.Lock()
	require.Equal(t, 1, effects)
	require.Equal(t, 2, count)
	mu.Unlock()
}
func TestBootstrapInputCancelWire(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/sessions":
			assert.Equal(t, "user-secret", r.Header.Get("X-Subject-Token"))
			fmt.Fprint(w, `{"auth_session_id":"session-1","version":1,"expires_at":"2030-01-01T00:00:00Z"}`)
		case "/v1/runs/run-1/input":
			var body loom.InputRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "q-1", body.QuestionID)
			assert.Equal(t, []string{"event-1"}, body.ChoiceIDs)
			fmt.Fprint(w, `{"run_id":"run-1","status":"queued","version":3}`)
		case "/v1/runs/run-1/cancel":
			fmt.Fprint(w, `{"run_id":"run-1","status":"cancelled","version":4}`)
		default:
			t.Error("unexpected route")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	session, err := client.Bootstrap(
		context.Background(),
		loom.Auth{ServiceToken: "service", SubjectToken: "user-secret"},
	)
	require.NoError(t, err)
	require.Equal(t, "session-1", session.ID)
	auth := loom.Auth{ServiceToken: "service", SessionID: session.ID}
	receipt, err := client.Input(
		context.Background(),
		auth,
		"run-1",
		"input-1",
		loom.InputRequest{Kind: "answer", QuestionID: "q-1", ChoiceIDs: []string{"event-1"}},
	)
	require.NoError(t, err)
	require.Equal(t, "queued", receipt.Status)
	receipt, err = client.Cancel(context.Background(), auth, "run-1", "cancel-1")
	require.NoError(t, err)
	require.Equal(t, "cancelled", receipt.Status)
}
func TestReauthorizationLostReceiptAndBoundedResult(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"lost-reauth", "large-run"} {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc == "lost-reauth" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if !assert.NoError(t, err) {
						return
					}
					assert.NoError(t, conn.Close())
					return
				}
				fmt.Fprint(w, strings.Repeat("x", loom.MaxResponseBytes+1))
			}))
			defer server.Close()
			client, err := loom.New(server.URL, server.Client())
			require.NoError(t, err)
			if tc == "lost-reauth" {
				_, err = client.Reauthorize(
					context.Background(),
					loom.Auth{ServiceToken: "service", SubjectToken: "subject"},
					"session-1",
					1,
				)
			} else {
				_, err = client.GetRun(
					context.Background(),
					loom.Auth{ServiceToken: "service", SessionID: "session-1"},
					"run-1",
				)
			}
			var failure *loom.Error
			require.ErrorAs(t, err, &failure)
			if tc == "lost-reauth" {
				require.Equal(t, loom.Unknown, failure.Outcome)
				require.False(t, failure.Retryable)
			}
		})
	}
}

func TestMalformedSuccessDoesNotConfirmRun(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) }))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	_, err = client.CreateRun(
		context.Background(),
		loom.Auth{ServiceToken: "service", SessionID: "session-1"},
		"operation-1",
		loom.CreateRunRequest{Input: loom.Input{Text: "hello"}},
	)
	var failure *loom.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, loom.Unknown, failure.Outcome)
	require.True(t, failure.Retryable)
}

func TestControlReceiptReconcilesWithSameKey(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"input", "cancel"} {
		for _, tc := range []struct{ name, body string }{
			{"empty", `{}`},
			{"wrong-run", `{"run_id":"another-run","status":"queued","version":2}`},
			{"missing-status", `{"run_id":"run-1","version":2}`},
			{"zero-version", `{"run_id":"run-1","status":"queued","version":0}`},
			{"negative-version", `{"run_id":"run-1","status":"queued","version":-1}`},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/v1/runs/run-1/"+method, r.URL.Path)
					assert.Equal(t, "same-key", r.Header.Get("Idempotency-Key"))
					if calls.Add(1) == 1 {
						fmt.Fprint(w, tc.body)
						return
					}
					fmt.Fprint(w, `{"run_id":"run-1","status":"queued","version":2}`)
				}))
				defer server.Close()
				client, err := loom.New(server.URL, server.Client())
				require.NoError(t, err)
				auth := loom.Auth{ServiceToken: "service", SessionID: "session-1"}
				apply := func() (loom.ControlReceipt, error) {
					if method == "cancel" {
						return client.Cancel(context.Background(), auth, "run-1", "same-key")
					}
					return client.Input(
						context.Background(),
						auth,
						"run-1",
						"same-key",
						loom.InputRequest{Kind: "steering", Text: "continue"},
					)
				}
				_, err = apply()
				var failure *loom.Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, loom.Unknown, failure.Outcome)
				require.True(t, failure.Retryable)
				receipt, err := apply()
				require.NoError(t, err)
				require.Equal(t, "run-1", receipt.ID)
				require.Equal(t, int64(2), receipt.Version)
				require.Equal(t, int32(2), calls.Load())
			})
		}
	}
}

func TestClosedListenerDoesNotConsumeBootstrapAttempt(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client, err := loom.New(endpoint, &http.Client{Timeout: time.Second})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.Bootstrap(ctx, loom.Auth{ServiceToken: "service-secret", SubjectToken: "subject-secret"})
	var failure *loom.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, loom.NotStarted, failure.Outcome)
	require.True(t, failure.Retryable)
	require.NotContains(t, err.Error(), endpoint)
	require.NotContains(t, err.Error(), "secret")
}

func TestInputRecoveryRecognizesOnlyTypedTerminalRejection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		terminal bool
	}{
		{"terminal", http.StatusConflict, `{"error":{"code":"run_terminal","message":"private diagnostic"}}`, true},
		{"other_conflict", http.StatusConflict, `{"error":{"code":"idempotency_conflict"}}`, false},
		{"generic_conflict", http.StatusConflict, ``, false},
		{"wrong_status", http.StatusUnauthorized, `{"error":{"code":"run_terminal"}}`, false},
		{"upstream_uncertainty", http.StatusServiceUnavailable, `{"error":{"code":"run_terminal"}}`, false},
		{"malformed", http.StatusConflict, `{"error":{"code":"run_terminal"}`, false},
		{"trailing_json", http.StatusConflict, `{"error":{"code":"run_terminal"}} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/runs/run-1/input", r.URL.Path)
				assert.Equal(t, "accepted-message-1", r.Header.Get("Idempotency-Key"))
				assert.Equal(t, "subject-secret", r.Header.Get("X-Subject-Token"))
				assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client, err := loom.New(server.URL, server.Client())
			require.NoError(t, err)
			_, err = client.Input(
				context.Background(),
				loom.Auth{ServiceToken: "service-secret", SubjectToken: "subject-secret"},
				"run-1",
				"accepted-message-1",
				loom.InputRequest{Kind: "steering", Text: "accepted text"},
			)
			var failure *loom.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.terminal, failure.Code == loom.CodeRunTerminal)
			require.NotContains(t, err.Error(), "private diagnostic")
			require.NotContains(t, err.Error(), "subject-secret")
			if tc.status == http.StatusServiceUnavailable {
				require.Equal(t, loom.Unknown, failure.Outcome)
			}
		})
	}
}
func TestGetRunPreservesResolvedPromptState(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(
			w,
			`{"run_id":"run","status":"running","question":{"question_id":"q","text":"Pick","allow_text":true,"expires_at":"2026-10-16T10:00:00Z","answer":{"id":"input","kind":"answer","question_id":"q","choice_ids":["one"],"applied":true}},"approval":{"approval_id":"a","text":"Confirm","decision":"approve","expires_at":"2026-10-16T10:00:00Z"}}`,
		)
	}))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	run, err := client.GetRun(t.Context(), loom.Auth{ServiceToken: "service"}, "run")
	require.NoError(t, err)
	require.NotNil(t, run.Question)
	require.NotNil(t, run.Question.Answer)
	require.Equal(t, "answer", run.Question.Answer.Kind)
	require.Equal(t, []string{"one"}, run.Question.Answer.ChoiceIDs)
	require.NotNil(t, run.Approval)
	require.Equal(t, "approve", run.Approval.Decision)
}
