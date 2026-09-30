package bot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const (
	r33AckOK          = `{"ok":true,"result":true}`
	r33AckRejected    = `{"ok":false,"error_code":400,"description":"query is too old"}`
	r33AckRateLimited = `{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`
	r33AckWarning     = "callback acknowledgement failed"
)

// r33QueryWait only bounds a failure; a published query ends the wait at once.
const r33QueryWait = 5 * time.Second

// r33ControlPolicy is a local telegram.ControlPolicy; the telegram test stub
// lives in another package.
type r33ControlPolicy struct {
	admit   func(context.Context) (delivery.Admission, error)
	observe func(context.Context, delivery.Outcome) (delivery.Outcome, time.Time, error)
}

func (p r33ControlPolicy) Admit(ctx context.Context) (delivery.Admission, error) {
	if p.admit != nil {
		return p.admit(ctx)
	}
	return delivery.Admission{Ready: true}, nil
}

func (p r33ControlPolicy) Observe(ctx context.Context, o delivery.Outcome) (delivery.Outcome, time.Time, error) {
	if p.observe != nil {
		return p.observe(ctx, o)
	}
	return o, time.Now().Add(time.Second), nil
}

func r33ObserveFailure(err error) r33ControlPolicy {
	return r33ControlPolicy{observe: func(context.Context, delivery.Outcome) (delivery.Outcome, time.Time, error) {
		return delivery.Outcome{}, time.Time{}, err
	}}
}

// r33WithTelegram attaches a real telegram.Client whose endpoint answers every
// request with one fixed response, and returns the request count and warnings.
func r33WithTelegram(
	t *testing.T, b *Bot, status int, body string, policy r33ControlPolicy,
) (*atomic.Int32, *bytes.Buffer) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			t.Errorf("unexpected Telegram method %q", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	var logs bytes.Buffer
	b.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	b.TG = telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}
	return &calls, &logs
}

// r33UnreachableDatabaseBot fails at dial, so the driver reports a concrete
// *pgconn.ConnectError: positive SQL provenance from the handler body itself.
func r33UnreachableDatabaseBot(t *testing.T) *Bot {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://bot@fake-postgres/bot?sslmode=disable")
	require.NoError(t, err)
	config.ConnConfig.LookupFunc = func(context.Context, string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("r33 dial refused")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return &Bot{DB: pool}
}

func r33Callback() telegram.Update {
	return telegram.Update{ID: 7, Callback: &telegram.Callback{ID: "callback-1"}}
}

func requireR33NoDomainMarker(t *testing.T, err error) {
	t.Helper()
	requireSanitizedDatabaseError(t, err)
	_, domain := errors.AsType[*core.ProblemError](err)
	require.False(t, domain)
	require.NotErrorIs(t, err, context.Canceled)
}

func TestR33AcknowledgeReturnsControlDatabaseFailure(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		policy r33ControlPolicy
		status int
		body   string
		calls  int32
	}{
		"admission storage": {
			policy: r33ControlPolicy{admit: func(context.Context) (delivery.Admission, error) {
				return delivery.Admission{}, core.DatabaseOperationError(io.EOF)
			}},
			status: http.StatusOK, body: r33AckOK, calls: 0,
		},
		"observe storage after rate limit": {
			policy: r33ObserveFailure(core.DatabaseOperationError(io.EOF)),
			status: http.StatusTooManyRequests, body: r33AckRateLimited, calls: 1,
		},
		"observe domain marker after rate limit": {
			policy: r33ObserveFailure(core.DatabaseFailure(
				&core.ProblemError{Status: http.StatusConflict, Code: "stale_version"},
			)),
			status: http.StatusTooManyRequests, body: r33AckRateLimited, calls: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := &Bot{}
			calls, _ := r33WithTelegram(t, b, tc.status, tc.body, tc.policy)
			requireR33NoDomainMarker(t, b.acknowledge(t.Context(), "callback-1"))
			require.Equal(t, tc.calls, calls.Load())
		})
	}
}

func TestR33AcknowledgeSQLSurvivesCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Caller shutdown begins while the detached control completion is observed.
	policy := r33ControlPolicy{observe: func(
		completion context.Context, _ delivery.Outcome,
	) (delivery.Outcome, time.Time, error) {
		cancel()
		require.NoError(t, completion.Err())
		return delivery.Outcome{}, time.Time{}, errors.Join(core.DatabaseOperationError(io.EOF), ctx.Err())
	}}
	b := &Bot{}
	calls, _ := r33WithTelegram(t, b, http.StatusTooManyRequests, r33AckRateLimited, policy)
	requireR33NoDomainMarker(t, b.acknowledge(ctx, "callback-1"))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, int32(1), calls.Load())
}

func TestR33AcknowledgeOrdinaryFailuresStayBestEffort(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		policy   r33ControlPolicy
		status   int
		body     string
		calls    int32
		canceled bool
		warn     bool
	}{
		"success": {
			status: http.StatusOK, body: r33AckOK, calls: 1,
		},
		"telegram 400": {
			status: http.StatusBadRequest, body: r33AckRejected, calls: 1, warn: true,
		},
		"provider eof": {
			status: http.StatusOK, body: "", calls: 1, warn: true,
		},
		"observe plain eof": {
			policy: r33ObserveFailure(io.EOF),
			status: http.StatusTooManyRequests, body: r33AckRateLimited, calls: 1, warn: true,
		},
		"control cooldown": {
			policy: r33ControlPolicy{admit: func(context.Context) (delivery.Admission, error) {
				return delivery.Admission{Reason: "delivery_cooldown", NotBefore: time.Now().Add(time.Hour)}, nil
			}},
			status: http.StatusOK, body: r33AckOK, calls: 0, warn: true,
		},
		"pure cancellation": {
			policy: r33ControlPolicy{admit: func(ctx context.Context) (delivery.Admission, error) {
				return delivery.Admission{}, core.DatabaseOperationError(ctx.Err())
			}},
			status: http.StatusOK, body: r33AckOK, calls: 0, canceled: true, warn: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			b := &Bot{}
			calls, logs := r33WithTelegram(t, b, tc.status, tc.body, tc.policy)
			require.NoError(t, b.acknowledge(ctx, "callback-1"))
			require.Equal(t, tc.calls, calls.Load())
			require.Equal(t, tc.warn, strings.Contains(logs.String(), r33AckWarning))
		})
	}
}

// Actual deferred handlers: the body fails at its first SQL read, then the
// deferred acknowledgement runs exactly once.
func r33DeferredHandlers() map[string]struct {
	run  func(*Bot, context.Context, incoming, telegram.Update) error
	text string
} {
	return map[string]struct {
		run  func(*Bot, context.Context, incoming, telegram.Update) error
		text string
	}{
		"media": {run: (*Bot).handleMediaCallback, text: mediaPrefix + "token"},
		"food":  {run: (*Bot).handleFood, text: foodPrefix + "token"},
	}
}

// requireR33Query waits for the fake server goroutine to publish the SQL it
// received; the publication is asynchronous to the driver returning an error.
func requireR33Query(t *testing.T, queries <-chan string) {
	t.Helper()
	select {
	case query := <-queries:
		require.NotEmpty(t, query)
	case <-time.After(r33QueryWait):
		require.FailNow(t, "intent SQL was not observed by the fake PostgreSQL server")
	}
}

