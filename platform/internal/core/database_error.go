package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDatabase identifies a database failure without exposing driver diagnostics.
var ErrDatabase = errors.New("database unavailable")

// ErrDatabaseSerialization permits transaction-level retries without driver details.
var ErrDatabaseSerialization = fmt.Errorf("%w: serialization conflict", ErrDatabase)

// DatabaseOperationError sanitizes an error at a known SQL operation. Call only
// after handling expected SQL-state outcomes; absence and cancellation survive.
func DatabaseOperationError(err error) error {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrDatabaseSerialization) {
		return ErrDatabaseSerialization
	}
	if databaseError, ok := errors.AsType[*pgconn.PgError](err); ok && databaseError.Code == "40001" {
		return ErrDatabaseSerialization
	}
	return ErrDatabase
}

// DatabaseOperationContextError preserves request cancellation at a known SQL
// boundary. A driver-local cancellation with a live request remains a database
// failure. Handle expected SQL-state and absence outcomes before calling it.
func DatabaseOperationContextError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrDatabaseSerialization) {
		return ErrDatabaseSerialization
	}
	if statement, ok := errors.AsType[*pgconn.PgError](err); ok {
		return DatabaseOperationError(statement)
	}
	if errors.Is(err, ErrDatabase) {
		return ErrDatabase
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if cancellation := ctx.Err(); cancellation != nil && errors.Is(err, cancellation) {
		return cancellation
	}
	return ErrDatabase
}

type databaseFailureError struct{ public error }

func (e *databaseFailureError) Error() string                { return e.public.Error() }
func (e *databaseFailureError) Unwrap() error                { return e.public }
func (*databaseFailureError) Is(target error) bool           { return target == ErrDatabase }
func (e *databaseFailureError) MarshalJSON() ([]byte, error) { return json.Marshal(e.public) }

// DatabaseFailure marks an already sanitized error. Never pass a driver error as public.
func DatabaseFailure(public error) error {
	if public == nil || errors.Is(public, context.Canceled) || errors.Is(public, context.DeadlineExceeded) ||
		errors.Is(public, ErrDatabase) {
		return public
	}
	return &databaseFailureError{public: public}
}

// IsDatabaseFailure recognizes preserved provenance and concrete PostgreSQL failures.
// HTTP status, error text and generic transport errors do not establish SQL origin.
func IsDatabaseFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDatabase) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if _, ok := errors.AsType[*pgconn.PgError](err); ok {
		return true
	}
	_, ok := errors.AsType[*pgconn.ConnectError](err)
	return ok
}
