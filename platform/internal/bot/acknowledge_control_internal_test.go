package bot

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/store"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAcknowledgeRequestDeadline(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		controlled bool
		caller     time.Duration
		http       time.Duration
		want       time.Duration
	}{
		"uncontrolled custom client": {},
		"uncontrolled caller":        {caller: 20 * time.Second, want: 20 * time.Second},
		"uncontrolled HTTP timeout":  {http: 15 * time.Second, want: 15 * time.Second},
		"uncontrolled shorter caller": {
			caller: time.Second, http: 15 * time.Second, want: time.Second,
		},
		"controlled budget": {controlled: true, want: callbackAcknowledgementTimeout},
		"controlled longer caller": {
			controlled: true, caller: 20 * time.Second, want: callbackAcknowledgementTimeout,
		},
		"controlled shorter caller": {controlled: true, caller: time.Second, want: time.Second},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			ctx := context.Background()
			if tc.caller != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.caller)
				defer cancel()
			}
			var deadline time.Time
			var hasDeadline bool
			var calls int
			b := &Bot{TG: telegram.Client{Base: "http://synthetic.invalid", HTTP: &http.Client{
				Timeout: tc.http,
				Transport: r34Transport(func(r *http.Request) (*http.Response, error) {
					calls++
					deadline, hasDeadline = r.Context().Deadline()
					return &http.Response{
						StatusCode: http.StatusOK, Header: make(http.Header),
						Body: io.NopCloser(strings.NewReader(r33AckOK)),
					}, nil
				}),
			}}}
			if tc.controlled {
				b.TG.Control = r33ControlPolicy{}
			}
			require.NoError(t, b.acknowledge(ctx, "synthetic-callback"))
			assert.Equal(t, 1, calls)
			assert.Equal(t, tc.want != 0, hasDeadline)
			if tc.want != 0 {
				assert.False(t, deadline.Before(start.Add(tc.want)))
				assert.False(t, deadline.After(time.Now().Add(tc.want)))
			}
		})
	}
}

func acknowledgementDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_profile_callback_ack_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	return db
}

// A real control reservation reproduces getUpdates winning admission before ACK.
func TestAcknowledgeControlReservationReachesProvider(t *testing.T) {
	t.Parallel()
	db := acknowledgementDatabase(t)
	settings := botIntentTestSettings()
	settings.BotInterval = 200 * time.Millisecond
	policy, err := delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	var calls atomic.Int32
	var matched atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"callback_query_id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.ID == "stale-profile-en" &&
			strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			matched.Store(true)
		}
		calls.Add(1)
		_, _ = w.Write([]byte(r33AckOK))
	}))
	t.Cleanup(server.Close)
	b := &Bot{TG: telegram.Client{Base: server.URL, Token: "synthetic", Control: policy}}
	admission, err := policy.Admit(t.Context())
	require.NoError(t, err)
	require.True(t, admission.Ready)
	blocked, err := policy.Admit(t.Context())
	require.NoError(t, err)
	require.False(t, blocked.Ready)
	require.Equal(t, "delivery_cooldown", blocked.Reason)
	require.NoError(t, b.acknowledge(t.Context(), "stale-profile-en"))
	assert.Equal(t, int32(1), calls.Load())
	assert.True(t, matched.Load())
}

func TestAcknowledgeControlOutcomes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		body     string
		status   int
		interval time.Duration
		budget   time.Duration
		reserve  bool
		calls    int32
		warn     bool
	}{
		"definite rate limit": {
			status: http.StatusTooManyRequests,
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":0}}`, calls: 2,
		},
		"malformed rate limit": {
			status: http.StatusTooManyRequests,
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":"invalid"}}`, calls: 1, warn: true,
		},
		"paused bot": {
			status: http.StatusUnauthorized, body: `{"ok":false,"error_code":401}`, calls: 1, warn: true,
		},
		"unknown response": {status: http.StatusOK, calls: 1, warn: true},
		"caller deadline": {
			reserve: true, interval: time.Hour, budget: 50 * time.Millisecond, warn: true,
		},
		"fixed deadline": {reserve: true, interval: time.Hour, warn: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := acknowledgementDatabase(t)
			settings := botIntentTestSettings()
			settings.BotInterval = max(10*time.Millisecond, tc.interval)
			policy, err := delivery.NewControlPacer(db, settings)
			require.NoError(t, err)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				_, _ = w.Write([]byte(r33AckOK))
			}))
			t.Cleanup(server.Close)
			var logs bytes.Buffer
			b := &Bot{
				TG:     telegram.Client{Base: server.URL, Token: "private-synthetic-token", Control: policy},
				Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			}
			if tc.reserve {
				admission, reserveErr := policy.Admit(t.Context())
				require.NoError(t, reserveErr)
				require.True(t, admission.Ready)
			}
			start := time.Now()
			ctx := t.Context()
			if tc.budget != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.budget)
				defer cancel()
			}
			require.NoError(t, b.acknowledge(ctx, "private-synthetic-callback"))
			assert.Equal(t, tc.calls, calls.Load())
			assert.Equal(t, tc.warn, strings.Contains(logs.String(), r33AckWarning))
			assert.NotContains(t, logs.String(), "private-synthetic")
			if tc.reserve {
				limit := callbackAcknowledgementTimeout
				if tc.budget != 0 {
					limit = tc.budget
				}
				assert.Less(t, time.Since(start), limit+time.Second)
				assert.GreaterOrEqual(t, time.Since(start), limit)
			}
		})
	}
}
