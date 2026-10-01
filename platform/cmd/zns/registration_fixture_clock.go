package main

import (
	"context"

	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const registrationClockAppMode = "app"
const registrationClockReadAttempts = 3

var errRegistrationClockPublication = errors.New("registration clock publication changed during read")

const registrationClockMountFields = 10

type registrationClockMount struct {
	id, device, root, path string
	readOnly               bool
}

// Mount roots identify the backing filesystem paths, even when an alias has a
// different namespace path. A writable ancestor or child exposes publication.
func registrationClockReaderMount(raw []byte, id, directory string) error {
	var mounts []registrationClockMount
	var reader registrationClockMount
	for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
		mount, err := parseRegistrationClockMount(line)
		if err != nil {
			return err
		}
		mounts = append(mounts, mount)
		if mount.id == id {
			if reader.id != "" {
				return errors.New("duplicate registration clock mount identity")
			}
			reader = mount
		}
	}
	if reader.id == "" || !reader.readOnly || reader.path != directory {
		return errors.New("registration clock requires a whole-directory read-only mount")
	}
	for _, mount := range mounts {
		if mount.device == reader.device && !mount.readOnly &&
			(registrationClockPathWithin(mount.root, reader.root) || registrationClockPathWithin(reader.root, mount.root)) {
			return errors.New("registration clock publication has a writable mount alias")
		}
	}
	return nil
}

func registrationClockPathWithin(child, parent string) bool {
	return child == parent || strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}

func parseRegistrationClockMount(line string) (registrationClockMount, error) {
	var mount registrationClockMount
	fields := strings.Fields(line)
	if len(fields) < registrationClockMountFields {
		return mount, errors.New("invalid registration clock mount metadata")
	}
	separator := 6
	for separator < len(fields) && fields[separator] != "-" {
		separator++
	}
	if separator+4 != len(fields) {
		return mount, errors.New("invalid registration clock mount metadata")
	}
	for _, value := range []string{fields[0], fields[1]} {
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return mount, err
		}
	}
	major, minor, validDevice := strings.Cut(fields[2], ":")
	if !validDevice {
		return mount, errors.New("invalid registration clock mount device")
	}
	for _, value := range []string{major, minor} {
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return mount, err
		}
	}
	root, err := registrationClockMountPath(fields[3])
	if err != nil {
		return mount, err
	}
	path, err := registrationClockMountPath(fields[4])
	if err != nil {
		return mount, err
	}
	options := "," + fields[5] + ","
	ro, rw := strings.Contains(options, ",ro,"), strings.Contains(options, ",rw,")
	if ro == rw {
		return mount, errors.New("invalid registration clock mount access")
	}
	return registrationClockMount{id: fields[0], device: fields[2], root: root, path: path, readOnly: ro}, nil
}

func registrationClockMountPath(value string) (string, error) {
	var decoded strings.Builder
	for position := 0; position < len(value); position++ {
		if value[position] != '\\' {
			decoded.WriteByte(value[position])
			continue
		}
		if position+3 >= len(value) {
			return "", errors.New("invalid registration clock mount path escape")
		}
		escape := value[position+1 : position+4]
		switch escape {
		case "040":
			decoded.WriteByte(' ')
		case "011":
			decoded.WriteByte('\t')
		case "012":
			decoded.WriteByte('\n')
		case "134":
			decoded.WriteByte('\\')
		default:
			return "", errors.New("invalid registration clock mount path escape")
		}
		position += 3
	}
	mountPath := decoded.String()
	if !strings.HasPrefix(mountPath, "/") || path.Clean(mountPath) != mountPath {
		return "", errors.New("invalid registration clock mount path")
	}
	return mountPath, nil
}

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
	if command != registrationClockAppMode &&
		registrationClockEnvironment().Enabled() {
		return errors.New("registration clock is supported only by the combined app")
	}
	return nil
}

// Preflight validates the effective connection and private initial publication
// before opening the database. Live fixture validation precedes admission.
func preflightRegistrationClock(
	ctx context.Context,
	command string,
	cfg config.Config,
) (*registrationFileClock, bool, error) {
	if err := rejectRegistrationClockMode(command); err != nil {
		return nil, true, err
	}
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
	instance, err := runtimeapp.EnvironmentInstance(true)
	if err != nil {
		return nil, true, err
	}
	if instance.Installation != registrationclock.Installation || !managedTopologyAllowed(cfg) {
		return nil, true, errors.New("registration clock requires the exact managed installation and topology")
	}
	connection, err := pgxpool.ParseConfig(cfg.Database.URL.Value())
	if err != nil {
		return nil, true, errors.New("invalid database configuration")
	}
	if err = registrationClockAllocation(connection, settings); err != nil {
		return nil, true, err
	}
	clock := &registrationFileClock{config: settings}
	if _, err = clock.Now(ctx); err != nil {
		return nil, true, err
	}
	return clock, true, nil
}

