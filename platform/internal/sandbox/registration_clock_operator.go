package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
)

const clockActionInit = "init"
const clockActionRead = "read"
const clockActionAdvance = "advance"
const clockFilePermission = 0o600

// RegistrationClockChange is accepted only by the assigned synthetic operator.
// The case, anchor and allocation come from the shared fixed settings.
type RegistrationClockChange struct {
	Action           string
	ExpectedRevision uint64
	Target           time.Time
}

func (c RegistrationClockChange) validate() error {
	switch c.Action {
	case clockActionInit, clockActionRead:
		if c.ExpectedRevision != 0 || !c.Target.IsZero() {
			return errors.New("revision and target are advance-only")
		}
	case clockActionAdvance:
		if c.ExpectedRevision == 0 || c.Target.IsZero() {
			return errors.New("advance requires the observed revision and target")
		}
	default:
		return errors.New("unknown registration clock operation")
	}
	return nil
}

// ApplyRegistrationClock verifies the allocated database before touching state.
// Init publishes the file before its marker: an interrupted init is retryable
// while app activation stays denied until the marker transaction commits.
func ApplyRegistrationClock(
	ctx context.Context,
	db *pgxpool.Pool,
	cfg config.Config,
	settings registrationclock.Settings,
	change RegistrationClockChange,
) (registrationclock.State, error) {
	var empty registrationclock.State
	if cfg.Env != "sandbox" || !cfg.SyntheticOnly || db == nil {
		return empty, errors.New("registration clock operator requires the owned synthetic sandbox")
	}
	if err := settings.Validate(); err != nil {
		return empty, err
	}
	if err := change.validate(); err != nil {
		return empty, err
	}
	if err := registrationClockOperatorPlatform(); err != nil {
		return empty, err
	}
	if err := registrationClockOperatorAllocation(db.Config(), settings); err != nil {
		return empty, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return empty, errors.New("registration clock database unavailable")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918431003)`); err != nil {
		return empty, errors.New("registration clock fixture lock unavailable")
	}
	if change.Action == clockActionInit {
		err = registrationFixtureGuard(ctx, tx)
	} else {
		err = registrationFixtureOperatorGuard(ctx, tx,
			RegistrationFixture{Stand: RegistrationFixtureStand, Action: clockActionRead})
	}
	if err != nil {
		return empty, errors.New("registration clock database ownership or identities mismatch")
	}
	var fixtureReady, markerReady bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name=$1),
 EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name=$2)`, registrationFixtureMarker, registrationclock.Marker).
		Scan(&fixtureReady, &markerReady)
	if err != nil || !fixtureReady || (change.Action != clockActionInit && !markerReady) {
		return empty, errors.New("registration clock fixture or marker unavailable")
	}
	state, err := operateRegistrationClockFile(ctx, settings, settings.File, change)
	if err != nil {
		return empty, err
	}
	if change.Action == clockActionInit {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO public.zns_sandbox_fixtures(name) VALUES($1) ON CONFLICT DO NOTHING`,
			registrationclock.Marker,
		); err != nil {
			return empty, errors.New("clock state published; marker unavailable, retry init before activation")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, errors.New("clock operation may be published; verify state and marker before retry")
	}
	return state, nil
}

func registrationClockOperatorAllocation(pool *pgxpool.Config, settings registrationclock.Settings) error {
	connection := pool.ConnConfig
	address := net.JoinHostPort(connection.Host, strconv.Itoa(int(connection.Port))) + "/" + connection.Database
	if address != settings.DatabaseAddress {
		return errors.New("registration clock operator database address mismatch")
	}
	for _, fallback := range connection.Fallbacks {
		if fallback.Host != connection.Host || fallback.Port != connection.Port {
			return errors.New("registration clock operator database fallback mismatch")
		}
	}
	return nil
}

func operateRegistrationClockFile(
	ctx context.Context,
	settings registrationclock.Settings,
	path string,
	change RegistrationClockChange,
) (registrationclock.State, error) {
	var state registrationclock.State
	if err := settings.Validate(); err != nil {
		return state, err
	}
	if err := change.validate(); err != nil {
		return state, err
	}
	if err := ctx.Err(); err != nil {
		return state, err
	}
	directory := filepath.Dir(path)
	openedDirectory, lock, err := lockRegistrationClockDirectory(directory)
	if err != nil {
		return state, err
	}
	defer lock.Close()
	defer openedDirectory.Close()
	state, err = readOperatorClockPublication(openedDirectory, path, settings)
	if change.Action == clockActionInit && errors.Is(err, os.ErrNotExist) {
		anchor, anchorErr := settings.AnchorTime()
		if anchorErr != nil {
			return state, anchorErr
		}
		state = registrationclock.State{
			Version: 1, Installation: settings.Installation, Case: settings.Case,
			Stand: registrationclock.Stand, DatabaseAddress: settings.DatabaseAddress,
			Anchor: anchor, Current: anchor, Revision: 1,
		}
		return publishOperatorClock(ctx, openedDirectory, path, settings, state)
	}
	if err != nil {
		return state, err
	}
	switch change.Action {
	case clockActionRead:
		return state, nil
	case clockActionInit:
		if state.Revision != 1 || !state.Current.Equal(state.Anchor) {
			return state, errors.New("init cannot reset or reactivate an advanced clock")
		}
		return state, nil
	case clockActionAdvance:
		if state.Revision != change.ExpectedRevision || state.Revision == math.MaxUint64 {
			return state, errors.New("registration clock revision conflict")
		}
		if !change.Target.After(state.Current) {
			return state, errors.New("registration clock target must advance current time")
		}
		state.Current = change.Target
		state.Revision++
		if err = state.Validate(settings); err != nil {
			return state, err
		}
		return publishOperatorClock(ctx, openedDirectory, path, settings, state)
	default:
		return state, errors.New("unknown registration clock operation")
	}
}

func readOperatorClock(path string, settings registrationclock.Settings) (registrationclock.State, error) {
	directory, err := openRegistrationClockDirectory(filepath.Dir(path))
	if err != nil {
		return registrationclock.State{}, err
	}
	defer directory.Close()
	return readOperatorClockPublication(directory, path, settings)
}

func readOperatorClockPublication(
	directory *os.File,
	path string,
	settings registrationclock.Settings,
) (registrationclock.State, error) {
	var state registrationclock.State
	file, err := openRegistrationClockState(directory, filepath.Base(path))
	if err != nil {
		return state, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, registrationclock.Bytes+1))
	if err != nil {
		return state, err
	}
	state, err = registrationclock.Decode(raw)
	if err == nil {
		err = state.Validate(settings)
	}
	if err == nil {
		err = validateRegistrationClockDirectory(directory, filepath.Dir(path))
	}
	return state, err
}

func publishOperatorClock(
	ctx context.Context,
	directory *os.File,
	path string,
	settings registrationclock.Settings,
	state registrationclock.State,
) (registrationclock.State, error) {
	if err := state.Validate(settings); err != nil {
		return state, err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return state, err
	}
	state, err = registrationclock.Decode(raw)
	if err != nil {
		return state, err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return state, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil {
		return state, err
	}
	pinned, err := directory.Stat()
	if err != nil || !os.SameFile(opened, pinned) {
		return state, errors.New("registration clock directory changed before publication")
	}
	name := ".state-" + rand.Text()
	temporary, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, clockFilePermission)
	if err != nil {
		return state, err
	}
	defer func() {
		_ = temporary.Close()
		_ = root.Remove(name)
	}()
	if err = temporary.Chmod(clockFilePermission); err == nil {
		_, err = temporary.Write(raw)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if err == nil {
		err = temporary.Close()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = validateRegistrationClockDirectory(directory, filepath.Dir(path))
	}
	if err == nil {
		err = root.Rename(name, filepath.Base(path))
	}
	if err != nil {
		return state, err
	}
	if err = syncRegistrationClockDirectory(directory); err != nil {
		return state, errors.New("clock publication durability uncertain; read back before retry")
	}
	if err = errors.Join(ctx.Err(), validateRegistrationClockDirectory(directory, filepath.Dir(path))); err != nil {
		return state, errors.New("clock publication binding uncertain; read back before retry")
	}
	return state, nil
}
