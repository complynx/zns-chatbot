package passbooking

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Existing cancellation matrices use an actually cancelled request context.
func receiptParentContext(t *testing.T, failure error) context.Context {
	t.Helper()
	ctx := t.Context()
	if errors.Is(failure, context.Canceled) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	if errors.Is(failure, context.DeadlineExceeded) {
		expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		cancel()
		return expired
	}
	return ctx
}

func TestRetiredReceiptSQLCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, parentCancelled := range []bool{false, true} {
			name := failure.Error() + "/driver"
			if parentCancelled {
				name = failure.Error() + "/parent"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, want := t.Context(), core.ErrDatabase
				if parentCancelled {
					ctx, want = receiptParentContext(t, failure), failure
				}
				for boundary, run := range receiptSQLBoundaries(t) {
					t.Run(boundary, func(t *testing.T) {
						t.Parallel()
						err := run(ctx, failure)
						require.Equal(t, want, err)
						require.Equal(t, !parentCancelled, core.IsDatabaseFailure(err))
					})
				}
			})
		}
	}
}

func receiptSQLBoundaries(t *testing.T) map[string]func(context.Context, error) error {
	t.Helper()
	boundaries := map[string]func(context.Context, error) error{
		"mutation event": func(ctx context.Context, failure error) error {
			return LockMutationEvents(ctx, originFailureTx{err: failure}, []string{"dance"})
		},
		"authority target": func(ctx context.Context, failure error) error {
			_, err := OperationReadAuthorities(
				ctx, originFailureTx{err: failure}, "admin", "dance", commandAdminAssign, []string{"owner"},
			)
			return err
		},
		"read events query": func(ctx context.Context, failure error) error {
			_, err := LockReadAuthorityEvents(ctx, originFailureTx{err: failure}, nil)
			return err
		},
		"read actor": func(ctx context.Context, failure error) error {
			tx := &readDatabaseTx{err: failure, rows: &readDatabaseRows{}}
			_, err := LockReadAuthorities(ctx, tx, "owner", nil)
			return err
		},
		"read target": func(ctx context.Context, failure error) error {
			_, err := lockReadTarget(ctx, originFailureTx{err: failure}, ReadAuthority{TargetTelegramID: 1})
			return err
		},
		"target booking": func(ctx context.Context, failure error) error {
			tx := &readDatabaseTx{err: failure, scans: []readDatabaseScan{func(dest ...any) error {
				*dest[0].(*string) = "owner"
				*dest[1].(*bool) = true
				return nil
			}}}
			_, err := lockReadTarget(ctx, tx, ReadAuthority{TargetTelegramID: 1, Action: CommandTakeover})
			require.Empty(t, tx.scans)
			return err
		},
		"batch item receipt": func(ctx context.Context, failure error) error {
			batch := &RuntimeBatchState{
				key: "key", actor: "admin", plan: runtimeBatchPlan{Action: commandAdminCancel, Event: "dance"},
			}
			item := RuntimeBatchItem{
				Assignment: AdminAssignment{Event: "dance", Key: "batch-key-0"},
				Outcome:    AdminBatchOutcome{Key: "batch-key-0"},
			}
			_, err := batch.operationItemReceipt(ctx, originFailureTx{err: failure}, 0, item)
			return err
		},
	}
	maps.Copy(boundaries, receiptAuthorizationBoundaries(t))
	for _, scan := range []bool{false, true} {
		name := "read events terminal"
		if scan {
			name = "read events scan"
		}
		boundaries[name] = func(ctx context.Context, failure error) error {
			rows := &readDatabaseRows{next: scan, err: failure, scan: func(...any) error { return failure }}
			_, err := LockReadAuthorityEvents(ctx, &readDatabaseTx{rows: rows}, nil)
			return err
		}
	}
	return boundaries
}

// Event reads have two streams separated by a scalar validation query.
type receiptEventTx struct {
	readDatabaseTx

	t       *testing.T
	queries []receiptEventQuery
}

