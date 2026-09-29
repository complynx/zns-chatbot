package runtimeapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdmissionRolesHaveFixedOrderedKeys(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		role Role
		want []int32
	}{
		{API, []int32{1}},
		{Bot, []int32{2}},
		{App, []int32{1, 2}},
	} {
		keys, err := roleKeys(test.role)
		require.NoError(t, err)
		require.Equal(t, test.want, keys)
	}
}

func TestAdmissionDoneDoesNotClaimCleanupJoin(t *testing.T) {
	t.Parallel()
	monitor, stop := context.WithCancel(t.Context())
	defer stop()
	admission := &Admission{stop: stop, done: make(chan struct{}), joined: make(chan struct{}), err: ErrLost}
	close(admission.done)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err := admission.Close(canceled)
	require.ErrorIs(t, err, ErrClose)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, ErrLost)
	require.ErrorIs(t, monitor.Err(), context.Canceled)
	require.ErrorIs(t, admission.Err(), ErrLost)
	deadline, stopDeadline := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stopDeadline()
	err = admission.Close(deadline)
	require.ErrorIs(t, err, ErrClose)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(admission.joined)
	require.ErrorIs(t, admission.Close(t.Context()), ErrLost)
	require.NotErrorIs(t, admission.Close(t.Context()), ErrClose)
}

func TestAdmissionRejectsUnknownRoleBeforeConnection(t *testing.T) {
	t.Parallel()
	for _, role := range []Role{0, 4, 255} {
		admission, err := Acquire(t.Context(), nil, role)
		require.ErrorIs(t, err, ErrRole)
		require.Nil(t, admission)
	}
	admission, err := Acquire(t.Context(), nil, API)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Nil(t, admission)
}
