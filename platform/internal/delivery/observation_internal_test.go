package delivery

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type observationDB struct {
	rows  pgx.Rows
	err   error
	calls int
}

func (d *observationDB) Query(ctx context.Context, _ string, args ...any) (pgx.Rows, error) {
	d.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("observation requires a bounded deadline")
	}
	if len(args) != 1 {
		panic("observation requires only bot identity")
	}
	return d.rows, d.err
}

type observationRows struct {
	pgx.Rows

	item      QueueObservation
	remaining int
	err       error
	scanErr   error
	closed    bool
}

func (r *observationRows) Next() bool {
	if r.remaining == 0 {
		return false
	}
	r.remaining--
	return true
}
func (r *observationRows) Err() error { return r.err }
func (r *observationRows) Close()     { r.closed = true }
func (r *observationRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dest[0].(*Owner), *dest[1].(*Class), *dest[2].(*Kind) = r.item.Owner, r.item.Class, r.item.State
	*dest[3].(*int64), *dest[4].(*int64), *dest[5].(*float64) = r.item.Count, r.item.UnknownAge, r.item.OldestAgeSeconds
	*dest[6].(*int64), *dest[7].(*int64), *dest[8].(*int64) = r.item.Delayed, r.item.Paused, r.item.UnboundedDeadline
	*dest[9].(*float64), *dest[10].(*float64) = r.item.NextAttemptSeconds, r.item.MaximumWaitSeconds
	return nil
}

func TestQueueObservationSQLFailureProvenance(t *testing.T) {
	t.Parallel()
	for _, source := range []error{errors.New("private driver diagnostic"), &pgconn.PgError{Code: "40001", Message: "private SQL"}} {
		for _, phase := range []string{"query", "scan", "iteration"} {
			t.Run(phase+source.Error(), func(t *testing.T) {
				t.Parallel()
				rows := &observationRows{
					remaining: 1,
					item:      QueueObservation{Owner: Orders, Class: Interactive, State: Deferred},
				}
				db := &observationDB{rows: rows}
				switch phase {
				case "query":
					db.err = source
				case "scan":
					rows.scanErr = source
				case "iteration":
					rows.err = source
				}
				items, err := QueueObservations(t.Context(), db, 1)
				require.Nil(t, items)
				require.ErrorIs(t, err, core.ErrDatabase)
				require.NotContains(t, err.Error(), "private")
				if phase != "query" {
					require.True(t, rows.closed)
				}
				var statement *pgconn.PgError
				if errors.As(source, &statement) && statement.Code == "40001" {
					require.ErrorIs(t, err, core.ErrDatabaseSerialization)
				}
			})
		}
	}
}

func TestQueueObservationCardinalityAndDimensions(t *testing.T) {
	t.Parallel()
	good := QueueObservation{
		Owner:              Orders,
		Class:              Interactive,
		State:              Deferred,
		Count:              2,
		UnknownAge:         1,
		OldestAgeSeconds:   60,
		Delayed:            1,
		NextAttemptSeconds: 30,
		MaximumWaitSeconds: 30,
	}
	rows := &observationRows{remaining: 1, item: good}
	items, err := QueueObservations(t.Context(), &observationDB{rows: rows}, 1)
	require.NoError(t, err)
	require.Equal(t, []QueueObservation{good}, items)
	require.True(t, rows.closed)
	for _, mutate := range []func(*QueueObservation){
		func(o *QueueObservation) { o.Owner = "private-owner" }, func(o *QueueObservation) { o.Class = "private-class" },
		func(o *QueueObservation) { o.State = Succeeded }, func(o *QueueObservation) { o.UnknownAge = 3 },
		func(o *QueueObservation) { o.OldestAgeSeconds = math.NaN() }, func(o *QueueObservation) { o.NextAttemptSeconds = math.Inf(1) },
	} {
		bad := good
		mutate(&bad)
		require.False(t, bad.Valid())
	}
	_, err = QueueObservations(t.Context(), &observationDB{rows: &observationRows{remaining: 71, item: good}}, 1)
	require.ErrorIs(t, err, ErrQueueState)
	db := &observationDB{}
	_, err = QueueObservations(t.Context(), db, 0)
	require.ErrorIs(t, err, ErrSettings)
	require.Zero(t, db.calls)
}

func TestQueueObservationCancellationIsNotDriverCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := QueueObservations(ctx, &observationDB{err: context.Canceled}, 1)
	require.ErrorIs(t, err, context.Canceled)
	_, err = QueueObservations(t.Context(), &observationDB{err: context.Canceled}, 1)
	require.ErrorIs(t, err, core.ErrDatabase)
}
