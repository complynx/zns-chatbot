package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func clockSettings() registrationclock.Settings {
	return registrationclock.Settings{
		File: registrationclock.Path, Installation: registrationclock.Installation,
		Case: registrationclock.Case, DatabaseAddress: registrationclock.DatabaseAddress,
		Anchor: "2026-10-01T12:00:00Z",
	}
}

func clockState(t *testing.T, current time.Time, revision uint64) registrationclock.State {
	t.Helper()
	settings := clockSettings()
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	state := registrationclock.State{
		Version: 1, Installation: settings.Installation, Case: settings.Case,
		Stand: registrationclock.Stand, DatabaseAddress: settings.DatabaseAddress,
		Anchor: anchor, Current: current, Revision: revision,
	}
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	decoded, err := registrationclock.Decode(raw)
	require.NoError(t, err)
	return decoded
}

func TestRegistrationClockStateBoundsAndReplacement(t *testing.T) {
	t.Parallel()
	settings := clockSettings()
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	first := clockState(t, anchor, 1)
	require.NoError(t, validateRegistrationClockState(first, settings, registrationclock.State{}))
	next := clockState(t, anchor.Add(registrationclock.Horizon), 2)
	require.NoError(t, validateRegistrationClockState(next, settings, first))
	require.NoError(
		t,
		validateRegistrationClockState(next, settings, registrationclock.State{}),
		"replacement reads persistent current",
	)
	require.Error(t, validateRegistrationClockState(first, settings, next), "rewind is rejected")
	for name, change := range map[string]func(*registrationclock.State){
		"horizon":                  func(s *registrationclock.State) { s.Current = s.Current.Add(time.Microsecond) },
		"precision":                func(s *registrationclock.State) { s.Current = s.Current.Add(-time.Nanosecond) },
		"wrong-case":               func(s *registrationclock.State) { s.Case = "other" },
		"wrong-anchor":             func(s *registrationclock.State) { s.Anchor = s.Anchor.Add(time.Microsecond) },
		"same-revision-change":     func(s *registrationclock.State) { s.Revision = 1 },
		"revision-without-advance": func(s *registrationclock.State) { s.Current = anchor },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			invalid := next
			change(&invalid)
			require.Error(t, validateRegistrationClockState(invalid, settings, first))
		})
	}
}

func TestRegistrationClockDecodeIsBoundedAndStrict(t *testing.T) {
	t.Parallel()
	for _, raw := range [][]byte{
		[]byte(`{"unexpected":true}`), []byte(`{} {}`), make([]byte, registrationclock.Bytes+1), nil,
	} {
		_, err := registrationclock.Decode(raw)
		require.Error(t, err)
	}
	settings := clockSettings()
	settings.DatabaseAddress = "127.0.0.1:55432/synthetic_qa_zns_registration_fixture"
	require.Error(t, settings.Validate(), "same database name is not the allocated runtime")
	settings = clockSettings()
	settings.Anchor = "2026-10-01T12:00:00.000000001Z"
	require.Error(t, settings.Validate())
}

func TestRegistrationClockDecodeRequiresExactUniqueFields(t *testing.T) {
	t.Parallel()
	settings := clockSettings()
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	raw, err := json.Marshal(clockState(t, anchor, 1))
	require.NoError(t, err)
	valid := string(raw)
	for name, malformed := range map[string]string{
		"duplicate-version": strings.Replace(valid, `"version":1`, `"version":0,"version":1`, 1),
		"duplicate-current": strings.Replace(valid, `"current":`, `"current":null,"current":`, 1),
		"wrong-case":        strings.Replace(valid, `"version":`, `"VERSION":`, 1),
		"missing":           strings.Replace(valid, `"version":1,`, "", 1),
		"unknown":           strings.Replace(valid, `"version":1`, `"version":1,"extra":true`, 1),
		"trailing":          valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, decodeErr := registrationclock.Decode([]byte(malformed))
			require.Error(t, decodeErr)
		})
	}
	decoded, err := registrationclock.Decode(raw)
	require.NoError(t, err)
	require.NoError(t, decoded.Validate(settings))
}

