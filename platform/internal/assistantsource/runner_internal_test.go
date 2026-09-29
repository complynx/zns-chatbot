package assistantsource

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type sourceStore struct {
	mu         sync.Mutex
	identities map[string]string
	values     map[string][]string
	failure    string
}

func (s *sourceStore) ConfigureSource(_ context.Context, slot, identity string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identities[slot] != identity {
		delete(s.values, slot)
	}
	s.identities[slot] = identity
	return nil
}
func (s *sourceStore) ReplaceSource(_ context.Context, slot, _, _ string, texts []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[slot] = texts
	return nil
}
func (s *sourceStore) SourceFailure(_ context.Context, _, _, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = code
	return nil
}

func TestRunnerStartupRefreshRetainsLastGoodAndRedacts(t *testing.T) {
	t.Parallel()
	settings := testSettings(t)
	var logs bytes.Buffer
	var modeMu sync.Mutex
	fail := true
	fetched := make(chan struct{}, 10)
	drive, err := NewDrive(settings, transportFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "oauth2.googleapis.com" {
			return response(request, http.StatusOK, `{"access_token":"credential-canary","expires_in":3600}`), nil
		}
		modeMu.Lock()
		failure := fail
		modeMu.Unlock()
		fetched <- struct{}{}
		if failure {
			return response(request, http.StatusServiceUnavailable, "secret-error-body-canary"), nil
		}
		return response(request, http.StatusOK, "remote-content-canary"), nil
	}))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "qa.yaml")
	require.NoError(t, os.WriteFile(path, []byte("collection:\n- answer: static-content-canary\n"), 0o600))
	store := &sourceStore{identities: map[string]string{}, values: map[string][]string{}}
	runner := Runner{Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Drive: drive, StaticPath: path}
	stop, err := runner.Start(t.Context())
	require.NoError(t, err)
	<-fetched
	stop()
	assert.Empty(t, store.values[knowledge.AssistantAbout])
	assert.Contains(t, store.values[knowledge.AssistantQA][0], "static-content-canary")
	modeMu.Lock()
	fail = false
	modeMu.Unlock()
	runner.Refresh(t.Context())
	assert.Equal(t, []string{"remote-content-canary"}, store.values[knowledge.AssistantAbout])
	modeMu.Lock()
	fail = true
	modeMu.Unlock()
	runner.Refresh(t.Context())
	assert.Equal(t, []string{"remote-content-canary"}, store.values[knowledge.AssistantAbout])
	assert.Equal(t, "fetch_failed", store.failure)
	for _, canary := range []string{"credential-canary", "secret-error-body-canary", "remote-content-canary", "static-content-canary"} {
		assert.NotContains(t, logs.String(), canary)
	}
}
