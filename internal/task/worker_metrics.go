package task

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"
)

func (s *Service) recordWorkerDuration(ctx context.Context, kind string, err error, duration time.Duration) {
	if s.observer != nil {
		s.observer.RecordWorkerDuration(ctx, kind, workerReturnOutcome(err), duration)
	}
}

// Match River's return classification order without interpreting business state or context races.
func workerReturnOutcome(err error) string {
	if err == nil {
		return "returned"
	}
	if _, ok := errors.AsType[*river.JobSnoozeError](err); ok {
		return "snoozed"
	}
	if _, ok := errors.AsType[*river.JobCancelError](err); ok {
		return "cancelled"
	}
	return "error"
}