func TestRegistrationClockStartupPreflightPrecedesDatabase(t *testing.T) {
	settings := clockSettings()
	t.Setenv("REGISTRATION_CLOCK_FILE", settings.File)
	for key, value := range map[string]string{
		"REGISTRATION_CLOCK_FILE": settings.File, "REGISTRATION_CLOCK_INSTALLATION": settings.Installation,
		"REGISTRATION_CLOCK_CASE": settings.Case, "REGISTRATION_CLOCK_DATABASE_ADDRESS": settings.DatabaseAddress,
		"REGISTRATION_CLOCK_ANCHOR": settings.Anchor,
	} {
		t.Setenv(key, value)
	}
	opened := false
	opener := func(context.Context, config.Config, *observability.Runtime) (*pgxpool.Pool, error) {
		opened = true
		return nil, io.ErrUnexpectedEOF
	}
	cfg := config.Config{Env: "sandbox", SyntheticOnly: true}
	if runtime.GOOS != "linux" {
		err := runCommandWithDatabase(t.Context(), registrationClockAppMode, nil, cfg, nil, opener)
		require.ErrorContains(t, err, "requires Linux UID guards")
		require.False(t, opened, "actual startup must not open, ping or admit the database")
	}
	t.Setenv("REGISTRATION_CLOCK_CASE", "wrong-case")
	err := runCommandWithDatabase(t.Context(), registrationClockAppMode, nil, cfg, nil, opener)
	require.Error(t, err)
	require.False(t, opened)
}

func TestRegistrationClockConfigurationIsOptIn(t *testing.T) {
	for _, name := range []string{
		"REGISTRATION_CLOCK_FILE", "REGISTRATION_CLOCK_INSTALLATION", "REGISTRATION_CLOCK_CASE",
		"REGISTRATION_CLOCK_DATABASE_ADDRESS", "REGISTRATION_CLOCK_ANCHOR",
	} {
		t.Setenv(name, "")
	}
	clock, configured, err := configuredRegistrationClock(t.Context(), nil, config.Config{Env: "production"})
	require.NoError(t, err)
	require.Nil(t, clock, "defaults do not touch a database or file")
	require.False(t, configured)
	require.NoError(t, rejectRegistrationClockMode("api"))
	t.Setenv("REGISTRATION_CLOCK_FILE", registrationclock.Path)
	require.Error(t, rejectRegistrationClockMode("api"))
	require.Error(t, rejectRegistrationClockMode("bot"))
	require.NoError(t, rejectRegistrationClockMode("app"))
	_, _, err = configuredRegistrationClock(t.Context(), nil, config.Config{Env: "production"})
	require.Error(t, err)
	_, _, err = configuredRegistrationClock(t.Context(), nil, config.Config{Env: "sandbox", SyntheticOnly: true})
	require.Error(t, err, "partial configuration fails before database access")
	if runtime.GOOS != "linux" {
		settings := clockSettings()
		t.Setenv("REGISTRATION_CLOCK_INSTALLATION", settings.Installation)
		t.Setenv("REGISTRATION_CLOCK_CASE", settings.Case)
		t.Setenv("REGISTRATION_CLOCK_DATABASE_ADDRESS", settings.DatabaseAddress)
		t.Setenv("REGISTRATION_CLOCK_ANCHOR", settings.Anchor)
		_, _, err = configuredRegistrationClock(t.Context(), nil, config.Config{Env: "sandbox", SyntheticOnly: true})
		require.ErrorContains(t, err, "requires Linux UID guards", "reject before any database access")
	}
}

