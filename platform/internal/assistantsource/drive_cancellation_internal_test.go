package assistantsource

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stalled token endpoint observes only the HTTP request context. Releasing
// the fake endpoint in cleanup also bounds a regression's test goroutines.
func blockedTokenDrive(t *testing.T) (*Drive, <-chan struct{}, chan<- struct{}) {
	t.Helper()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	drive, err := NewDrive(testSettings(t), transportFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, "oauth2.googleapis.com", request.URL.Host)
		started <- struct{}{}
		select {
		case <-request.Context().Done():
			return nil, request.Context().Err()
		case <-release:
			return nil, context.Canceled
		}
	}))
	require.NoError(t, err)
	return drive, started, release
}

func TestDriveTokenAcquisitionStopsOnFetchCancellation(t *testing.T) {
	t.Parallel()
	drive, started, release := blockedTokenDrive(t)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := drive.Fetch(ctx)
		result <- err
	}()
	t.Cleanup(func() { cancel(); close(release) })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("OAuth token request did not start")
	}
	before := time.Now()
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
		require.Less(t, time.Since(before), time.Second)
	case <-time.After(time.Second):
		t.Fatal("fetch cancellation did not cancel the OAuth token request promptly")
	}
}

func TestRunnerStopCancelsBlockedTokenAcquisition(t *testing.T) {
	t.Parallel()
	drive, started, release := blockedTokenDrive(t)
	store := &sourceStore{identities: map[string]string{}, values: map[string][]string{}}
	stop, err := (Runner{Store: store, Drive: drive}).Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { close(release); stop() })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runner's OAuth token request did not start")
	}
	stopped := make(chan struct{})
	before := time.Now()
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		require.Less(t, time.Since(before), time.Second)
	case <-time.After(time.Second):
		t.Fatal("runner stop waited for the OAuth client timeout")
	}
	assert.Empty(t, store.values)
	assert.Empty(t, store.failure, "shutdown must not publish a source failure")
}
