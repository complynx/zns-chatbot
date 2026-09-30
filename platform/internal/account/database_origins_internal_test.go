package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const driverSecret = "driver secret detail"

// scriptedTx answers account SQL in call order. Unscripted pgx.Tx methods
// panic through the nil embedded interface, so extra SQL cannot pass silently.
type scriptedTx struct {
	pgx.Tx

	rows    []scriptedRow
	execErr error
	sql     []string
}

type scriptedRow struct {
	values []any
	err    error
}

func (r scriptedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, target := range dest {
		reflect.ValueOf(target).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

func (t *scriptedTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.sql = append(t.sql, sql)
	if len(t.rows) == 0 {
		return scriptedRow{err: errors.New("unscripted query")}
	}
	row := t.rows[0]
	t.rows = t.rows[1:]
	return row
}

func (t *scriptedTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.sql = append(t.sql, sql)
	return pgconn.NewCommandTag("INSERT 0 1"), t.execErr
}

type databaseFault struct {
	name string
	err  error
	want error
}

func databaseFaults() []databaseFault {
	return []databaseFault{
		{name: "typed PostgreSQL", err: &pgconn.PgError{Code: "08006", Message: driverSecret}, want: core.ErrDatabase},
		{
			name: "serialization",
			err:  &pgconn.PgError{Code: "40001", Message: driverSecret},
			want: core.ErrDatabaseSerialization,
		},
		{name: "untyped EOF", err: fmt.Errorf("%s: %w", driverSecret, io.EOF), want: core.ErrDatabase},
	}
}

func requireDatabaseOrigin(t *testing.T, err, want error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	require.EqualError(t, err, want.Error())
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), driverSecret)
}

func requireProblem(t *testing.T, err error, status int, code string) {
	t.Helper()
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, status, problem.Status)
	require.Equal(t, code, problem.Code)
	require.False(t, core.IsDatabaseFailure(err))
}

func ownerRow(raw, effective string) scriptedRow {
	return scriptedRow{values: []any{raw, effective}}
}

func TestLanguageTransactionMarksSQLOrigins(t *testing.T) {
	t.Parallel()
	for _, fault := range databaseFaults() {
		t.Run(fault.name+"/owner lock", func(t *testing.T) {
			t.Parallel()
			tx := &scriptedTx{rows: []scriptedRow{{err: fault.err}}}
			prepared, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", LanguageChange{Language: "ru"})
			requireDatabaseOrigin(t, err, fault.want)
			require.Nil(t, prepared)
			require.Len(t, tx.sql, 1)
		})
		t.Run(fault.name+"/replay read", func(t *testing.T) {
			t.Parallel()
			tx := &scriptedTx{rows: []scriptedRow{ownerRow("", "en"), {err: fault.err}}}
			input := LanguageChange{Language: "ru", OperationKey: "replay"}
			_, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", input)
			requireDatabaseOrigin(t, err, fault.want)
			require.Len(t, tx.sql, 2)
		})
		t.Run(fault.name+"/update", func(t *testing.T) {
			t.Parallel()
			tx := &scriptedTx{rows: []scriptedRow{ownerRow("", "en")}}
			prepared, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", LanguageChange{Language: "ru"})
			require.NoError(t, err)
			tx.rows = append(tx.rows, scriptedRow{err: fault.err})
			_, err = prepared.Apply(t.Context())
			requireDatabaseOrigin(t, err, fault.want)
			require.Len(t, tx.sql, 2)
		})
		t.Run(fault.name+"/receipt insert", func(t *testing.T) {
			t.Parallel()
			// Initialization over an existing choice skips the separately owned broadcast profile write.
			tx := &scriptedTx{
				rows:    []scriptedRow{ownerRow("ru", "ru"), {err: pgx.ErrNoRows}, {values: []any{"ru"}}},
				execErr: fault.err,
			}
			input := LanguageChange{Language: "ru", Initialize: true, OperationKey: "receipt"}
			prepared, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", input)
			require.NoError(t, err)
			value, err := prepared.Apply(t.Context())
			requireDatabaseOrigin(t, err, fault.want)
			require.Equal(t, "ru", value.Language)
			require.Len(t, tx.sql, 4)
			require.Contains(t, tx.sql[3], "INSERT INTO core.language_operations")
		})
	}
}