type receiptEventQuery struct {
	rows pgx.Rows
	err  error
}

func (tx *receiptEventTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	require.NotEmpty(tx.t, tx.queries, "unexpected event query after failure")
	query := tx.queries[0]
	tx.queries = tx.queries[1:]
	return query.rows, query.err
}

func TestRetiredReceiptEventSQLCancellation(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, parentCancelled := range []bool{false, true} {
			name := failure.Error() + "/driver"
			if parentCancelled {
				name = failure.Error() + "/parent"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, want := t.Context(), core.ErrDatabase
				if parentCancelled {
					ctx, want = receiptParentContext(t, failure), failure
				}
				for _, stage := range []string{
					"event", "tiers query", "tiers terminal", "positions", "admins query", "admins scan", "admins terminal",
				} {
					t.Run(stage, func(t *testing.T) {
						t.Parallel()
						tx := receiptEventFailureTx(t, stage, failure)
						_, err := readEvent(ctx, tx, "dance")
						require.Equal(t, want, err)
						require.Equal(t, !parentCancelled, core.IsDatabaseFailure(err))
						require.Empty(t, tx.scans)
						require.Empty(t, tx.queries)
					})
				}
			})
		}
	}
}

func receiptAuthorizationBoundaries(t *testing.T) map[string]func(context.Context, error) error {
	t.Helper()
	boundaries := map[string]func(context.Context, error) error{}
	for _, stage := range []string{"actor", "proof", "admin"} {
		boundaries["authorize "+stage] = func(ctx context.Context, failure error) error {
			tx := &readDatabaseTx{err: failure}
			action := "solo"
			if stage != "actor" {
				tx.scans = []readDatabaseScan{func(...any) error { return nil }}
				action = commandAdminAssign
				if stage == "proof" {
					action = commandProofAccept
				}
			}
			_, err := authorize(ctx, tx, "admin", action, "dance")
			require.Empty(t, tx.scans)
			return err
		}
	}
	for name, run := range map[string]func(context.Context, pgx.Tx, string, string) error{
		"takeover": authorizeTakeover, "cancel": authorizeBatchCancel,
	} {
		for _, fallback := range []bool{false, true} {
			stage := name + "/primary"
			if fallback {
				stage = name + "/fallback"
			}
			boundaries[stage] = func(ctx context.Context, failure error) error {
				tx := &adminDatabaseTx{rowErrors: []error{failure}}
				if fallback {
					tx.rowErrors = []error{pgx.ErrNoRows, failure}
				}
				err := run(ctx, tx, "admin", "dance")
				require.Empty(t, tx.rowErrors)
				return err
			}
		}
	}
	return boundaries
}

func receiptEventFailureTx(t *testing.T, stage string, failure error) *receiptEventTx {
	t.Helper()
	ok := readDatabaseScan(func(...any) error { return nil })
	tx := &receiptEventTx{t: t}
	tx.err = failure
	if stage != "event" {
		tx.scans = []readDatabaseScan{ok}
		switch stage {
		case "tiers query":
			tx.queries = []receiptEventQuery{{err: failure}}
		case "tiers terminal":
			tx.queries = []receiptEventQuery{{rows: &readDatabaseRows{err: failure}}}
		default:
			tx.queries = []receiptEventQuery{{rows: &readDatabaseRows{}}}
		}
	}
	if stage == "admins query" || stage == "admins scan" || stage == "admins terminal" {
		tx.scans = append(tx.scans, ok)
		query := receiptEventQuery{err: failure}
		if stage != "admins query" {
			query = receiptEventQuery{rows: &readDatabaseRows{next: true, err: failure,
				scan: func(...any) error {
					if stage == "admins scan" {
						return failure
					}
					return nil
				}}}
		}
		tx.queries = append(tx.queries, query)
	}
	return tx
}
