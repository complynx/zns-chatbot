//nolint:testpackage // Tests verify private journal and shared-pool lifecycle invariants.
package descriptor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
)

type interruptedDescriptor struct {
	symboldescribe.Loom

	started, cancelled chan struct{}
}

func (d *interruptedDescriptor) GetRun(ctx context.Context, _ loom.Auth, _ string) (loom.Run, error) {
	close(d.started)
	<-ctx.Done()
	close(d.cancelled)
	return loom.Run{}, ctx.Err()
}

func TestEmbeddedWorkerSharesPoolStopsAndResumesOriginalRun(t *testing.T) {
	t.Parallel()
	p := &descriptorPeers{}
	d := descriptorDependencies(t, p)
	generation := uuid.New()
	seedDescriptor(t, d, "embedded", generation)
	interrupted := &interruptedDescriptor{Loom: d.flow.Loom,
		started: make(chan struct{}), cancelled: make(chan struct{})}
	flow := d.flow
	flow.Loom = interrupted
	o := Options{Store: d.store, Flow: flow, Auth: d.auth, BotID: d.bot, Journal: t.TempDir(), Concurrency: 1}
	worker, err := New(o)
	require.NoError(t, err)
	require.Same(t, d.store, worker.options.Store)
	require.NoError(t, worker.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = worker.Stop(ctx)
	})
	select {
	case <-interrupted.started:
	case <-time.After(10 * time.Second):
		t.Fatal("descriptor did not start the controlled run")
	}
	duplicate, err := New(o)
	require.NoError(t, err)
	require.Error(t, duplicate.Start(t.Context()))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, worker.Stop(ctx))
	require.NoError(t, worker.Stop(ctx))
	select {
	case <-interrupted.cancelled:
	default:
		t.Fatal("Stop returned before cancelling the active step")
	}
	_, err = os.Stat(filepath.Join(o.Journal, "worker.lock"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = d.store.Settings(t.Context())
	require.NoError(t, err, "descriptor must not close the application's shared pool")
	p.mu.Lock()
	p.complete = true
	p.mu.Unlock()
	o.Flow = d.flow
	resumed, err := New(o)
	require.NoError(t, err)
	require.NoError(t, resumed.Start(t.Context()))
	t.Cleanup(func() {
		stop, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelStop()
		_ = resumed.Stop(stop)
	})
	require.Eventually(t, func() bool {
		entry, readErr := d.store.PendingSymbol(t.Context(), d.bot, "sticker", "embedded")
		return readErr == nil && entry.Described
	}, 10*time.Second, 20*time.Millisecond)
	p.mu.Lock()
	require.Equal(t, 1, p.creates, "restart must observe the original run")
	require.Equal(t, 1, p.chats)
	p.mu.Unlock()
}
