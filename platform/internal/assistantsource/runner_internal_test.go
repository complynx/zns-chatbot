package assistantsource

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// sourceStore is the existing in-memory Store. Optional faults are returned
// after domain handling, like the real knowledge service's SQL origins.
type sourceStore struct {
	mu           sync.Mutex
	identities   map[string]string
	values       map[string][]string
	failure      string
	configureErr error
	failureErr   error
	// replace, when set, runs outside the lock before a successful write so a
	// test can block or fail the operation.
	replace func(context.Context) error
}

func (s *sourceStore) ConfigureSource(_ context.Context, slot, identity string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configureErr != nil {
		return s.configureErr
	}
	if s.identities[slot] != identity {
		delete(s.values, slot)
	}
	s.identities[slot] = identity
	return nil
}
func (s *sourceStore) ReplaceSource(ctx context.Context, slot, _, _ string, texts []string) error {
	s.mu.Lock()
	replace := s.replace
	s.mu.Unlock()
	if replace != nil {
		if err := replace(ctx); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[slot] = texts
	return nil
}
func (s *sourceStore) SourceFailure(_ context.Context, _, _, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failureErr != nil {
		return s.failureErr
	}
	s.failure = code
	return nil
}

func (s *sourceStore) recordedFailure() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failure
}

func newSourceStore() *sourceStore {
	return &sourceStore{identities: map[string]string{}, values: map[string][]string{}}
}

// fatalRecorder counts every report so a test can prove a single delivery.
func fatalRecorder() (func(error), <-chan error) {
	reports := make(chan error, 4)
	return func(err error) { reports <- err }, reports
}

func unexpectedFatal(t *testing.T) func(error) {
	t.Helper()
	return func(err error) { t.Errorf("unexpected fatal report: %v", err) }
}

// privateSQLFault is what the knowledge service returns at a known SQL origin.
func privateSQLFault() error {
	return core.DatabaseOperationError(&pgconn.PgError{Code: "08006", Message: "private-sql-canary"})
}

// servingDrive answers OAuth and export; status selects export success or a
// provider failure carrying a canary body.
func servingDrive(t *testing.T, status func() int) *Drive {
	t.Helper()
	drive, err := NewDrive(testSettings(t), transportFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "oauth2.googleapis.com" {
			return response(request, http.StatusOK, `{"access_token":"credential-canary","expires_in":3600}`), nil
		}
		code := status()
		if code != http.StatusOK {
			return response(request, code, "secret-error-body-canary"), nil
		}
		return response(request, http.StatusOK, "remote-content-canary"), nil
	}))
	require.NoError(t, err)
	return drive
}

func always(code int) func() int { return func() int { return code } }

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
	store := newSourceStore()
	runner := Runner{Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Drive: drive, StaticPath: path}
	stop, err := runner.Start(t.Context(), unexpectedFatal(t))
	require.NoError(t, err)
	<-fetched
	stop()
	assert.Empty(t, store.values[knowledge.AssistantAbout])
	assert.Contains(t, store.values[knowledge.AssistantQA][0], "static-content-canary")
	modeMu.Lock()
	fail = false
	modeMu.Unlock()
	require.NoError(t, runner.Refresh(t.Context()))
	assert.Equal(t, []string{"remote-content-canary"}, store.values[knowledge.AssistantAbout])
	modeMu.Lock()
	fail = true
	modeMu.Unlock()
	require.NoError(t, runner.Refresh(t.Context()), "a provider failure stays optional")
	assert.Equal(t, []string{"remote-content-canary"}, store.values[knowledge.AssistantAbout])
	assert.Equal(t, "fetch_failed", store.failure)
	for _, canary := range []string{
		"credential-canary", "secret-error-body-canary", "remote-content-canary", "static-content-canary",
	} {
		assert.NotContains(t, logs.String(), canary)
	}
}

func assertSafeLogs(t *testing.T, logs string) {
	t.Helper()
	for _, canary := range []string{
		"credential-canary", "secret-error-body-canary", "remote-content-canary", "private-sql-canary",
	} {
		assert.NotContains(t, logs, canary)
	}
}

func TestRunnerProviderFailureFollowedBySourceFailureSQLIsFatal(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	store := newSourceStore()
	store.failureErr = privateSQLFault()
	runner := Runner{
		Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Drive: servingDrive(t, always(http.StatusServiceUnavailable)),
	}
	err := runner.Refresh(t.Context())
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
	assert.Contains(t, logs.String(), "fetch_failed")
	assertSafeLogs(t, logs.String())
}

func TestRunnerReplaceSourceKeepsDatabaseProvenance(t *testing.T) {
	t.Parallel()
	for _, fault := range []error{
		privateSQLFault(),
		&pgconn.PgError{Code: "57P01", Message: "private-sql-canary"},
	} {
		var logs bytes.Buffer
		store := newSourceStore()
		store.replace = func(context.Context) error { return fault }
		runner := Runner{
			Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			Drive: servingDrive(t, always(http.StatusOK)),
		}
		err := runner.Refresh(t.Context())
		require.ErrorIs(t, err, core.ErrDatabase)
		assert.NotContains(t, err.Error(), "private-sql-canary")
		require.NotErrorIs(t, err, ErrFetch, "a database failure is not rewritten as a fetch failure")
		assert.Empty(t, store.recordedFailure())
		assert.Empty(t, store.values)
		assert.Contains(t, logs.String(), "database_unavailable")
		assertSafeLogs(t, logs.String())
	}
}

