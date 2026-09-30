package passbooking

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestReceiptJSONSQLFailureOrigins(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]func(context.Context, pgx.Tx) error{
		"locked batch": func(ctx context.Context, tx pgx.Tx) error {
			return (&RuntimeBatchState{}).LockRuntimeBatchInTx(ctx, tx)
		},
		"assignment": func(ctx context.Context, tx pgx.Tx) error {
			_, err := assignmentTransitions(ctx, tx, "actor", AdminAssignment{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
				err := run(t.Context(), &readDatabaseTx{err: failure})
				require.ErrorIs(t, err, core.ErrDatabase, "live parent keeps driver-local failure SQL")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			err := run(ctx, &readDatabaseTx{err: context.Canceled})
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, core.IsDatabaseFailure(err))
			err = run(t.Context(), &readDatabaseTx{err: pgx.ErrNoRows})
			if name == "assignment" {
				require.Equal(t, conflict("source_stale"), err)
			} else {
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
		})
	}
}

func TestReceiptJSONLockedBatchDecode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, raw string
		wantError bool
	}{
		{name: "valid", raw: `{"event":"dance","action":"invite","items":[]}`},
		{name: "null", raw: `null`},
		{name: "empty object", raw: `{}`},
		{name: "syntax", raw: `{`, wantError: true},
		{name: "wrong shape", raw: `[]`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			authorized := false
			source := json.RawMessage(`{"authorities":[]}`)
			batch := RuntimeBatchState{Source: source, plan: runtimeBatchPlan{Event: "stale", Action: "stale"}}
			tx := &readDatabaseTx{scans: []readDatabaseScan{
				func(dest ...any) error {
					if err := pgtype.NewMap().
						Scan(pgtype.JSONBOID, pgx.TextFormatCode, []byte(test.raw), dest[0]); err != nil {
						return err
					}
					*dest[1].(*json.RawMessage) = source
					return nil
				},
				func(dest ...any) error {
					authorized = true
					*dest[0].(*int64), *dest[1].(*bool) = 101, true
					return nil
				},
			}}
			err := batch.LockRuntimeBatchInTx(t.Context(), tx)
			if test.wantError {
				require.Error(t, err)
				require.False(t, core.IsDatabaseFailure(err))
				require.False(t, authorized, "invalid JSON must not reach authorization")
				return
			}
			require.NoError(t, err)
			require.True(t, authorized)
			var expected runtimeBatchPlan
			require.NoError(t, json.Unmarshal([]byte(test.raw), &expected))
			require.Equal(t, expected, batch.plan, "null and missing fields must clear pre-lock state")
			require.Equal(t, source, batch.Source)
		})
	}
}

func TestReceiptJSONScalarBatchReceiptFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			tx := &readDatabaseTx{err: failure, scans: []readDatabaseScan{
				func(dest ...any) error {
					return pgtype.NewMap().Scan(pgtype.JSONBOID, pgx.TextFormatCode,
						[]byte(`{"event":"dance","action":"admin_assign","items":[{"telegram_id":101}]}`), dest[0])
				},
				func(dest ...any) error {
					*dest[0].(*int64), *dest[1].(*bool) = 101, true
					return nil
				},
				func(dest ...any) error {
					*dest[0].(*string) = "actor"
					return nil
				},
			}}
			witness, err := BatchOperationWitness("actor", RuntimeBatch{
				Event: "dance", Key: "key", Action: commandAdminAssign, Recipients: []int64{101},
			})
			require.NoError(t, err)
			batch := &RuntimeBatchState{actor: "actor", key: hash([]byte(witness.Key))}
			_, err = (Service{}).witnessBatchReceipt(t.Context(), tx, "actor", witness, batch)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.Empty(t, tx.scans, "fault follows successful plan decode and current admin authorization")
		})
	}
}

func TestReceiptJSONLockedBatchKeepsSourceAndAuthorizationGuards(t *testing.T) {
	t.Parallel()
	for _, sourceMatches := range []bool{false, true} {
		batch := RuntimeBatchState{Source: json.RawMessage(`null`)}
		authCalls := 0
		tx := &readDatabaseTx{scans: []readDatabaseScan{
			func(dest ...any) error {
				if err := pgtype.NewMap().Scan(pgtype.JSONBOID, pgx.TextFormatCode,
					[]byte(`{"event":"dance","action":"invite"}`), dest[0]); err != nil {
					return err
				}
				*dest[1].(*json.RawMessage) = json.RawMessage(`{}`)
				if sourceMatches {
					*dest[1].(*json.RawMessage) = batch.Source
				}
				return nil
			},
			func(dest ...any) error {
				authCalls++
				*dest[0].(*int64), *dest[1].(*bool) = 101, false
				return nil
			},
		}}
		err := batch.LockRuntimeBatchInTx(t.Context(), tx)
		if sourceMatches {
			require.Equal(t, forbidden(), err)
			require.Equal(t, 1, authCalls)
		} else {
			require.Equal(t, conflict("idempotency_conflict"), err)
			require.Zero(t, authCalls)
		}
	}
}

