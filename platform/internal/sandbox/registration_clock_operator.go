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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
)

const clockActionInit = "init"
const clockActionRead = "read"
const clockActionAdvance = "advance"
const clockFilePermission = 0o600

const registrationClockSetupPrefix = "registration-clock-setup:" + registrationclock.Installation + ":" + registrationclock.Case + ":"

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
		err = registrationClockOwnerGuard(ctx, tx)
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
	anchor, err := settings.AnchorTime()
	if err != nil {
		return empty, err
	}
	if _, err = registrationClockSetup(ctx, tx, anchor); err != nil {
		return empty, err
	}
	if change.Action == clockActionInit && !markerReady {
		if err = registrationClockFreshAdmission(ctx, tx); err != nil {
			return empty, err
		}
	}
	state, err := operateRegistrationClockFile(ctx, settings, settings.File, change, markerReady)
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
	markerCommitted bool,
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
		if markerCommitted {
			return state, errors.New("committed registration clock publication is missing")
		}
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
		var info os.FileInfo
		info, err = temporary.Stat()
		if err == nil {
			err = privateClockOwner(info, false)
		}
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

func registrationClockSetupMarker(anchor, opens time.Time) string {
	return registrationClockSetupPrefix + strconv.FormatInt(
		anchor.UnixMicro(),
		10,
	) + ":" + strconv.FormatInt(
		opens.UnixMicro(),
		10,
	)
}

// Initial activation cannot reinterpret registration already admitted without
// this clock. Committed-clock replay does not revisit or remove domain data.
func registrationClockFreshAdmission(ctx context.Context, tx pgx.Tx) error {
	var fresh bool
	err := tx.QueryRow(ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM core.pass_bookings WHERE event_id IN ($1,$2))
 AND NOT EXISTS(SELECT 1 FROM core.registration_intents WHERE event_id IN ($1,$2))
 AND NOT EXISTS(SELECT 1 FROM core.registration_intent_requests WHERE event_id IN ($1,$2))`,
		RegistrationFixtureEventA, RegistrationFixtureEventB).Scan(&fresh)
	if err != nil {
		return err
	}
	if !fresh {
		return errors.New("registration clock requires untouched A/B admission before initial activation")
	}
	return nil
}

// Initial publication is an owning-role operation, never a superuser fallback.
func registrationClockOwnerGuard(ctx context.Context, tx pgx.Tx) error {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT current_database()=$1 AND current_user='zns_app' AND session_user='zns_app'
 AND pg_get_userbyid(d.datdba)='zns_app' AND r.rolcanlogin AND NOT r.rolsuper
 AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication AND NOT r.rolbypassrls
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid)
 FROM pg_database d CROSS JOIN pg_roles r WHERE d.datname=current_database() AND r.rolname=current_user`,
		RegistrationFixtureDatabase).Scan(&allowed)
	if err != nil || !allowed {
		return errors.New("registration clock requires the bounded owning zns_app login")
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE public.zns_sandbox_fixtures, core.users, core.pass_events,
 core.pass_event_tiers, core.pass_bookings, core.registration_intents, core.registration_ingress, core.registration_intent_requests,
 core.pass_payment_admins, core.pass_booking_admins IN ACCESS SHARE MODE`); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT pg_get_userbyid(c.relowner)='zns_app'
 AND has_table_privilege('zns_app',c.oid,'SELECT') AND has_table_privilege('zns_app',c.oid,'INSERT')
 AND (SELECT count(*) FROM public.zns_sandbox_fixtures WHERE name IN ('product-v1','product-passport-v1'))=2
 AND (SELECT count(*) FROM core.users)=3
 AND (SELECT count(*) FROM core.users WHERE (id='alice' AND telegram_id=101)
 OR (id='bob' AND telegram_id=202) OR (id='visitor' AND telegram_id=303))=3
 AND (SELECT count(*)=9 AND bool_and(pg_get_userbyid(t.relowner)='zns_app')
 FROM pg_class t JOIN pg_namespace n ON n.oid=t.relnamespace
 WHERE n.nspname='core' AND t.relkind='r' AND t.relname IN
 ('users','pass_events','pass_event_tiers','pass_bookings','registration_intents','registration_ingress','registration_intent_requests',
 'pass_payment_admins','pass_booking_admins'))
 FROM pg_class c WHERE c.oid='public.zns_sandbox_fixtures'::regclass AND c.relkind='r'`).Scan(&allowed)
	if err != nil || !allowed {
		return errors.New("registration clock owning fixture metadata mismatch")
	}
	return nil
}

