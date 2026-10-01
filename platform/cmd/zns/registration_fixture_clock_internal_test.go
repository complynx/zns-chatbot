package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
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