func TestRunnerReplaceSourceDomainFailureStaysOptional(t *testing.T) {
	t.Parallel()
	store := newSourceStore()
	store.replace = func(context.Context) error { return errors.New("knowledge_stale") }
	runner := Runner{Store: store, Drive: servingDrive(t, always(http.StatusOK))}
	require.NoError(t, runner.Refresh(t.Context()))
	assert.Equal(t, "fetch_failed", store.recordedFailure())
}

func TestRunnerStartReturnsStartupDatabaseFailure(t *testing.T) {
	t.Parallel()
	existing := filepath.Join(t.TempDir(), "qa.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("collection:\n- answer: static\n"), 0o600))
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	for name, test := range map[string]struct {
		store  func(*sourceStore)
		static string
	}{
		"configure": {store: func(s *sourceStore) { s.configureErr = privateSQLFault() }},
		"static publish": {
			store:  func(s *sourceStore) { s.replace = func(context.Context) error { return privateSQLFault() } },
			static: existing,
		},
		"static failure record": {store: func(s *sourceStore) { s.failureErr = privateSQLFault() }, static: missing},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newSourceStore()
			test.store(store)
			stop, err := (Runner{Store: store, StaticPath: test.static}).Start(t.Context(), unexpectedFatal(t))
			require.ErrorIs(t, err, core.ErrDatabase)
			assert.NotContains(t, err.Error(), "private-sql-canary")
			assert.Nil(t, stop)
		})
	}
}

func TestRunnerStartKeepsNonDatabaseOutcomes(t *testing.T) {
	t.Parallel()
	store := newSourceStore()
	store.configureErr = errors.New("private-domain-canary")
	_, err := (Runner{Store: store}).Start(t.Context(), unexpectedFatal(t))
	require.EqualError(t, err, "assistant source configuration failed")
	require.False(t, core.IsDatabaseFailure(err))

	store = newSourceStore()
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	stop, err := (Runner{Store: store, StaticPath: missing}).Start(t.Context(), unexpectedFatal(t))
	require.NoError(t, err, "an unreadable static source stays optional")
	stop()
	assert.Equal(t, "fetch_failed", store.recordedFailure())
}

func TestRunnerDriveDatabaseFailureReportsOnceAndStops(t *testing.T) {
	t.Parallel()
	store := newSourceStore()
	var calls int
	var callsMu sync.Mutex
	store.replace = func(context.Context) error {
		callsMu.Lock()
		defer callsMu.Unlock()
		calls++
		return privateSQLFault()
	}
	onFatal, reports := fatalRecorder()
	stop, err := (Runner{Store: store, Drive: servingDrive(t, always(http.StatusOK))}).Start(t.Context(), onFatal)
	require.NoError(t, err)
	select {
	case report := <-reports:
		require.ErrorIs(t, report, core.ErrDatabase)
	case <-time.After(time.Second):
		t.Fatal("refresh database failure was not reported")
	}
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("refresh loop did not exit after its fatal report")
	}
	assert.Empty(t, reports, "a fatal is reported once")
	callsMu.Lock()
	defer callsMu.Unlock()
	assert.Equal(t, 1, calls)
}

// Normal stop begins first; the active publish then finishes its bounded work.
// A database failure observed there still reaches the owner, while pure
// cancellation stays normal and records no source failure.
func TestRunnerStopJoinsActivePublishAndRetainsDatabaseFailure(t *testing.T) {
	t.Parallel()
	for name, fatal := range map[string]bool{"database": true, "cancellation": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			started, release := make(chan struct{}), make(chan struct{})
			store := newSourceStore()
			store.replace = func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				<-release
				if fatal {
					return errors.Join(core.ErrDatabase, ctx.Err())
				}
				return ctx.Err()
			}
			onFatal, reports := fatalRecorder()
			runner := Runner{Store: store, Drive: servingDrive(t, always(http.StatusOK))}
			stop, err := runner.Start(t.Context(), onFatal)
			require.NoError(t, err)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("publish did not start")
			}
			stopped := make(chan struct{})
			go func() { stop(); close(stopped) }()
			select {
			case <-stopped:
				t.Fatal("stop returned before the active publish finished")
			case <-time.After(50 * time.Millisecond):
			}
			close(release)
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("stop did not join the active publish")
			}
			if fatal {
				require.Len(t, reports, 1)
				require.ErrorIs(t, <-reports, core.ErrDatabase)
			} else {
				assert.Empty(t, reports)
			}
			assert.Empty(t, store.recordedFailure(), "shutdown must not publish a source failure")
		})
	}
}
