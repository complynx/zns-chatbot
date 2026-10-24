package task

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

func TestWorkerReturnClassificationMatchesRiver(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", want: "returned"},
		{name: "snooze", err: river.JobSnooze(time.Second), want: "snoozed"},
		{name: "wrapped-snooze", err: fmt.Errorf("fixture: %w", river.JobSnooze(time.Second)), want: "snoozed"},
		{name: "cancel-without-cause", err: river.JobCancel(nil), want: "cancelled"},
		{name: "wrapped-cancel", err: fmt.Errorf("fixture: %w", river.JobCancel(context.Canceled)), want: "cancelled"},
		{name: "snooze-before-cancel", err: errors.Join(river.JobCancel(nil), river.JobSnooze(time.Second)), want: "snoozed"},
		{name: "context-cancel", err: context.Canceled, want: "error"},
		{name: "context-deadline", err: context.DeadlineExceeded, want: "error"},
		{name: "ordinary-error", err: errors.New("private-worker-error"), want: "error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, workerReturnOutcome(test.err))
		})
	}
}
