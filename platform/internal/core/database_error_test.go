package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestDatabaseFailurePreservesPublicContract(t *testing.T) {
	t.Parallel()
	public := &core.ProblemError{Status: http.StatusInternalServerError, Code: "internal_error"}
	err := core.DatabaseFailure(public)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, "internal_error")
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Same(t, public, problem)
	encoded, encodeErr := json.Marshal(err)
	require.NoError(t, encodeErr)
	require.JSONEq(t, `{"code":"internal_error"}`, string(encoded))
	require.Same(t, err, core.DatabaseFailure(err))
	var decoded core.ProblemError
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.False(t, core.IsDatabaseFailure(&decoded), "JSON does not convey private provenance")
}

func TestDatabaseFailureClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"statement", &pgconn.PgError{Code: "P0001", Message: "private SQL"}, true},
		{"wrapped statement", fmt.Errorf("query: %w", &pgconn.PgError{Code: "23505"}), true},
		{"connection", &pgconn.ConnectError{}, true},
		{"marked", core.DatabaseFailure(errors.New("lookup unavailable")), true},
		{"server status", &core.ProblemError{Status: 500, Code: "internal_error"}, false},
		{"domain", &core.ProblemError{Status: 409, Code: "conflict"}, false},
		{"missing", pgx.ErrNoRows, false},
		{"text", errors.New("database unavailable"), false},
		{"network", &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}, false},
		{"EOF", io.EOF, false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, core.IsDatabaseFailure(test.err))
		})
	}
}

func TestDatabaseFailurePreservesCancellation(t *testing.T) {
	t.Parallel()
	for _, err := range []error{nil, context.Canceled, context.DeadlineExceeded} {
		require.Equal(t, err, core.DatabaseFailure(err))
	}
}

func TestDatabaseFailureClassificationKeepsJoinedSQLCause(t *testing.T) {
	t.Parallel()
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, joined := range []error{
			errors.Join(core.ErrDatabase, cancellation),
			errors.Join(cancellation, core.ErrDatabase),
		} {
			require.True(t, core.IsDatabaseFailure(joined))
			require.ErrorIs(t, joined, cancellation)
		}
		require.False(t, core.IsDatabaseFailure(cancellation))
	}
}

func TestDatabaseOperationErrorRequiresKnownOrigin(t *testing.T) {
	t.Parallel()
	require.False(t, core.IsDatabaseFailure(io.EOF), "provider EOF has no SQL provenance")
	require.ErrorIs(t, core.DatabaseOperationError(io.EOF), core.ErrDatabase)
	private := &pgconn.PgError{Code: "P0001", Message: "private SQL diagnostic"}
	err := core.DatabaseOperationError(private)
	require.EqualError(t, err, "database unavailable")
	var driver *pgconn.PgError
	require.NotErrorAs(t, err, &driver)
	for _, unchanged := range []error{nil, pgx.ErrNoRows, context.Canceled, context.DeadlineExceeded} {
		require.Equal(t, unchanged, core.DatabaseOperationError(unchanged))
	}
}

func TestDatabaseSerializationKeepsOnlyRetryClassification(t *testing.T) {
	t.Parallel()
	driver := &pgconn.PgError{Code: "40001", Message: "private SQL", Detail: "private row"}
	err := core.DatabaseOperationError(fmt.Errorf("query: %w", driver))
	require.ErrorIs(t, err, core.ErrDatabaseSerialization)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "private")
	var original *pgconn.PgError
	require.NotErrorAs(t, err, &original)
	require.Same(t, err, core.DatabaseOperationError(err))
	require.Same(t, err, core.DatabaseFailure(err))
	require.NotErrorIs(t, core.DatabaseOperationError(io.EOF), core.ErrDatabaseSerialization)
	require.NotErrorIs(t, core.DatabaseOperationError(&pgconn.PgError{Code: "40P01"}), core.ErrDatabaseSerialization)
}

func TestDatabaseCancellationDropsConnectionDiagnostics(t *testing.T) {
	t.Parallel()
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cancellation.Error(), func(t *testing.T) {
			t.Parallel()
			config, err := pgconn.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
			require.NoError(t, err)
			config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
				return nil, cancellation
			}
			_, err = pgconn.ConnectConfig(t.Context(), config)
			require.ErrorIs(t, err, cancellation)
			require.Contains(t, err.Error(), "private-user", "exercise the actual driver wrapper")
			safe := core.DatabaseOperationError(err)
			require.ErrorIs(t, safe, cancellation)
			require.EqualError(t, safe, cancellation.Error())
			require.False(t, core.IsDatabaseFailure(safe))
		})
	}
}

func TestDatabaseOperationContextPreservesSQLAndAbsence(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"nil", nil, nil},
		{"absence", pgx.ErrNoRows, pgx.ErrNoRows},
		{"wrapped absence", fmt.Errorf("absent: %w", pgx.ErrNoRows), pgx.ErrNoRows},
		{"transport after cancellation", io.EOF, core.ErrDatabase},
		{"explicit marker", errors.Join(context.Canceled, core.ErrDatabase), core.ErrDatabase},
		{"serialization marker", errors.Join(context.Canceled, core.ErrDatabaseSerialization), core.ErrDatabaseSerialization},
		{"statement", errors.Join(context.Canceled, &pgconn.PgError{Code: "23505", Message: "private"}), core.ErrDatabase},
		{"serialization statement", errors.Join(context.Canceled, &pgconn.PgError{Code: "40001", Message: "private"}),
			core.ErrDatabaseSerialization},
		{"different cancellation", context.DeadlineExceeded, core.ErrDatabase},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := core.DatabaseOperationContextError(ctx, test.err)
			require.ErrorIs(t, got, test.want)
			if got != nil {
				require.NotContains(t, got.Error(), "private")
			}
		})
	}
}

func TestDatabaseOperationContextDistinguishesLocalCancellation(t *testing.T) {
	t.Parallel()
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cancellation.Error(), func(t *testing.T) {
			t.Parallel()
			driver := databaseContextConnectFailure(t, cancellation)
			parent, cancel := databaseCancelledContext(t.Context(), cancellation)
			defer cancel()
			for _, err := range []error{
				cancellation,
				fmt.Errorf("private query diagnostic: %w", cancellation),
				driver,
				fmt.Errorf("private query diagnostic: %w", driver),
			} {
				local := core.DatabaseOperationContextError(t.Context(), err)
				require.Equal(t, core.ErrDatabase, local)
				require.True(t, core.IsDatabaseFailure(local))
				request := core.DatabaseOperationContextError(parent, err)
				require.Equal(t, cancellation, request)
				require.False(t, core.IsDatabaseFailure(request))
			}
			provider := fmt.Errorf("provider: %w", cancellation)
			require.False(t, core.IsDatabaseFailure(provider), "mixed/provider errors never enter the SQL helper")
		})
	}
}

func databaseCancelledContext(parent context.Context, cancellation error) (context.Context, context.CancelFunc) {
	if errors.Is(cancellation, context.DeadlineExceeded) {
		return context.WithDeadline(parent, time.Time{})
	}
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx, cancel
}

func databaseContextConnectFailure(t *testing.T, cancellation error) error {
	t.Helper()
	config, err := pgconn.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, cancellation
	}
	_, err = pgconn.ConnectConfig(t.Context(), config)
	var driver *pgconn.ConnectError
	require.ErrorAs(t, err, &driver)
	require.ErrorIs(t, err, cancellation)
	require.Contains(t, err.Error(), "private-user")
	return err
}