func TestReceiptJSONAssignmentTransitions(t *testing.T) {
	t.Parallel()
	before := Booking{Event: "dance", Owner: "owner", TelegramID: 101, Version: 1}
	after := before
	after.Version = 2
	beforeJSON, err := json.Marshal([]Booking{before})
	require.NoError(t, err)
	afterJSON, err := json.Marshal([]Booking{after})
	require.NoError(t, err)
	for _, test := range []struct {
		name, before, after string
		wantError           bool
		wantPair            bool
	}{
		{name: "valid", before: string(beforeJSON), after: string(afterJSON), wantPair: true},
		{name: "null", before: "null", after: "null"},
		{name: "empty", before: "[]", after: "[]"},
		{name: "before syntax", before: "{", after: string(afterJSON), wantError: true},
		{name: "before shape", before: "{}", after: string(afterJSON), wantError: true},
		{name: "after shape", before: string(beforeJSON), after: "{}", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tx := &readDatabaseTx{scans: []readDatabaseScan{func(dest ...any) error {
				codec := pgtype.NewMap()
				if scanErr := codec.Scan(
					pgtype.JSONBOID,
					pgx.TextFormatCode,
					[]byte(test.before),
					dest[0],
				); scanErr != nil {
					return scanErr
				}
				return codec.Scan(pgtype.JSONBOID, pgx.TextFormatCode, []byte(test.after), dest[1])
			}}}
			result, readErr := assignmentTransitions(t.Context(), tx, "actor", AdminAssignment{Event: "dance"})
			if test.wantError {
				require.Error(t, readErr)
				require.False(t, core.IsDatabaseFailure(readErr))
				require.Nil(t, result)
				return
			}
			require.NoError(t, readErr)
			if test.wantPair {
				require.Equal(t, []BookingTransition{{Before: before, After: after}}, result)
			} else {
				require.Nil(t, result)
			}
		})
	}
}

func TestReceiptJSONWitnessBatchPostgres(t *testing.T) {
	t.Parallel()
	db, err := pgxpool.NewWithConfig(t.Context(), adminDatabasePaymentConfig(t))
	require.NoError(t, err)
	t.Cleanup(db.Close)
	_, err = db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name) VALUES('actor',101,'Synthetic')`)
	require.NoError(t, err)
	witness, err := BatchOperationWitness("actor", RuntimeBatch{
		Event: "dance", Key: "key", Action: commandAdminCancel, Recipients: []int64{101},
	})
	require.NoError(t, err)
	service := Service{DB: db}
	_, err = service.WitnessBatch(t.Context(), "actor", witness)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	for _, test := range []struct {
		name, plan string
		wantJSON   bool
		wantStale  bool
	}{
		{name: "valid", plan: `{"event":"dance","action":"admin_cancel","items":[{"telegram_id":101}]}`},
		{name: "null", plan: `null`, wantStale: true},
		{name: "wrong shape", plan: `[]`, wantJSON: true},
		{name: "wrong field", plan: `{"items":"invalid"}`, wantJSON: true},
		{name: "different event", plan: `{"event":"other","action":"admin_cancel","items":[]}`, wantStale: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			caseWitness, witnessErr := BatchOperationWitness("actor", RuntimeBatch{
				Event: "dance", Key: "key-" + test.name, Action: commandAdminCancel, Recipients: []int64{101},
			})
			require.NoError(t, witnessErr)
			_, writeErr := db.Exec(t.Context(), `INSERT INTO core.pass_admin_batches(actor,key_hash,request_hash,plan)
VALUES('actor',$1,$2,$3::jsonb) ON CONFLICT(actor,key_hash) DO UPDATE SET plan=excluded.plan`,
				hash([]byte(caseWitness.Key)), caseWitness.Digest, test.plan)
			require.NoError(t, writeErr)
			batch, readErr := service.WitnessBatch(t.Context(), "actor", caseWitness)
			if test.wantJSON {
				var typed *json.UnmarshalTypeError
				require.ErrorAs(t, readErr, &typed)
				require.False(t, core.IsDatabaseFailure(readErr))
				require.Nil(t, batch)
				return
			}
			if test.wantStale {
				require.Equal(t, conflict("source_stale"), readErr)
				require.Nil(t, batch)
				return
			}
			require.NoError(t, readErr)
			require.True(t, witnessBatchMatches(caseWitness, batch))
			forged := caseWitness
			forged.Digest = hash([]byte("different"))
			_, readErr = service.WitnessBatch(t.Context(), "actor", forged)
			require.Equal(t, conflict("source_stale"), readErr)
			_, readErr = service.WitnessBatch(t.Context(), "foreign", caseWitness)
			require.Equal(t, forbidden(), readErr)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.WitnessBatch(ctx, "actor", witness)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, core.ErrDatabase)
}
