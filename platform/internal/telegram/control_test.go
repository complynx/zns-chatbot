package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type controlPolicy struct {
	admit   func(context.Context) (delivery.Admission, error)
	observe func(context.Context, delivery.Outcome) (delivery.Outcome, time.Time, error)
}

func (p controlPolicy) Admit(ctx context.Context) (delivery.Admission, error) {
	if p.admit != nil {
		return p.admit(ctx)
	}
	return delivery.Admission{Ready: true}, nil
}
func (p controlPolicy) Observe(ctx context.Context, o delivery.Outcome) (delivery.Outcome, time.Time, error) {
	if p.observe != nil {
		return p.observe(ctx, o)
	}
	return o, time.Now(), nil
}

func TestControlAdmissionScope(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"getMe", "getUpdates", "getFile", "getChat", "setMyCommands", "setChatMenuButton", "answerCallbackQuery", "sendMessage", "editMessageText", "sendDocument"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			calls, admissions := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			}))
			defer server.Close()
			blocked := method != "sendMessage" && method != "editMessageText" && method != "sendDocument"
			client := telegram.Client{
				Base:  server.URL,
				Token: "synthetic",
				Control: controlPolicy{admit: func(context.Context) (delivery.Admission, error) {
					admissions++
					return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Hour)}, nil
				}},
			}
			err := client.Call(t.Context(), method, struct{}{}, nil)
			if blocked {
				var deferred *telegram.ControlError
				require.ErrorAs(t, err, &deferred)
				require.Equal(t, 1, admissions)
				require.Zero(t, calls)
			} else {
				require.NoError(t, err)
				require.Zero(t, admissions)
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestControlRecordsStructuredAndMalformedRejection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		status     int
		kind       delivery.Kind
		missing    bool
	}{
		{name: "valid", body: `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`, status: 200, kind: delivery.Deferred},
		{name: "missing", body: `{"ok":false,"error_code":429}`, status: 429, kind: delivery.Deferred, missing: true},
		{name: "negative", body: `{"ok":false,"error_code":429,"parameters":{"retry_after":-1}}`, status: 429, kind: delivery.Parked},
		{name: "noninteger", body: `{"ok":false,"error_code":429,"parameters":{"retry_after":"later"}}`, status: 429, kind: delivery.Parked},
		{name: "malformed", body: `not json`, status: 429, kind: delivery.Parked},
		{name: "contradictory", body: `{"ok":true,"result":true}`, status: 429, kind: delivery.Parked},
		{name: "credential", body: `{"ok":false,"error_code":401}`, status: 401, kind: delivery.Paused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			var observed []delivery.Outcome
			policy := controlPolicy{
				observe: func(ctx context.Context, o delivery.Outcome) (delivery.Outcome, time.Time, error) {
					require.NoError(t, ctx.Err())
					_, bounded := ctx.Deadline()
					require.True(t, bounded)
					observed = append(observed, o)
					return o, time.Now().Add(time.Second), nil
				},
			}
			client := telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}
			var deferred *telegram.ControlError
			require.ErrorAs(t, client.Call(t.Context(), "getChat", struct{}{}, nil), &deferred)
			require.Len(t, observed, 1)
			require.Equal(t, tc.kind, observed[0].Kind)
			require.Equal(t, tc.missing, observed[0].Missing)
		})
	}
}

func TestControlStartupRetryAndIdentity(t *testing.T) {
	t.Parallel()
	calls, admissions := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":4242,"is_bot":true}}`))
	}))
	defer server.Close()
	policy := controlPolicy{admit: func(context.Context) (delivery.Admission, error) {
		admissions++
		if admissions == 1 {
			return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Millisecond)}, nil
		}
		return delivery.Admission{Ready: true}, nil
	}}
	client := telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}
	require.NoError(
		t,
		telegram.RetryControl(t.Context(), func(ctx context.Context) error { return client.VerifyBot(ctx, 4242) }),
	)
	require.Equal(t, 3, admissions)
	require.Equal(t, 2, calls)
}

