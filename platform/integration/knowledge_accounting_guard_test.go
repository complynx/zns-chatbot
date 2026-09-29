package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

// The wrapper pauses between real reservation and real dispatch. All accounting
// and the blocking policy lock still run in PostgreSQL through credits.Service.
type guardedAccountingBarrier struct {
	credits.Service

	reserved chan struct{}
	proceed  chan struct{}
}

func (r guardedAccountingBarrier) Reserve(ctx context.Context, attempt credits.Attempt) error {
	if err := r.Service.Reserve(ctx, attempt); err != nil {
		return err
	}
	close(r.reserved)
	select {
	case <-r.proceed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestKnowledgeProviderGuardAfterAccountingBlock(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"reserve", "dispatch", "codex-reserve", "codex-dispatch"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			checkKnowledgeAccountingGuard(t, phase)
		})
	}
}

func checkKnowledgeAccountingGuard(t *testing.T, phase string) {
	t.Helper()
	db := database(t)
	service := credits.Service{DB: db, Enforce: true}
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var revoked atomic.Bool
	denied := errors.New("synthetic source retired")
	var recorder credits.Recorder = service
	var locker pgx.Tx
	if strings.HasSuffix(phase, "reserve") {
		locker = lockAssessmentAccounting(t, db)
	}
	barrier := guardedAccountingBarrier{
		Service:  service,
		reserved: make(chan struct{}),
		proceed:  make(chan struct{}),
	}
	if strings.HasSuffix(phase, "dispatch") {
		recorder = barrier
	}
	ctx := credits.WithScope(t.Context(), credits.Scope{Actor: "alice", Payer: "alice", Key: "guard-" + phase})
	var model agent.KnowledgeClassifier = agent.OpenAI{Key: "synthetic", BaseURL: server.URL, HTTP: server.Client(), Accounting: recorder}
	marker := filepath.Join(t.TempDir(), "provider-started")
	if strings.HasPrefix(phase, "codex-") {
		executable, executableErr := os.Executable()
		require.NoError(t, executableErr)
		model = agent.Codex{SyntheticOnly: true, Executable: executable, Accounting: recorder}
	}
	finished := make(chan error, 1)
	go func() {
		_, callErr := model.AssessKnowledge(
			ctx,
			agent.KnowledgeAssessmentInput{Text: marker, BeforeProvider: func(context.Context) error {
				if revoked.Load() {
					return denied
				}
				return nil
			}},
		)
		finished <- callErr
	}()
	if strings.HasSuffix(phase, "dispatch") {
		select {
		case <-barrier.reserved:
		case <-time.After(5 * time.Second):
			t.Fatal("real reservation did not complete")
		}
		locker = lockAssessmentAccounting(t, db)
		close(barrier.proceed)
	}
	pid := locker.Conn().PgConn().PID()
	require.Eventually(t, func() bool {
		var blocked bool
		queryErr := db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid)))`, pid).
			Scan(&blocked)
		return queryErr == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "actual credits policy lock must block Begin")
	revoked.Store(true)
	require.NoError(t, locker.Rollback(t.Context()))
	select {
	case err = <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("provider attempt did not finish after policy unlock")
	}
	require.ErrorIs(t, err, denied)
	assert.Zero(t, calls.Load(), "no actual provider request after source retirement")
	assert.NoFileExists(t, marker, "no actual Codex process after source retirement")
	var state string
	var clean bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state,dispatched_at IS NULL AND usage IS NULL AND cost_nano_usd IS NULL AND settled_at IS NULL FROM credits.attempts WHERE operation_key=$1`, "guard-"+phase).
			Scan(&state, &clean),
	)
	assert.Equal(t, "not_sent", state)
	assert.True(t, clean, "denied dispatch has no fabricated usage, cost or send timestamp")
	usage, usageErr := service.Usage(t.Context(), "alice", "alice")
	require.NoError(t, usageErr)
	assert.Zero(t, usage.HeldNanoUSD)
	assert.Zero(t, usage.UnboundedUnknown)
}

func lockAssessmentAccounting(t *testing.T, db *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), `SELECT pg_advisory_xact_lock(64001,0)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	return tx
}

// The integration binary provides an offline Codex process whose only effect is
// a synthetic marker. Normal integration test startup remains unchanged.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "exec" {
		os.Exit(knowledgeAccountingProcess())
	}
	os.Exit(m.Run())
}

func knowledgeAccountingProcess() int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 1
	}
	_, input, found := strings.Cut(string(data), "Input JSON:\n")
	if !found {
		return 1
	}
	var value agent.KnowledgeAssessmentInput
	if json.Unmarshal([]byte(input), &value) != nil {
		return 1
	}
	if os.WriteFile(value.Text, []byte("synthetic process started"), 0o600) != nil {
		return 1
	}
	return 0
}

func TestKnowledgeCodexAccountingProcessControl(t *testing.T) {
	t.Parallel()
	db := database(t)
	recorder := credits.Service{DB: db}
	executable, err := os.Executable()
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "allowed-provider-started")
	model := agent.Codex{SyntheticOnly: true, Executable: executable, Accounting: recorder}
	_, err = model.AssessKnowledge(
		t.Context(),
		agent.KnowledgeAssessmentInput{Text: marker, BeforeProvider: func(context.Context) error { return nil }},
	)
	require.Error(t, err, "marker-only fixture returns no assessment")
	require.FileExists(t, marker, "positive control proves the actual process fixture starts")
	var state string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT state FROM credits.attempts`).Scan(&state))
	require.Equal(t, "settled", state)
}

func TestCreditsKnownNotSentPreservesAccountingTruth(t *testing.T) {
	t.Parallel()
	db := database(t)
	recorder := credits.Service{DB: db}
	ctx, receipts := credits.WithReceiptCollector(t.Context())
	call, err := credits.Begin(ctx, recorder, "test.assessment", "openai", "synthetic")
	require.NoError(t, err)
	require.Len(t, receipts.IDs(), 1)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.NoError(t, call.NotSent(cancelled), "cleanup survives request cancellation")
	require.NoError(t, call.NotSent(ctx), "confirmed unsent cleanup is idempotent")
	var state string
	var untouched bool
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT state,usage IS NULL AND cost_nano_usd IS NULL AND dispatched_at IS NULL FROM credits.attempts WHERE id=$1`, receipts.IDs()[0]).
			Scan(&state, &untouched),
	)
	require.Equal(t, "not_sent", state)
	require.True(t, untouched)
	require.ErrorIs(t, call.Finish(ctx), credits.ErrAccounting, "unsent cannot masquerade as settled")
	sent, err := credits.Begin(ctx, recorder, "test.sent", "openai", "synthetic")
	require.NoError(t, err)
	require.NoError(t, sent.Finish(ctx))
	require.ErrorIs(t, sent.NotSent(ctx), credits.ErrAccounting, "settled uncertain send cannot be released")
}
