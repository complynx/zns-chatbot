package bot

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const startupPrivateDiagnostic = "private-startup-dial-detail"

// faultedStartupPool fails at the real pgx connection interface with private
// driver diagnostics, before any SQL statement can reach a server.
func faultedStartupPool(t *testing.T) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	dials := &atomic.Int32{}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New(startupPrivateDiagnostic)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, dials
}

func requireSafeDatabaseFailure(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, core.IsDatabaseFailure(err))
	require.EqualError(t, err, core.ErrDatabase.Error())
	require.NotContains(t, err.Error(), "private")
	var driver *pgconn.ConnectError
	require.NotErrorAs(t, err, &driver)
}

func startupMenuServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestStartTelegramPollingCursorDatabaseFaultIsSanitized(t *testing.T) {
	t.Parallel()
	pool, dials := faultedStartupPool(t)
	server := startupMenuServer(t, `{"ok":true,"result":true}`)
	b := Bot{DB: pool, TG: telegram.Client{Base: server.URL, Token: "synthetic"}}
	offset, err := b.startTelegramPolling(t.Context())
	requireSafeDatabaseFailure(t, err)
	require.Zero(t, offset)
	require.Positive(t, dials.Load(), "the fault must come from the actual cursor SQL path")
}

func TestStartTelegramPollingProviderFailureIsNotDatabase(t *testing.T) {
	t.Parallel()
	pool, dials := faultedStartupPool(t)
	server := startupMenuServer(t, `{"ok":false,"error_code":429,"description":"retry"}`)
	b := Bot{DB: pool, TG: telegram.Client{Base: server.URL, Token: "synthetic"}}
	_, err := b.startTelegramPolling(t.Context())
	require.Error(t, err)
	require.ErrorContains(t, err, "register Telegram command menu")
	require.NotErrorIs(t, err, core.ErrDatabase)
	require.False(t, core.IsDatabaseFailure(err))
	require.Zero(t, dials.Load(), "provider failure must stop before cursor SQL")
}

func TestReconciliationDatabaseFaultsAreSanitized(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		run  func(*Bot, context.Context) error
	}{
		{"all", (*Bot).reconcileAllViews},
		{"passes", (*Bot).reconcilePassMenus},
		{"workflow", (*Bot).reconcileViews},
		{"orders", (*Bot).reconcileOrderViews},
		{"profile", (*Bot).reconcileProfileViews},
		{"media", (*Bot).reconcileMediaViews},
		{"knowledge", (*Bot).reconcileKnowledgeViews},
		{"massage", (*Bot).reconcileMassageViews},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pool, dials := faultedStartupPool(t)
			requireSafeDatabaseFailure(t, test.run(&Bot{DB: pool}, t.Context()))
			require.Positive(t, dials.Load(), "the fault must come from the actual reconciliation SQL path")
		})
	}
}

func TestReconcileDatabaseFailureClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"marked boundary", core.DatabaseFailure(errors.New("identity lookup unavailable")), true},
		{"driver statement", &pgconn.PgError{Code: "XX000", Message: "private SQL"}, true},
		{"sanitized", core.ErrDatabase, true},
		{"identity", identity.ErrZitadelIdentity, false},
		{"provider problem", &core.ProblemError{Status: http.StatusInternalServerError, Code: "internal_error"}, false},
		{"domain problem", &core.ProblemError{Status: http.StatusConflict, Code: "conflict"}, false},
		{"transport EOF", io.EOF, false},
		{"validation", errors.New("invalid view"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			failure := reconcileDatabaseFailure(test.err)
			if !test.want {
				require.NoError(t, failure)
				return
			}
			require.ErrorIs(t, failure, core.ErrDatabase)
			require.EqualError(t, failure, core.ErrDatabase.Error())
		})
	}
	require.ErrorIs(t, reconcileDatabaseFailure(errors.Join(core.ErrDatabase, context.Canceled)), core.ErrDatabase)
	require.NoError(t, reconcileDatabaseFailure(context.Canceled), "ordinary cancellation remains nonfatal")
}

func TestMediaRenderReadDatabaseFailuresAreSanitized(t *testing.T) {
	t.Parallel()
	pool, _ := faultedStartupPool(t)
	b := Bot{DB: pool}
	_, err := b.loadMediaIntake(t.Context(), "alice", "media")
	requireSafeDatabaseFailure(t, err)
	_, err = b.loadMediaOutcome(t.Context(), "alice", "media")
	requireSafeDatabaseFailure(t, err)
	_, _, err = b.loadMediaUploadState(t.Context(), "alice", "media")
	requireSafeDatabaseFailure(t, err)
}

type startupLinks struct{ err error }

func (l startupLinks) Telegram(context.Context, int64) (identity.User, error) {
	return identity.User{}, l.err
}

type startupExchange struct{}

func (startupExchange) Exchange(context.Context, string) (string, error) { return "", nil }

func TestReconcilePassMenusPropagatesOnlyDatabaseIdentityFailure(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO bot.pass_views(owner,chat_id,revision,state) VALUES('alice',813123,1,'{"view":"events"}')`,
	)
	require.NoError(t, err)
	for _, test := range []struct {
		name     string
		lookup   error
		database bool
	}{
		{"database", &pgconn.PgError{Code: "XX000", Message: "private identity SQL"}, true},
		{"identity", identity.ErrZitadelIdentity, false},
		{"provider", errors.New("private provider detail"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b := Bot{
				DB:  db,
				API: appclient.Client{Exchange: startupExchange{}, Links: startupLinks{err: test.lookup}},
			}
			reconcileErr := b.reconcilePassMenus(t.Context())
			if !test.database {
				require.NoError(t, reconcileErr, "ordinary identity/provider failure remains a per-view warning")
				return
			}
			require.ErrorIs(t, reconcileErr, core.ErrDatabase)
			require.EqualError(t, reconcileErr, core.ErrDatabase.Error())
			require.NotContains(t, reconcileErr.Error(), "private")
		})
	}
}