func TestControlRetryDoesNotRepeatUnknownPauseOrStorageFailure(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"unknown", "pause", "storage"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if mode == "unknown" {
					_, _ = w.Write([]byte(`broken`))
					return
				}
				_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`))
			}))
			defer server.Close()
			policy := controlPolicy{
				observe: func(_ context.Context, _ delivery.Outcome) (delivery.Outcome, time.Time, error) {
					if mode == "storage" {
						return delivery.Outcome{}, time.Time{}, errors.New("storage unavailable")
					}
					return delivery.Outcome{Kind: delivery.Parked, Reason: "telegram_invalid_cooldown"}, time.Now(), nil
				},
			}
			client := telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}
			require.Error(
				t,
				telegram.RetryControl(
					t.Context(),
					func(ctx context.Context) error { return client.Call(ctx, "getMe", struct{}{}, nil) },
				),
			)
			require.Equal(t, 1, calls)
		})
	}
}

func TestControlRetryCancellationAndCallbackSingleAttempt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	client := telegram.Client{Control: controlPolicy{admit: func(context.Context) (delivery.Admission, error) {
		return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Hour)}, nil
	}}}
	require.ErrorIs(
		t,
		telegram.RetryControl(
			ctx,
			func(ctx context.Context) error { return client.Call(ctx, "getMe", struct{}{}, nil) },
		),
		context.DeadlineExceeded,
	)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`))
	}))
	defer server.Close()
	client = telegram.Client{Base: server.URL, Token: "synthetic", Control: controlPolicy{}}
	require.Error(t, client.Call(t.Context(), "answerCallbackQuery", struct{}{}, nil))
	require.Equal(t, 1, calls)
}

func TestDownloadResolvedReusesMetadataAndRecordsCooldown(t *testing.T) {
	t.Parallel()
	for _, rateLimited := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rate limited"}[rateLimited], func(t *testing.T) {
			t.Parallel()
			admissions, observations, lookups := 0, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					lookups++
					_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"documents/test.txt","file_size":4}}`))
					return
				}
				if rateLimited {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":12}}`))
					return
				}
				_, _ = w.Write([]byte("body"))
			}))
			defer server.Close()
			policy := controlPolicy{admit: func(context.Context) (delivery.Admission, error) {
				admissions++
				return delivery.Admission{Ready: true}, nil
			}, observe: func(_ context.Context, o delivery.Outcome) (delivery.Outcome, time.Time, error) {
				observations++
				require.Equal(t, int64(12), o.RetryAfter)
				return o, time.Now().Add(time.Second), nil
			}}
			client := telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}
			var file telegram.File
			require.NoError(t, client.Call(t.Context(), "getFile", map[string]string{"file_id": "opaque"}, &file))
			body, err := client.DownloadResolved(t.Context(), file)
			if rateLimited {
				var deferred *telegram.ControlError
				require.ErrorAs(t, err, &deferred)
				require.Equal(t, 1, observations)
			} else {
				require.NoError(t, err)
				require.Equal(t, []byte("body"), body)
				require.Zero(t, observations)
			}
			require.Equal(t, 1, admissions)
			require.Equal(t, 1, lookups)
			_, err = client.DownloadResolved(t.Context(), telegram.File{Path: "../bad"})
			require.ErrorIs(t, err, telegram.ErrInvalidDocument)
		})
	}
}

func TestControlAdmissionStorageFailureNeverDispatches(t *testing.T) {
	t.Parallel()
	storageErr := errors.New("pacing storage unavailable")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client := telegram.Client{
		Base:  server.URL,
		Token: "synthetic",
		Control: controlPolicy{
			admit: func(context.Context) (delivery.Admission, error) { return delivery.Admission{}, storageErr },
		},
	}
	require.ErrorIs(t, client.Call(t.Context(), "getFile", struct{}{}, nil), storageErr)
	require.Zero(t, calls)
}

func TestControlResolveChatRefreshPreservesHealthyAlias(t *testing.T) {
	t.Parallel()
	var admissions, wireCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wireCalls.Add(1)
		var request struct {
			Chat string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Chat == "@broken" {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":-100123}}`))
	}))
	defer server.Close()
	policy := controlPolicy{admit: func(context.Context) (delivery.Admission, error) {
		if admissions.Add(1) <= 2 {
			return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Millisecond)}, nil
		}
		return delivery.Admission{Ready: true}, nil
	}}
	client := telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}
	bindings := &destination.Bindings{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.ErrorIs(
		t,
		bindings.Refresh(ctx, client, []string{"@broken", "@healthy"}, time.Minute),
		destination.ErrUnavailable,
	)
	chat, err := bindings.Lookup("@healthy")
	require.NoError(t, err)
	require.Equal(t, "-100123", chat)
	require.Equal(t, int32(4), admissions.Load())
	require.Equal(t, int32(2), wireCalls.Load())
}

func TestControlResolveChatUnknownOnceAndBoundedWait(t *testing.T) {
	t.Parallel()
	var wireCalls atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { wireCalls.Add(1); _, _ = w.Write([]byte(`broken`)) },
		),
	)
	defer server.Close()
	client := telegram.Client{Base: server.URL, Token: "synthetic", Control: controlPolicy{}}
	_, err := client.ResolveChat(t.Context(), "@unknown")
	require.Error(t, err)
	require.Equal(t, int32(1), wireCalls.Load())
	client.Control = controlPolicy{admit: func(context.Context) (delivery.Admission, error) {
		return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Hour)}, nil
	}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	_, err = client.ResolveChat(ctx, "@waiting")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), wireCalls.Load())
}
