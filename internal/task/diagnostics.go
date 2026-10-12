package task

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/complynx/shuttle/internal/db"
)

const databaseDiagnosticInterval = 30 * time.Second

// reportDatabaseFailure distinguishes faults from normal routing/lease waits.
// Raw database errors can contain user data. Report only a class and SQLSTATE.
func (s *Service) reportDatabaseFailure(ctx context.Context, id uuid.UUID, operation string, err error) {
	if errors.Is(err, db.ErrRoutingPending) || errors.Is(err, db.ErrLeaseLost) ||
		errors.Is(err, context.Canceled) {
		return
	}
	// One diagnostic per process per interval bounds logs during shared outages.
	s.diagnosticMu.Lock()
	now := time.Now()
	if now.Before(s.nextDatabaseDiagnostic) {
		s.diagnosticMu.Unlock()
		return
	}
	s.nextDatabaseDiagnostic = now.Add(databaseDiagnosticInterval)
	s.diagnosticMu.Unlock()
	class, sqlstate := "database_error", ""
	if failure, ok := errors.AsType[*pgconn.PgError](err); ok {
		sqlstate = failure.Code
	} else if errors.Is(err, context.DeadlineExceeded) {
		class = "timeout"
	} else if errors.Is(err, pgx.ErrNoRows) {
		class = "missing_record"
	}
	s.logger.ErrorContext(ctx, "task database operation failed",
		"operation", operation, "task_id", id, "class", class, "sqlstate", sqlstate)
}