func TestR33DeferredAcknowledgementSQLDominatesBodyError(t *testing.T) {
	t.Parallel()
	for name, handler := range r33DeferredHandlers() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, queries := sqlBot(t, nil, pgReply{action: pgDrop})
			calls, _ := r33WithTelegram(
				t,
				b,
				http.StatusTooManyRequests,
				r33AckRateLimited,
				r33ObserveFailure(core.DatabaseOperationError(io.EOF)),
			)
			err := handler.run(b, t.Context(), incoming{owner: "owner-1", chat: 1, text: handler.text}, r33Callback())
			requireR33NoDomainMarker(t, err)
			require.Len(t, queries, 1)
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestR33DeferredOrdinaryAcknowledgementKeepsBodyResult(t *testing.T) {
	t.Parallel()
	for name, handler := range r33DeferredHandlers() {
		t.Run(name+" unclassified body error", func(t *testing.T) {
			t.Parallel()
			b, queries := sqlBot(t, nil, pgReply{action: pgDrop})
			calls, _ := r33WithTelegram(t, b, http.StatusBadRequest, r33AckRejected, r33ControlPolicy{})
			err := handler.run(b, t.Context(), incoming{owner: "owner-1", chat: 1, text: handler.text}, r33Callback())
			require.Error(t, err)
			require.False(t, core.IsDatabaseFailure(err))
			require.Len(t, queries, 1)
			require.Equal(t, int32(1), calls.Load())
		})
		for ackName, ack := range map[string]struct {
			status int
			body   string
		}{
			"telegram 400": {status: http.StatusBadRequest, body: r33AckRejected},
			"provider eof": {status: http.StatusOK, body: ""},
		} {
			t.Run(name+" body SQL with "+ackName, func(t *testing.T) {
				t.Parallel()
				b := r33UnreachableDatabaseBot(t)
				calls, _ := r33WithTelegram(t, b, ack.status, ack.body, r33ControlPolicy{})
				err := handler.run(
					b,
					t.Context(),
					incoming{owner: "owner-1", chat: 1, text: handler.text},
					r33Callback(),
				)
				require.True(t, core.IsDatabaseFailure(err))
				require.Equal(t, int32(1), calls.Load())
			})
		}
	}
}

// r33IntentBot wires the fake PostgreSQL pool into the existing delivery
// fixture: valid botIntentTestSettings and the local bot-delivery host path, so
// denyOnboarding's enqueue reaches intent SQL instead of failing on zero
// delivery settings or an unconfigured remote host before any query.
func r33IntentBot(t *testing.T) (*Bot, <-chan string) {
	t.Helper()
	fake, queries := sqlBot(t, nil, pgReply{action: pgDrop})
	b := botDeliveryTestBot(fake.DB)
	return &b, queries
}

func TestR33DenyOnboardingAcknowledgementSQLSkipsIntent(t *testing.T) {
	t.Parallel()
	b, queries := r33IntentBot(t)
	calls, _ := r33WithTelegram(
		t, b, http.StatusTooManyRequests, r33AckRateLimited, r33ObserveFailure(core.DatabaseOperationError(io.EOF)),
	)
	err := b.denyOnboarding(t.Context(), incoming{chat: 1, language: "en"}, r33Callback())
	requireR33NoDomainMarker(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, queries)
}

func TestR33DenyOnboardingOrdinaryAcknowledgementStillQueues(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		update telegram.Update
		calls  int32
	}{
		"callback with telegram 400": {update: r33Callback(), calls: 1},
		"message needs no ack":       {update: telegram.Update{ID: 7, Message: &telegram.Message{}}, calls: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, queries := r33IntentBot(t)
			calls, _ := r33WithTelegram(t, b, http.StatusBadRequest, r33AckRejected, r33ControlPolicy{})
			// The dropped fake connection fails the intent write: the result must be
			// that SQL failure, and the fake server must have received the query.
			err := b.denyOnboarding(t.Context(), incoming{chat: 1, language: "en"}, tc.update)
			require.Error(t, err)
			require.True(t, core.IsDatabaseFailure(err), "enqueue must fail at intent SQL, not before it: %v", err)
			requireR33Query(t, queries)
			require.Equal(t, tc.calls, calls.Load())
		})
	}
}
