package passbooking

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

type registrationTestClock struct {
	now time.Time
	err error
}

type admissionClockTx struct {
	pgx.Tx

	clock    *registrationTestClock
	after    time.Time
	first    time.Time
	inserted []any
	queries  []string
}

func (tx *admissionClockTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.clock.now = tx.after
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (tx *admissionClockTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	return adminDatabaseScanRow(func(dest ...any) error {
		switch {
		case strings.HasPrefix(query, "SELECT clock_timestamp()"):
			*dest[0].(*time.Time) = tx.clock.now
		case strings.HasPrefix(query, "SELECT id,generation"):
			return pgx.ErrNoRows
		case strings.HasPrefix(query, "INSERT INTO core.registration_ingress"):
			*dest[0].(*int64) = 1
			if len(dest) > 1 {
				received := tx.clock.now
				if !tx.first.IsZero() {
					received = tx.first
				}
				*dest[1].(*time.Time) = received
			}
		case strings.HasPrefix(query, "SELECT EXISTS"):
			*dest[0].(*bool) = false
		case strings.HasPrefix(query, "INSERT INTO core.registration_intents"):
			tx.inserted = args
		default:
			return io.ErrUnexpectedEOF
		}
		return nil
	})
}

func TestRegistrationClockCaptureObservesAfterAllocatorWait(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"fresh", "original-reception", "finished", "default"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			clock := &registrationTestClock{now: start}
			tx := &admissionClockTx{clock: clock, after: start.Add(11 * time.Minute)}
			prepared := &PreparedCommand{
				tx:                    tx,
				actor:                 "alice",
				current:               &Booking{},
				command:               Command{Event: "dance"},
				registrationClock:     clock,
				registrationRetention: 10 * time.Minute,
				event: event{
					finishes: start.Add(time.Hour),
					tiers:    []passallocation.Tier{{Start: start.Add(5 * time.Minute)}},
				},
			}
			if scenario == "original-reception" {
				tx.first = start
			}
			if scenario == "finished" {
				prepared.event.finishes = start.Add(5 * time.Minute)
			}
			if scenario == "default" {
				prepared.registrationClock = nil
			}
			_, err := prepared.captureNewAdmission(t.Context(), nil)
			if scenario == "finished" {
				require.Equal(t, conflict("pass_event_finished"), err)
				require.Nil(t, tx.inserted)
				return
			}
			require.NoError(t, err)
			if scenario == "default" {
				require.Nil(t, tx.inserted[8], "SQL insertion-time retention remains the default")
				require.Nil(t, tx.inserted[9], "SQL checked_at remains the default")
				require.Equal(t, "SELECT clock_timestamp()", tx.queries[0])
				require.Len(t, tx.queries, 5, "default query count remains unchanged")
				return
			}
			require.Equal(t, tx.after, *tx.inserted[9].(*time.Time), "checked_at follows allocator lock")
			require.True(t, *tx.inserted[5].(*bool), "sales opened during allocator wait")
			first := tx.after
			if scenario == "original-reception" {
				first = start
			}
			require.Equal(t, first, *tx.inserted[8].(*time.Time), "retention anchors immutable reception")
			require.Len(t, tx.queries, 4, "no additional store query")
		})
	}
}

func (c *registrationTestClock) Now(context.Context) (time.Time, error) { return c.now, c.err }

type clockRow struct {
	now time.Time
	err error
}

func (r clockRow) Scan(dest ...any) error {
	if r.err == nil {
		*dest[0].(*time.Time) = r.now
	}
	return r.err
}

type clockTx struct {
	pgx.Tx

	row     clockRow
	queries []string
}

func (tx *clockTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	return tx.row
}

func TestRegistrationTimeKeepsDefaultSQLAuthority(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tx := &clockTx{row: clockRow{now: now}}
	observed, err := registrationTime(t.Context(), tx, nil)
	require.NoError(t, err)
	require.Equal(t, now, observed)
	require.Equal(t, []string{"SELECT clock_timestamp()"}, tx.queries)
	tx.row.err = io.EOF
	_, err = registrationTime(t.Context(), tx, nil)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotErrorIs(t, err, io.EOF)
}

func TestRegistrationTimeObservesBindingAtEachCall(t *testing.T) {
	t.Parallel()
	tx := &clockTx{}
	clock := &registrationTestClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	first, err := registrationTime(t.Context(), tx, clock)
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Hour)
	second, err := registrationTime(t.Context(), tx, clock)
	require.NoError(t, err)
	require.Equal(t, time.Hour, second.Sub(first))
	require.Empty(t, tx.queries)
	clock.err = io.EOF
	_, err = registrationTime(t.Context(), tx, clock)
	require.ErrorIs(t, err, io.EOF)
	require.False(t, core.IsDatabaseFailure(err))
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = registrationTime(canceled, tx, clock)
	require.ErrorIs(t, err, context.Canceled)
}