func TestLanguageTransactionPreservesNonDatabaseOutcomes(t *testing.T) {
	t.Parallel()
	t.Run("missing owner", func(t *testing.T) {
		t.Parallel()
		tx := &scriptedTx{rows: []scriptedRow{{err: pgx.ErrNoRows}}}
		_, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "ghost", LanguageChange{Language: "ru"})
		requireProblem(t, err, http.StatusNotFound, "user_not_found")
	})
	t.Run("invalid language", func(t *testing.T) {
		t.Parallel()
		tx := &scriptedTx{}
		_, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", LanguageChange{Language: "xx"})
		requireProblem(t, err, http.StatusBadRequest, "invalid_language")
		require.Empty(t, tx.sql)
	})
	t.Run("reused operation key", func(t *testing.T) {
		t.Parallel()
		tx := &scriptedTx{rows: []scriptedRow{ownerRow("en", "en"), {values: []any{"en", false}}}}
		input := LanguageChange{Language: "ru", OperationKey: "reused"}
		_, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", input)
		requireProblem(t, err, http.StatusConflict, "operation_key_reused")
	})
	t.Run("replay keeps current preference", func(t *testing.T) {
		t.Parallel()
		tx := &scriptedTx{rows: []scriptedRow{ownerRow("en", "en"), {values: []any{"ru", false}}}}
		input := LanguageChange{Language: "ru", OperationKey: "replayed"}
		prepared, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", input)
		require.NoError(t, err)
		value, found := prepared.Replay()
		require.True(t, found)
		require.Equal(t, "en", value.Language)
		applied, err := prepared.Apply(t.Context())
		require.NoError(t, err)
		require.Equal(t, value, applied)
		require.Len(t, tx.sql, 2)
	})
	t.Run("cancellation", func(t *testing.T) {
		t.Parallel()
		tx := &scriptedTx{rows: []scriptedRow{{err: fmt.Errorf("lock owner: %w", context.Canceled)}}}
		_, err := Service{}.PrepareLanguageInTx(t.Context(), tx, "alice", LanguageChange{Language: "ru"})
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, core.IsDatabaseFailure(err))
	})
}

// eofPool never reaches PostgreSQL: every connection attempt fails with raw EOF.
func eofPool(t *testing.T) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://account@127.0.0.1:1/account?sslmode=disable")
	require.NoError(t, err)
	dials := &atomic.Int32{}
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("%s: %w", driverSecret, io.EOF)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, dials
}

func validMetadataUpdate() TelegramMetadataUpdate {
	return TelegramMetadataUpdate{Sender: SenderMetadata{ID: 42, FirstName: "Alice", Username: "alice"}, UpdateID: 1}
}

func TestAccountPoolOriginsMarkTransportFailure(t *testing.T) {
	t.Parallel()
	calls := map[string]func(context.Context, Service) error{
		"preferences read": func(ctx context.Context, s Service) error {
			_, err := s.Preferences(ctx, "alice")
			return err
		},
		"language begin": func(ctx context.Context, s Service) error {
			_, err := s.SetLanguageWithOperation(ctx, "alice", "ru", false, "begin")
			return err
		},
		"metadata begin": func(ctx context.Context, s Service) error {
			return s.RefreshTelegramMetadata(ctx, "alice", validMetadataUpdate())
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pool, dials := eofPool(t)
			err := call(t.Context(), Service{DB: pool})
			requireDatabaseOrigin(t, err, core.ErrDatabase)
			require.Positive(t, dials.Load())
		})
	}
}

func TestAccountPoolOriginsPreserveNonDatabaseOutcomes(t *testing.T) {
	t.Parallel()
	t.Run("invalid language before begin", func(t *testing.T) {
		t.Parallel()
		pool, dials := eofPool(t)
		_, err := Service{DB: pool}.SetLanguageWithOperation(t.Context(), "alice", "xx", false, "invalid")
		requireProblem(t, err, http.StatusBadRequest, "invalid_language")
		require.Zero(t, dials.Load())
	})
	t.Run("invalid metadata is a no-op", func(t *testing.T) {
		t.Parallel()
		pool, dials := eofPool(t)
		update := validMetadataUpdate()
		update.Sender.IsBot = true
		require.NoError(t, Service{DB: pool}.RefreshTelegramMetadata(t.Context(), "alice", update))
		require.Zero(t, dials.Load())
	})
	t.Run("cancelled preferences read", func(t *testing.T) {
		t.Parallel()
		pool, _ := eofPool(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := Service{DB: pool}.Preferences(ctx, "alice")
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, core.IsDatabaseFailure(err))
	})
}
