//nolint:testpackage // Tests exercise the probe without database, timer sleeps or paid calls.
package descriptor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestBillingProbeFailureRetainsLastSuccessAndRecovers(t *testing.T) {
	t.Parallel()
	fail := false
	w := &Worker{options: Options{BillingRead: func(context.Context) (loom.CreditBalance, error) {
		if fail {
			return loom.CreditBalance{}, errors.New("private source failure")
		}
		return loom.CreditBalance{AvailableMicrocredits: 5000000, ReservedMicrocredits: 10}, nil
	}}}
	_, ok, at := w.BillingSnapshot()
	require.False(t, ok)
	require.True(t, at.IsZero())
	w.collectBilling(t.Context())
	balance, ok, first := w.BillingSnapshot()
	require.True(t, ok)
	require.EqualValues(t, 5000000, balance.AvailableMicrocredits)
	fail = true
	w.collectBilling(t.Context())
	_, ok, at = w.BillingSnapshot()
	require.False(t, ok)
	require.Equal(t, first, at)
	fail = false
	w.collectBilling(t.Context())
	_, ok, at = w.BillingSnapshot()
	require.True(t, ok)
	require.False(t, at.Before(first))
}

func TestBillingProbeStopCancelsAndJoinsInflightRead(t *testing.T) {
	t.Parallel()
	started, stopped := make(chan struct{}), make(chan struct{})
	w := &Worker{options: Options{BillingRead: func(ctx context.Context) (loom.CreditBalance, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return loom.CreditBalance{}, ctx.Err()
	}}}
	stop := w.startBilling(t.Context())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	stop()
	select {
	case <-stopped:
	default:
		t.Fatal("probe stop returned before read exited")
	}
	_, ok, _ := w.BillingSnapshot()
	require.False(t, ok)
}