// The setup owner records the approved pair before any clock publication.
// Event locks serialize the corresponding domain configuration; marker reads
// share the existing fixture advisory lock with the sole setup producer.
func registrationClockSetup(ctx context.Context, tx pgx.Tx, anchor time.Time) (time.Time, error) {
	var opening time.Time
	rows, err := tx.Query(ctx, `SELECT CASE WHEN octet_length(name)<=192 THEN name ELSE '' END
 FROM public.zns_sandbox_fixtures WHERE name LIKE $1 ORDER BY name LIMIT 2`,
		registrationClockSetupPrefix+"%")
	if err != nil {
		return opening, err
	}
	var marker string
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&marker); err != nil {
			break
		}
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil || count != 1 {
		return opening, errors.New("registration clock requires one committed setup binding")
	}
	parts := strings.Split(strings.TrimPrefix(marker, registrationClockSetupPrefix), ":")
	if len(parts) != 2 {
		return opening, errors.New("registration clock setup binding is invalid")
	}
	storedAnchor, anchorErr := strconv.ParseInt(parts[0], 10, 64)
	storedOpening, openingErr := strconv.ParseInt(parts[1], 10, 64)
	opening = time.UnixMicro(storedOpening).UTC()
	if anchorErr != nil || openingErr != nil || !time.UnixMicro(storedAnchor).Equal(anchor) ||
		marker != registrationClockSetupMarker(anchor, opening) {
		return opening, errors.New("registration clock setup anchor mismatch")
	}
	if err = passbooking.LockMutationEvents(
		ctx,
		tx,
		[]string{RegistrationFixtureEventA, RegistrationFixtureEventB},
	); err != nil {
		return opening, err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM core.users ORDER BY id FOR SHARE`); err != nil {
		return opening, err
	}
	var matches bool
	err = tx.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(finishes_at=$3::timestamptz+interval '7 days')
 FROM core.pass_events WHERE id IN ($1,$2)`, RegistrationFixtureEventA, RegistrationFixtureEventB, opening).Scan(&matches)
	if err != nil || !matches {
		return opening, errors.New("registration clock event setup mismatch")
	}
	if err = registrationClockTiers(ctx, tx, opening); err != nil {
		return opening, err
	}
	return opening, nil
}

func registrationClockTiers(ctx context.Context, tx pgx.Tx, opening time.Time) error {
	rows, err := tx.Query(ctx, `SELECT event_id,position,starts_at FROM core.pass_event_tiers
 WHERE event_id IN ($1,$2) ORDER BY event_id,position LIMIT 4`, RegistrationFixtureEventA, RegistrationFixtureEventB)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var event string
		var position int64
		var starts time.Time
		if err = rows.Scan(&event, &position, &starts); err != nil {
			return err
		}
		want := opening
		if event == RegistrationFixtureEventA && position == 1 {
			want = opening.Add(24 * time.Hour)
		} else if position != 0 {
			return errors.New("registration clock tier setup mismatch")
		}
		if !starts.Equal(want) {
			return errors.New("registration clock tier opening mismatch")
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != 3 {
		return errors.New("registration clock tier count mismatch")
	}
	rows.Close()
	var owned bool
	if err = tx.QueryRow(ctx, `SELECT pg_get_userbyid(relowner)='zns_app'
 FROM pg_class WHERE oid='core.pass_event_tiers'::regclass`).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return errors.New("registration clock tier ownership mismatch")
	}
	return nil
}
