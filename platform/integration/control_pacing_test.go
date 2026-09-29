package integration_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestControlPacingSharesPrimaryCooldownAndRestart(t *testing.T) {
	t.Parallel()
	db := database(t)
	settings := queueSettings()
	ref := delivery.Reference{Owner: delivery.Bot, Key: "control-bridge", Effect: "send"}
	queueRegister(t, db, ref, delivery.Destination{Chat: "101"}, delivery.Interactive)
	require.True(t, queueBegin(t, db, ref).Ready)
	queueFinish(t, db, ref, delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 60})
	pacer, err := delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	gate, err := pacer.Admit(t.Context())
	require.NoError(t, err)
	require.False(t, gate.Ready)
	require.Equal(t, "delivery_cooldown", gate.Reason)
	var rowCount int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.delivery_pacing WHERE bot_id=$1`, settings.BotID).
			Scan(&rowCount),
	)
	require.Equal(t, 3, rowCount)
	other := settings
	other.BotID++
	pacer, err = delivery.NewControlPacer(db, other)
	require.NoError(t, err)
	gate, err = pacer.Admit(t.Context())
	require.NoError(t, err)
	require.True(t, gate.Ready)
	_, deadline, err := pacer.Observe(
		t.Context(),
		delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", Missing: true},
	)
	require.NoError(t, err)
	require.True(t, deadline.After(time.Now()))
	reconnected, err := pgxpool.NewWithConfig(t.Context(), db.Config())
	require.NoError(t, err)
	t.Cleanup(reconnected.Close)
	restarted, err := delivery.NewControlPacer(reconnected, other)
	require.NoError(t, err)
	gate, err = restarted.Admit(t.Context())
	require.NoError(t, err)
	require.False(t, gate.Ready)
	require.True(t, deadline.Equal(gate.NotBefore))
	queueTransaction(t, db, func(tx pgx.Tx) {
		admission, admitErr := delivery.Reserve(t.Context(), tx, other, delivery.Destination{Chat: "202"})
		require.NoError(t, admitErr)
		require.False(t, admission.Ready)
	})
}

func TestControlPacingInvalidDelayParksAndCreatesNoChat(t *testing.T) {
	t.Parallel()
	db := database(t)
	settings := queueSettings()
	pacer, err := delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	outcome, _, err := pacer.Observe(
		t.Context(),
		delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: math.MaxInt64},
	)
	require.NoError(t, err)
	require.Equal(t, delivery.Parked, outcome.Kind)
	gate, err := pacer.Admit(t.Context())
	require.NoError(t, err)
	require.False(t, gate.Ready)
	require.Equal(t, "telegram_invalid_cooldown", gate.Reason)
	var scopes []string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT array_agg(chat ORDER BY chat) FROM core.delivery_pacing WHERE bot_id=$1`, settings.BotID).
			Scan(&scopes),
	)
	require.Equal(t, []string{"", "$control"}, scopes)
	_, _, err = pacer.Observe(t.Context(), delivery.Outcome{Kind: delivery.Succeeded, MessageID: 99})
	require.Error(t, err)
	gate, err = pacer.Admit(t.Context())
	require.NoError(t, err)
	require.Equal(t, "telegram_invalid_cooldown", gate.Reason)
}

func TestControlPacingConcurrentPrimaryAndNoTransactionDuringHTTP(t *testing.T) {
	t.Parallel()
	db := database(t)
	settings := queueSettings()
	settings.BotInterval = time.Second
	ref := delivery.Reference{Owner: delivery.Bot, Key: "concurrent-control", Effect: "send"}
	queueRegister(t, db, ref, delivery.Destination{Chat: "303"}, delivery.Interactive)
	pacer, err := delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	type result struct {
		primary bool
		gate    delivery.Admission
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	go func() {
		<-start
		gate, admitErr := pacer.Admit(t.Context())
		results <- result{gate: gate, err: admitErr}
	}()
	go func() {
		<-start
		tx, beginErr := db.Begin(t.Context())
		if beginErr != nil {
			results <- result{err: beginErr}
			return
		}
		defer func() { _ = tx.Rollback(t.Context()) }()
		gate, admitErr := delivery.Begin(t.Context(), tx, settings, ref)
		if admitErr == nil {
			admitErr = tx.Commit(t.Context())
		}
		results <- result{primary: true, gate: gate, err: admitErr}
	}()
	close(start)
	ready := 0
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		if got.primary {
			require.True(t, got.gate.Ready)
		}
		if got.gate.Ready {
			ready++
		}
	}
	require.GreaterOrEqual(t, ready, 1)
	settings.BotID++
	pacer, err = delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	locked := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tx, lockErr := db.Begin(context.Background())
		if lockErr == nil {
			_, lockErr = tx.Exec(
				context.Background(),
				`SELECT 1 FROM core.delivery_pacing WHERE bot_id=$1 AND chat='' FOR UPDATE NOWAIT`,
				settings.BotID,
			)
			_ = tx.Rollback(context.Background())
		}
		locked <- lockErr
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	client := telegram.Client{Base: server.URL, Token: "synthetic", Control: pacer}
	require.NoError(t, client.Call(t.Context(), "getChat", struct{}{}, nil))
	require.NoError(t, <-locked)
}

func TestControlPacingPreparationAllowsPrimary(t *testing.T) {
	t.Parallel()
	db := database(t)
	settings := queueSettings()
	settings.BotInterval = time.Minute
	ref := delivery.Reference{Owner: delivery.Bot, Key: "prepared-document", Effect: "send"}
	queueRegister(t, db, ref, delivery.Destination{Chat: "404"}, delivery.Interactive)
	pacer, err := delivery.NewControlPacer(db, settings)
	require.NoError(t, err)
	lookups, downloads := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			lookups++
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"documents/test.txt","file_size":4}}`))
			return
		}
		downloads++
		_, _ = w.Write([]byte("body"))
	}))
	defer server.Close()
	client := telegram.Client{Base: server.URL, Token: "synthetic", Control: pacer}
	var file telegram.File
	require.NoError(t, client.Call(t.Context(), "getFile", map[string]string{"file_id": "opaque"}, &file))
	body, err := client.DownloadResolved(t.Context(), file)
	require.NoError(t, err)
	require.Equal(t, []byte("body"), body)
	queueTransaction(t, db, func(tx pgx.Tx) {
		gate, beginErr := delivery.Begin(t.Context(), tx, settings, ref)
		require.NoError(t, beginErr)
		require.True(t, gate.Ready, "a successful control prerequisite must not postpone its primary delivery")
	})
	require.Equal(t, 1, lookups)
	require.Equal(t, 1, downloads)
}
