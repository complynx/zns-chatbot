package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

type readDatabaseTx struct {
	pgx.Tx

	err   error
	rows  pgx.Rows
	scans []readDatabaseScan
}

type readDatabaseScan func(...any) error

func (scan readDatabaseScan) Scan(dest ...any) error { return scan(dest...) }

func (tx *readDatabaseTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(tx.scans) > 0 {
		scan := tx.scans[0]
		tx.scans = tx.scans[1:]
		return scan
	}
	return readDatabaseScan(func(...any) error { return tx.err })
}

func (tx *readDatabaseTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.rows != nil {
		return tx.rows, nil
	}
	return nil, tx.err
}

type readDatabaseRows struct {
	pgx.Rows

	scan readDatabaseScan
	err  error
	next bool
}

func (rows *readDatabaseRows) Close() {}
func (rows *readDatabaseRows) Next() bool {
	next := rows.next
	rows.next = false
	return next
}
func (rows *readDatabaseRows) Scan(dest ...any) error { return rows.scan(dest...) }
func (rows *readDatabaseRows) Err() error             { return rows.err }

func TestReadDatabaseOrigins(t *testing.T) {
	t.Parallel()
	for _, input := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(input.Error(), func(t *testing.T) {
			t.Parallel()
			ctx := receiptParentContext(t, input)
			tx := &readDatabaseTx{err: input}
			want := input
			if errors.Is(input, io.EOF) {
				want = core.ErrDatabase
			}
			_, err := LockReadAuthorityEvents(ctx, tx, nil)
			require.ErrorIs(t, err, want)
			_, err = lockPaymentReadRole(t.Context(), tx, "actor", "event")
			require.ErrorIs(t, err, want)
			_, err = lockReadAuthority(t.Context(), tx, "actor", 1, ReadAuthority{Kind: ReadOwnedEvent})
			require.ErrorIs(t, err, want)
			_, err = lockReadPaymentQueue(t.Context(), tx, ReadAuthority{PaymentAttempt: "attempt"},
				dbgen.LockReadBookingRow{State: paid, PaymentAttempt: "attempt"})
			require.ErrorIs(t, err, want)
			_, err = lockReadTarget(ctx, tx, ReadAuthority{TargetTelegramID: 1})
			require.ErrorIs(t, err, want)
			_, err = readTierEvent(t.Context(), tx, "actor", "event")
			require.ErrorIs(t, err, want)
			_, err = readTierStatistics(t.Context(), tx, "event")
			require.ErrorIs(t, err, want)
			target := &TakeoverTarget{ReceivingAdmin: "receiver"}
			require.ErrorIs(t, target.readContacts(t.Context(), tx), want)
			tx.rows = &readDatabaseRows{}
			_, err = LockReadAuthorities(ctx, tx, "actor", nil)
			require.ErrorIs(t, err, want, "actor query follows the successful event lock query")
		})
	}
}

func TestReadDatabaseAuthorityAbsence(t *testing.T) {
	t.Parallel()
	tx := &readDatabaseTx{err: pgx.ErrNoRows}
	allowed, err := lockPaymentReadRole(t.Context(), tx, "actor", "event")
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = lockReadAuthority(t.Context(), tx, "actor", 1, ReadAuthority{Kind: ReadOwnedEvent})
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = lockReadPaymentQueue(t.Context(), tx, ReadAuthority{PaymentAttempt: "attempt"},
		dbgen.LockReadBookingRow{State: paid, PaymentAttempt: "attempt"})
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = lockReadTarget(t.Context(), tx, ReadAuthority{TargetTelegramID: 1})
	require.NoError(t, err)
	require.False(t, allowed)
	tx.rows = &readDatabaseRows{}
	_, err = LockReadAuthorities(t.Context(), tx, "actor", nil)
	require.ErrorIs(t, err, pgx.ErrNoRows, "actor absence retains its existing contract")
	_, err = readTierEvent(t.Context(), tx, "actor", "event")
	require.Equal(t, conflict("pass_event_unknown"), err)
}

func TestReadDatabaseTargetBookingFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, pgx.ErrNoRows} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			tx := &readDatabaseTx{err: failure, scans: []readDatabaseScan{func(dest ...any) error {
				*dest[0].(*string) = "target"
				*dest[1].(*bool) = true
				return nil
			}}}
			allowed, err := lockReadTarget(t.Context(), tx, ReadAuthority{TargetTelegramID: 1, Action: CommandTakeover})
			require.False(t, allowed)
			if errors.Is(failure, pgx.ErrNoRows) {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, core.ErrDatabase)
			}
		})
	}
}

func TestReadDatabaseStreams(t *testing.T) {
	t.Parallel()
	for _, scanFailure := range []bool{false, true} {
		rows := &readDatabaseRows{next: scanFailure, err: io.EOF,
			scan: func(...any) error { return io.EOF }}
		_, err := readTierStatistics(t.Context(), &readDatabaseTx{rows: rows}, "event")
		require.ErrorIs(t, err, core.ErrDatabase)
		rows.next = scanFailure
		_, err = LockReadAuthorityEvents(t.Context(), &readDatabaseTx{rows: rows}, nil)
		require.ErrorIs(t, err, core.ErrDatabase)
	}
}

func TestReadDatabaseValidationAndJSON(t *testing.T) {
	t.Parallel()
	_, err := (Service{}).OwnsEventBookings(t.Context(), "actor", nil)
	require.Equal(t, invalid(), err)
	_, err = (Service{}).PaymentQueue(t.Context(), "actor", "event", "bad")
	require.Equal(t, invalid(), err)
	_, err = (Service{}).PaymentHistoryPage(t.Context(), "actor", "event", "bad")
	require.False(t, core.IsDatabaseFailure(err))
	require.Error(t, err)
	_, err = LockReadAuthorityEvents(t.Context(), nil, []ReadAuthority{{}})
	require.Equal(t, invalid(), err)
	err = checkTierResult(TierStatus{Tiers: []passallocation.Tier{
		{Start: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)},
	}})
	var encoding *json.MarshalerError
	require.ErrorAs(t, err, &encoding)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestReadDatabaseTierStreamAndBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		count int
		rows  readDatabaseRows
		want  error
	}{
		{name: "scan", rows: readDatabaseRows{next: true, scan: func(...any) error { return io.EOF }},
			want: core.ErrDatabase},
		{name: "iteration", rows: readDatabaseRows{err: io.EOF}, want: core.ErrDatabase},
		{name: "position", rows: readDatabaseRows{next: true, scan: func(dest ...any) error {
			*dest[0].(*int) = 1
			return nil
		}}, want: conflict("pass_tiers_invalid")},
		{name: "limit", count: core.ReadResourceBytes/minimumTierBytes + 1,
			want: core.ReadProblem("read_result_limit")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ok := readDatabaseScan(func(...any) error { return nil })
			tx := &readDatabaseTx{rows: &test.rows, scans: []readDatabaseScan{ok, ok, ok, func(dest ...any) error {
				*dest[0].(*int) = test.count
				return nil
			}}}
			_, err := readTierEvent(t.Context(), tx, "actor", "event")
			require.Equal(t, test.want, err)
		})
	}
}

func TestReadDatabaseServiceOrigins(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	service := Service{DB: db}
	_, err = service.PaymentHistoryPage(t.Context(), "actor", "event", "")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = service.PaymentQueue(t.Context(), "actor", "event", "")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = service.PaymentQuote(t.Context(), "actor", "event")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = service.TakeoverTarget(t.Context(), "actor", "event", 1)
	require.ErrorIs(t, err, core.ErrDatabase)
}