func TestRegistrationClockFileReadsFreshStateAndFailsVisibly(t *testing.T) {
	t.Parallel()
	settings := clockSettings()
	settings.File = filepath.Join(t.TempDir(), "state.json")
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	write := func(current time.Time, revision uint64) {
		t.Helper()
		raw, marshalErr := json.Marshal(clockState(t, current, revision))
		require.NoError(t, marshalErr)
		require.NoError(t, os.WriteFile(settings.File, raw, 0o600))
	}
	write(anchor, 1)
	clock := &registrationFileClock{config: settings}
	if runtime.GOOS != "linux" {
		_, err = clock.Now(t.Context())
		require.Error(t, err, "POSIX owner-only guard cannot be inferred from Windows mode bits")
		return
	}
	first, err := clock.Now(t.Context())
	require.NoError(t, err)
	require.Equal(t, anchor, first)
	write(anchor.Add(time.Hour), 2)
	restarted := &registrationFileClock{config: settings}
	current, err := restarted.Now(t.Context())
	require.NoError(t, err)
	require.Equal(t, anchor.Add(time.Hour), current)
	_, err = clock.Now(t.Context())
	require.NoError(t, err)
	write(anchor, 1)
	_, err = clock.Now(t.Context())
	require.Error(t, err)
	require.NoError(t, os.Remove(settings.File))
	_, err = clock.Now(t.Context())
	require.ErrorIs(t, err, os.ErrNotExist)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = clock.Now(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRegistrationClockRejectsEveryUnsupportedCommandBeforeDatabase(t *testing.T) {
	t.Setenv("REGISTRATION_CLOCK_FILE", "")
	for _, name := range []string{"REGISTRATION_CLOCK_FILE", "REGISTRATION_CLOCK_INSTALLATION", "REGISTRATION_CLOCK_CASE", "REGISTRATION_CLOCK_DATABASE_ADDRESS", "REGISTRATION_CLOCK_ANCHOR"} {
		t.Setenv(name, "")
	}
	commands := []string{
		"migrate",
		"fixture",
		"product-fixture",
		"export-fixture",
		"api",
		"bot",
		"fake",
		"model",
		"health",
	}
	for _, command := range commands {
		require.NoError(t, rejectRegistrationClockMode(command))
	}
	for _, name := range []string{"REGISTRATION_CLOCK_FILE", "REGISTRATION_CLOCK_INSTALLATION", "REGISTRATION_CLOCK_CASE", "REGISTRATION_CLOCK_DATABASE_ADDRESS", "REGISTRATION_CLOCK_ANCHOR"} {
		t.Setenv(name, "partial")
		for _, command := range commands {
			opened := false
			opener := func(context.Context, config.Config, *observability.Runtime) (*pgxpool.Pool, error) {
				opened = true
				return nil, io.ErrUnexpectedEOF
			}
			err := runCommandWithDatabase(t.Context(), command, nil, config.Config{}, nil, opener)
			require.ErrorContains(t, err, "supported only by the combined app", command)
			require.False(t, opened, command)
		}
		t.Setenv(name, "")
	}
}

func TestRegistrationClockEffectiveAllocation(t *testing.T) {
	t.Parallel()
	for _, dsn := range []string{
		"postgres://postgres/" + registrationclock.Database + "?sslmode=disable",
		"host=postgres port=5432 dbname=" + registrationclock.Database + " sslmode=disable",
	} {
		pool, err := pgxpool.ParseConfig(dsn)
		require.NoError(t, err)
		require.NoError(t, registrationClockAllocation(pool, clockSettings()))
	}
	for _, dsn := range []string{
		"postgres://other/" + registrationclock.Database + "?sslmode=disable",
		"postgres://postgres/unrelated?sslmode=disable",
		"host=postgres,other port=5432 dbname=" + registrationclock.Database + " sslmode=disable",
		"postgres://postgres/" + registrationclock.Database + "?host=other&sslmode=disable",
	} {
		pool, err := pgxpool.ParseConfig(dsn)
		require.NoError(t, err)
		require.ErrorContains(t, registrationClockAllocation(pool, clockSettings()), "allocation mismatch")
	}
}

// This startup test owns the fixed Linux publication only inside an isolated
// tooling container with a private /run tmpfs.
func TestRegistrationClockLinuxStartupBoundary(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("REGISTRATION_CLOCK_STARTUP_TEST") != "isolated" {
		t.Skip("requires isolated Linux /run publication")
	}
	settings := clockSettings()
	t.Setenv("REGISTRATION_CLOCK_FILE", settings.File)
	for key, value := range map[string]string{
		"REGISTRATION_CLOCK_FILE": settings.File, "REGISTRATION_CLOCK_INSTALLATION": settings.Installation,
		"REGISTRATION_CLOCK_CASE": settings.Case, "REGISTRATION_CLOCK_DATABASE_ADDRESS": settings.DatabaseAddress,
		"REGISTRATION_CLOCK_ANCHOR": settings.Anchor,
	} {
		t.Setenv(key, value)
	}
	directory := filepath.Dir(settings.File)
	require.NoError(t, os.Mkdir(directory, 0o700), "must start with a private empty /run")
	require.NoError(t, os.Chmod(directory, 0o700))
	t.Cleanup(func() {
		require.NoError(t, os.Chmod(directory, 0o700))
		require.NoError(t, os.Remove(settings.File))
		require.NoError(t, os.Remove(directory))
	})
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	raw, err := json.Marshal(clockState(t, anchor, 1))
	require.NoError(t, err)
	write := func() {
		t.Helper()
		require.NoError(t, os.WriteFile(settings.File, raw, 0o600))
		require.NoError(t, os.Chmod(settings.File, 0o600))
	}
	cfg := config.Config{
		Env:           "sandbox",
		SyntheticOnly: true,
		Database: config.Database{
			URL: config.Secret("postgres://postgres/" + registrationclock.Database + "?sslmode=disable"),
		},
	}
	opened := false
	opener := func(context.Context, config.Config, *observability.Runtime) (*pgxpool.Pool, error) {
		opened = true
		return nil, io.ErrUnexpectedEOF
	}
	telemetry, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	cfg.Shutdown.TelemetryFlush = time.Second
	write()
	require.ErrorIs(t, runCommandWithDatabase(t.Context(), "app", nil, cfg, telemetry, opener), io.ErrUnexpectedEOF)
	require.True(t, opened, "valid actual UID and private publication reaches database opening")
	for _, scenario := range []string{"wrong-database", "wrong-effective-host", "missing", "malformed", "insecure-file", "insecure-directory"} {
		write()
		require.NoError(t, os.Chmod(directory, 0o700))
		invalid := cfg
		switch scenario {
		case "wrong-database":
			invalid.Database.URL = "postgres://postgres/unrelated?sslmode=disable"
		case "wrong-effective-host":
			invalid.Database.URL = config.Secret(
				"postgres://postgres/" + registrationclock.Database + "?host=other&sslmode=disable",
			)
		case "missing":
			require.NoError(t, os.Remove(settings.File))
		case "malformed":
			require.NoError(t, os.WriteFile(settings.File, []byte("{}"), 0o600))
		case "insecure-file":
			require.NoError(t, os.Chmod(settings.File, 0o644))
		case "insecure-directory":
			require.NoError(t, os.Chmod(directory, 0o755))
		}
		opened = false
		require.Error(t, runCommandWithDatabase(t.Context(), "app", nil, invalid, telemetry, opener), scenario)
		require.False(t, opened, "invalid startup must not open or admit a database: "+scenario)
	}
}

func TestRegistrationClockLinuxLiveStartupGuard(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("REGISTRATION_CLOCK_STARTUP_TEST") != "isolated" {
		t.Skip("requires isolated Linux publication and startup PostgreSQL fixture")
	}
	dsn := os.Getenv("REGISTRATION_CLOCK_STARTUP_DATABASE_URL")
	require.NotEmpty(t, dsn, "isolated startup gate must supply its private fixture connection")
	settings := clockSettings()
	t.Setenv("REGISTRATION_CLOCK_FILE", settings.File)
	for key, value := range map[string]string{
		"REGISTRATION_CLOCK_FILE": settings.File, "REGISTRATION_CLOCK_INSTALLATION": settings.Installation,
		"REGISTRATION_CLOCK_CASE": settings.Case, "REGISTRATION_CLOCK_DATABASE_ADDRESS": settings.DatabaseAddress,
		"REGISTRATION_CLOCK_ANCHOR": settings.Anchor,
	} {
		t.Setenv(key, value)
	}
	directory := filepath.Dir(settings.File)
	require.NoError(t, os.Mkdir(directory, 0o700))
	require.NoError(t, os.Chmod(directory, 0o700))
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	raw, err := json.Marshal(clockState(t, anchor, 1))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(settings.File, raw, 0o600))
	t.Cleanup(func() { require.NoError(t, os.Remove(settings.File)); require.NoError(t, os.Remove(directory)) })
	db, err := store.Open(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	owner, err := db.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(owner.Release)
	_, err = owner.Exec(t.Context(), "SELECT pg_advisory_lock(918274,1)")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, unlockErr := owner.Exec(context.WithoutCancel(t.Context()), "SELECT pg_advisory_unlock(918274,1)")
		require.NoError(t, unlockErr)
	})
	cfg := config.Config{Env: "sandbox", SyntheticOnly: true, Database: config.Database{URL: config.Secret(dsn)}}
	cfg.Shutdown.TelemetryFlush = time.Second
	startApp := func() error {
		t.Helper()
		telemetry, telemetryErr := observability.New(t.Context(), observability.Config{})
		require.NoError(t, telemetryErr)
		return runCommandWithDatabase(
			t.Context(),
			"app",
			nil,
			cfg,
			telemetry,
			func(ctx context.Context, cfg config.Config, _ *observability.Runtime) (*pgxpool.Pool, error) {
				return store.Open(ctx, cfg.Database.URL.Value())
			},
		)
	}
	require.ErrorIs(t, startApp(), runtimeapp.ErrBusy, "valid fixture reaches real runtime admission")
	_, err = db.Exec(t.Context(), "DELETE FROM public.zns_sandbox_fixtures WHERE name=$1", registrationclock.Marker)
	require.NoError(t, err)
	require.ErrorContains(
		t,
		startApp(),
		"database owner, identities or case marker mismatch",
		"live mismatch is rejected before the held admission lock",
	)
	_, err = db.Exec(t.Context(), "INSERT INTO public.zns_sandbox_fixtures(name) VALUES($1)", registrationclock.Marker)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), "UPDATE core.users SET telegram_id=999 WHERE id='alice'")
	require.NoError(t, err)
	require.ErrorContains(t, startApp(), "database owner, identities or case marker mismatch")
	_, err = db.Exec(t.Context(), "UPDATE core.users SET telegram_id=101 WHERE id='alice'")
	require.NoError(t, err)
	require.ErrorIs(t, startApp(), runtimeapp.ErrBusy, "restored fixture again reaches admission")
}
