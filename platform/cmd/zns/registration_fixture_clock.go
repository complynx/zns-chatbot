package main

import (
	"context"

	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

const registrationClockAPIMode = "api"
const registrationClockBotMode = "bot"
const registrationClockReadAttempts = 3

var errRegistrationClockPublication = errors.New("registration clock publication changed during read")

// The operator owns atomic state publication and persistent compare-and-set.
// The app reads fresh bounded state at each domain observation, never at intake
// or process start alone. No model or registration action can update this file.
type registrationFileClock struct {
	mu     sync.Mutex
	config registrationclock.Settings
	last   registrationclock.State
}

func registrationClockEnvironment() registrationclock.Settings {
	return registrationclock.Settings{
		File:            os.Getenv("REGISTRATION_CLOCK_FILE"),
		Installation:    os.Getenv("REGISTRATION_CLOCK_INSTALLATION"),
		Case:            os.Getenv("REGISTRATION_CLOCK_CASE"),
		DatabaseAddress: os.Getenv("REGISTRATION_CLOCK_DATABASE_ADDRESS"),
		Anchor:          os.Getenv("REGISTRATION_CLOCK_ANCHOR"),
	}
}

func rejectRegistrationClockMode(command string) error {
	if (command == registrationClockAPIMode || command == registrationClockBotMode) &&
		registrationClockEnvironment().Enabled() {
		return errors.New("registration clock is supported only by the combined app")
	}
	return nil
}

func configuredRegistrationClock(
	ctx context.Context,
	db *pgxpool.Pool,
	cfg config.Config,
) (registrationingress.Clock, bool, error) {
	settings := registrationClockEnvironment()
	if !settings.Enabled() {
		return nil, false, nil
	}
	if cfg.Env != "sandbox" || !cfg.SyntheticOnly {
		return nil, true, errors.New("registration clock requires synthetic sandbox mode")
	}
	if err := settings.Validate(); err != nil {
		return nil, true, err
	}
	if err := registrationClockPlatform(); err != nil {
		return nil, true, err
	}
	if err := registrationClockDatabaseGuard(ctx, db, settings); err != nil {
		return nil, true, err
	}
	clock := &registrationFileClock{config: settings}
	if _, err := clock.Now(ctx); err != nil {
		return nil, true, err
	}
	return clock, true, nil
}

func registrationClockDatabaseGuard(ctx context.Context, db *pgxpool.Pool, settings registrationclock.Settings) error {
	connection := db.Config().ConnConfig
	actual := net.JoinHostPort(connection.Host, strconv.Itoa(int(connection.Port))) + "/" + connection.Database
	if actual != settings.DatabaseAddress {
		return errors.New("registration clock database allocation mismatch")
	}
	var allowed bool
	err := db.QueryRow(ctx, `SELECT current_database()=$1 AND current_user=pg_get_userbyid(datdba)
 AND (SELECT count(*) FROM core.users)=3
 AND (SELECT count(*) FROM core.users WHERE (id='alice' AND telegram_id=101)
 OR (id='bob' AND telegram_id=202) OR (id='visitor' AND telegram_id=303))=3
 AND EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name=$2)
 FROM pg_database WHERE datname=current_database()`, registrationclock.Database,
		registrationclock.Marker).Scan(&allowed)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !allowed {
		return errors.New("registration clock database owner, identities or case marker mismatch")
	}
	return nil
}

func (c *registrationFileClock) Now(ctx context.Context) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	state, err := readRegistrationClock(c.config.File)
	if err != nil {
		return time.Time{}, err
	}
	if err = validateRegistrationClockState(state, c.config, c.last); err != nil {
		return time.Time{}, err
	}
	if err = ctx.Err(); err != nil {
		return time.Time{}, err
	}
	c.last = state
	return state.Current.UTC(), nil
}

func readRegistrationClock(path string) (registrationclock.State, error) {
	for range registrationClockReadAttempts {
		state, err := readRegistrationClockPublication(path)
		if !errors.Is(err, errRegistrationClockPublication) {
			return state, err
		}
	}
	return registrationclock.State{}, errRegistrationClockPublication
}

// A rename between lstat and open is a normal atomic publication. Retry only
// that race, within a fixed bound; malformed or unavailable state never falls
// back to real time or a previously cached observation.
func readRegistrationClockPublication(path string) (registrationclock.State, error) {
	var state registrationclock.State
	info, err := os.Lstat(path)
	if err != nil {
		return state, fmt.Errorf("registration clock state unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return state, errors.New("registration clock state must be a regular operator-only file")
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return state, fmt.Errorf("stat registration clock directory: %w", err)
	}
	if !parent.IsDir() || parent.Mode().Perm() != 0o700 {
		return state, errors.New("registration clock directory must be operator-only")
	}
	if err = registrationClockOwner(info, parent); err != nil {
		return state, err
	}
	file, err := os.Open(path)
	if err != nil {
		return state, fmt.Errorf("registration clock state unavailable: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return state, fmt.Errorf("stat registration clock state: %w", err)
	}
	if !os.SameFile(info, opened) {
		return state, errRegistrationClockPublication
	}
	raw, err := io.ReadAll(io.LimitReader(file, registrationclock.Bytes+1))
	if err != nil {
		return state, fmt.Errorf("read registration clock state: %w", err)
	}
	return registrationclock.Decode(raw)
}

func validateRegistrationClockState(
	s registrationclock.State,
	settings registrationclock.Settings,
	previous registrationclock.State,
) error {
	if err := s.Validate(settings); err != nil {
		return err
	}
	if previous.Revision != 0 && (!s.Anchor.Equal(previous.Anchor) || s.Revision < previous.Revision ||
		s.Current.Before(previous.Current) ||
		(s.Revision == previous.Revision && s.Digest() != previous.Digest()) ||
		(s.Revision > previous.Revision && !s.Current.After(previous.Current))) {
		return errors.New("registration clock state rewound or changed without revision")
	}
	return nil
}