func configuredRegistrationClock(
	ctx context.Context,
	db *pgxpool.Pool,
	cfg config.Config,
) (registrationingress.Clock, bool, error) {
	clock, configured, err := preflightRegistrationClock(ctx, registrationClockAppMode, cfg)
	if err != nil || !configured {
		return nil, configured, err
	}
	if err = registrationClockDatabaseGuard(ctx, db, clock.config); err != nil {
		return nil, true, err
	}
	return clock, true, nil
}

func registrationClockAllocation(pool *pgxpool.Config, settings registrationclock.Settings) error {
	connection := pool.ConnConfig
	actual := net.JoinHostPort(connection.Host, strconv.Itoa(int(connection.Port))) + "/" + connection.Database
	if actual != settings.DatabaseAddress {
		return errors.New("registration clock database allocation mismatch")
	}
	for _, fallback := range connection.Fallbacks {
		if fallback.Host != connection.Host || fallback.Port != connection.Port {
			return errors.New("registration clock database allocation mismatch")
		}
	}
	return nil
}

func registrationClockDatabaseGuard(ctx context.Context, db *pgxpool.Pool, settings registrationclock.Settings) error {
	if err := registrationClockAllocation(db.Config(), settings); err != nil {
		return err
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
	parentPath := filepath.Dir(path)
	parent, err := registrationClockParent(parentPath)
	if err != nil {
		return state, fmt.Errorf("stat registration clock directory: %w", err)
	}
	if !parent.IsDir() || parent.Mode().Perm() != 0o700 {
		return state, errors.New("registration clock directory must be operator-only")
	}
	if err = registrationClockOwner(info, parent); err != nil {
		return state, err
	}
	directory, err := registrationClockOpenDirectory(parentPath)
	if err != nil {
		return state, fmt.Errorf("open registration clock directory: %w", err)
	}
	defer directory.Close()
	openedParent, err := directory.Stat()
	if err != nil {
		return state, err
	}
	if !os.SameFile(parent, openedParent) {
		return state, errRegistrationClockPublication
	}
	if err = registrationClockReadOnly(directory); err != nil {
		return state, err
	}
	file, err := registrationClockOpenPublication(directory, path)
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
	if err = registrationClockMetadata(opened, openedParent); err != nil {
		return state, err
	}
	if err = registrationClockReadOnly(file); err != nil {
		return state, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, registrationclock.Bytes+1))
	if err != nil {
		return state, fmt.Errorf("read registration clock state: %w", err)
	}
	if err = validateRegistrationClockBinding(path, file, openedParent); err != nil {
		return state, err
	}

	return registrationclock.Decode(raw)
}

func registrationClockParent(path string) (os.FileInfo, error) {
	parent, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	for current := path; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return nil, statErr
		}
		if !info.IsDir() {
			return nil, errors.New("registration clock path must not contain symlinks")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return parent, nil
}

func registrationClockMetadata(file, directory os.FileInfo) error {
	if !file.Mode().IsRegular() || file.Mode().Perm() != 0o600 ||
		!directory.IsDir() || directory.Mode().Perm() != 0o700 {
		return errors.New("registration clock publication must remain operator-only")
	}
	return registrationClockOwner(file, directory)
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

func validateRegistrationClockBinding(path string, file *os.File, openedParent os.FileInfo) error {
	finalParent, err := registrationClockParent(filepath.Dir(path))
	if err != nil {
		return err
	}
	finalFile, err := file.Stat()
	if err != nil {
		return err
	}
	if err = registrationClockMetadata(finalFile, finalParent); err != nil {
		return err
	}
	if !os.SameFile(openedParent, finalParent) {
		return errRegistrationClockPublication
	}
	visible, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err = registrationClockMetadata(visible, finalParent); err != nil {
		return err
	}
	if !os.SameFile(finalFile, visible) {
		return errRegistrationClockPublication
	}
	return nil
}
