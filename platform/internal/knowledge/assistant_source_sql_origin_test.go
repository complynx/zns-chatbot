package knowledge_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// unreachableSourceService fails every connection attempt with dialErr. The
// driver wraps it with private connection details, as a real outage would.
func unreachableSourceService(t *testing.T, dialErr error) (knowledge.Service, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	dials := &atomic.Int32{}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, dialErr
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return knowledge.Service{DB: pool}, dials
}

// workerSourceDigest is a valid 64-character lowercase hex digest (8 x 8).
const workerSourceDigest = "aaaaaaaa" + "aaaaaaaa" + "aaaaaaaa" + "aaaaaaaa" +
	"aaaaaaaa" + "aaaaaaaa" + "aaaaaaaa" + "aaaaaaaa"

func workerSourceOperations(service knowledge.Service) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"configure": func(ctx context.Context) error {
			return service.ConfigureSource(ctx, knowledge.AssistantAbout, workerSourceDigest)
		},
		"replace": func(ctx context.Context) error {
			return service.ReplaceSource(
				ctx, knowledge.AssistantAbout, workerSourceDigest, workerSourceDigest, []string{"body"},
			)
		},
		"failure": func(ctx context.Context) error {
			return service.SourceFailure(ctx, knowledge.AssistantAbout, workerSourceDigest, "fetch_failed")
		},
	}
}

func TestAssistantSourceWorkerSQLOriginsAreSafeDatabaseFailures(t *testing.T) {
	t.Parallel()
	service, _ := unreachableSourceService(t, errors.New("private-dial-canary"))
	for name, operation := range workerSourceOperations(service) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := operation(t.Context())
			require.ErrorIs(t, err, core.ErrDatabase)
			require.True(t, core.IsDatabaseFailure(err))
			for _, private := range []string{"private-user", "private-db", "private-dial-canary"} {
				assert.NotContains(t, err.Error(), private)
			}
			var connect *pgconn.ConnectError
			require.NotErrorAs(t, err, &connect, "driver diagnostics must not survive")
		})
	}
}

func TestAssistantSourceWorkerSQLOriginsKeepCancellationNonFatal(t *testing.T) {
	t.Parallel()
	service, _ := unreachableSourceService(t, context.Canceled)
	for name, operation := range workerSourceOperations(service) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := operation(t.Context())
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, core.IsDatabaseFailure(err))
		})
	}
}

func TestAssistantSourceValidationStaysDomainBeforeSQL(t *testing.T) {
	t.Parallel()
	service, dials := unreachableSourceService(t, errors.New("private-dial-canary"))
	for name, err := range map[string]error{
		"configure slot": service.ConfigureSource(t.Context(), "unknown", ""),
		"replace digest": service.ReplaceSource(t.Context(), knowledge.AssistantAbout, workerSourceDigest, "bad", nil),
		"failure code":   service.SourceFailure(t.Context(), knowledge.AssistantAbout, workerSourceDigest, "private-code"),
	} {
		require.Error(t, err, name)
		require.False(t, core.IsDatabaseFailure(err), name)
	}
	assert.Zero(t, dials.Load(), "invalid input is rejected before any SQL operation")
}
