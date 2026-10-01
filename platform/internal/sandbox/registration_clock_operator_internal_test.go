package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/registrationclock"
	"github.com/complynx/zns-chatbot/platform/internal/replacement"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

const operatorTestAnchor = "2026-10-01T00:00:00.000001Z"
const operatorChildDirectory = "ZNS_CLOCK_TEST_CHILD_DIRECTORY"
const operatorChildBoundary = "ZNS_CLOCK_TEST_CHILD_BOUNDARY"

func operatorTestSettings() registrationclock.Settings {
	return registrationclock.Settings{
		File: registrationclock.Path, Installation: registrationclock.Installation,
		Case: registrationclock.Case, DatabaseAddress: registrationclock.DatabaseAddress,
		Anchor: operatorTestAnchor,
	}
}

func operatorTestFile(t *testing.T) (string, registrationclock.Settings) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("actual Linux UID, atomic rename and cross-process lock proof")
	}
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, 0o700))
	return filepath.Join(directory, "state.json"), operatorTestSettings()
}

func TestRegistrationClockOperatorPersistentCAS(t *testing.T) {
	t.Parallel()
	path, settings := operatorTestFile(t)
	initial, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(1), initial.Revision)
	require.Equal(t, initial.Anchor, initial.Current)
	repeated, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, initial.Digest(), repeated.Digest())
	target := initial.Anchor.Add(registrationclock.Horizon)
	advanced, err := operateRegistrationClockFile(t.Context(), settings, path,
		RegistrationClockChange{Action: "advance", ExpectedRevision: 1, Target: target}, false)
	require.NoError(t, err)
	require.Equal(t, uint64(2), advanced.Revision)
	require.Equal(t, target, advanced.Current)
	for _, change := range []RegistrationClockChange{
		{Action: "advance", ExpectedRevision: 1, Target: target},
		{Action: "advance", ExpectedRevision: 2, Target: target},
		{Action: "advance", ExpectedRevision: 2, Target: target.Add(time.Microsecond)},
		{Action: "advance", ExpectedRevision: 2, Target: target.Add(-time.Microsecond)},
		{Action: "init"},
	} {
		_, err = operateRegistrationClockFile(t.Context(), settings, path, change, false)
		require.Error(t, err)
	}
	read, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "read"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, advanced.Digest(), read.Digest(), "fresh operator reads persisted state, not a process cache")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestRegistrationClockOperatorConcurrentRevision(t *testing.T) {
	t.Parallel()
	path, settings := operatorTestFile(t)
	initial, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.NoError(t, err)
	var workers sync.WaitGroup
	errorsReceived := make(chan error, 2)
	for range 2 {
		workers.Go(func() {
			_, updateErr := operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{
					Action:           "advance",
					ExpectedRevision: 1,
					Target:           initial.Anchor.Add(time.Minute),
				},
				false,
			)
			errorsReceived <- updateErr
		})
	}
	workers.Wait()
	first, second := <-errorsReceived, <-errorsReceived
	require.NotEqual(t, first == nil, second == nil, "exactly one old-revision advance succeeds")
	read, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "read"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(2), read.Revision)
}

func TestRegistrationClockOperatorRejectsHardLinks(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"state.json", "operator.lock"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path, settings := operatorTestFile(t)
			_, err := operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{Action: "init"},
				false,
			)
			require.NoError(t, err)
			require.NoError(
				t,
				os.Link(filepath.Join(filepath.Dir(path), name), filepath.Join(filepath.Dir(path), "alias")),
			)
			_, err = operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{Action: "read"},
				false,
			)
			require.Error(t, err, "multiply linked state and lock files are not private publications")
		})
	}
}

func TestRegistrationClockOperatorRejectsRootOwner(t *testing.T) {
	t.Parallel()
	path, settings := operatorTestFile(t)
	if os.Geteuid() != 0 {
		t.Skip("isolated actual root process required for the root-fallback negative")
	}
	_, err := operateRegistrationClockFile(t.Context(), settings, path, RegistrationClockChange{Action: "init"}, false)
	require.Error(t, err, "root-owned private metadata is not a nonroot app allocation")
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRegistrationClockOperatorRejectsBindingsPermissionsAndLinks(t *testing.T) {
	t.Parallel()
	path, settings := operatorTestFile(t)
	_, err := operateRegistrationClockFile(t.Context(), settings, path, RegistrationClockChange{Action: "init"}, false)
	require.NoError(t, err)
	wrong := settings
	wrong.Installation = "010400000203"
	_, err = operateRegistrationClockFile(t.Context(), wrong, path, RegistrationClockChange{Action: "read"}, false)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0o644))
	_, err = operateRegistrationClockFile(t.Context(), settings, path, RegistrationClockChange{Action: "read"}, false)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0o600))
	link := filepath.Join(filepath.Dir(path), "state-link.json")
	require.NoError(t, os.Symlink(path, link))
	_, err = operateRegistrationClockFile(t.Context(), settings, link, RegistrationClockChange{Action: "read"}, false)
	require.Error(t, err)
	require.NoError(t, os.Chmod(filepath.Dir(path), 0o755))
	_, err = operateRegistrationClockFile(t.Context(), settings, path, RegistrationClockChange{Action: "read"}, false)
	require.Error(t, err)
}

func TestRegistrationClockOperatorCanceledPublication(t *testing.T) {
	t.Parallel()
	path, settings := operatorTestFile(t)
	initial, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = operateRegistrationClockFile(canceled, settings, path,
		RegistrationClockChange{Action: "advance", ExpectedRevision: 1, Target: initial.Anchor.Add(time.Minute)}, false)
	require.ErrorIs(t, err, context.Canceled)
	current, err := readOperatorClock(path, settings)
	require.NoError(t, err)
	require.Equal(t, initial.Digest(), current.Digest())
}

func TestRegistrationClockOperatorRejectsDifferentUID(t *testing.T) {
	t.Parallel()
	wrongUID := os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_WRONG_UID_FILE")
	if wrongUID == "" {
		t.Skip("isolated root-owned state file required for the different-UID probe")
	}
	info, err := os.Stat(wrongUID)
	require.NoError(t, err)
	err = privateClockOwner(info, false)
	require.ErrorContains(t, err, "owned by the app UID")
}

func TestRegistrationClockOperatorCrashBoundaries(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"before-rename", "after-rename"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			path, settings := operatorTestFile(t)
			_, err := operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{Action: "init"},
				false,
			)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRegistrationClockOperatorChild$")
			child.Env = append(
				os.Environ(),
				operatorChildDirectory+"="+filepath.Dir(path),
				operatorChildBoundary+"="+boundary,
			)
			stdout, err := child.StdoutPipe()
			require.NoError(t, err)
			stdin, err := child.StdinPipe()
			require.NoError(t, err)
			defer stdin.Close()
			require.NoError(t, child.Start())
			ready, err := bufio.NewReader(stdout).ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, "published-boundary\n", ready)
			_, err = operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{Action: "read"},
				false,
			)
			require.Error(t, err, "another process retains the operator lock")
			require.NoError(t, child.Process.Kill())
			require.Error(t, child.Wait())
			state, err := operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{Action: "read"},
				false,
			)
			require.NoError(t, err, "kernel releases the dead writer's lock")
			expected := uint64(1)
			if boundary == "after-rename" {
				expected = 2
			}
			require.Equal(t, expected, state.Revision)
			_, err = operateRegistrationClockFile(
				t.Context(),
				settings,
				path,
				RegistrationClockChange{
					Action:           "advance",
					ExpectedRevision: expected,
					Target:           state.Current.Add(time.Minute),
				},
				false,
			)
			require.NoError(t, err)
		})
	}
}

// Subprocess helper holds the real OS lock while the parent kills the writer.
func TestRegistrationClockOperatorChild(t *testing.T) {
	t.Parallel()
	directory := os.Getenv(operatorChildDirectory)
	if directory == "" {
		t.Skip("subprocess helper")
	}
	openedDirectory, lock, err := lockRegistrationClockDirectory(directory)
	require.NoError(t, err)
	defer lock.Close()
	defer openedDirectory.Close()
	path := filepath.Join(directory, "state.json")
	state, err := readOperatorClock(path, operatorTestSettings())
	require.NoError(t, err)
	state.Current = state.Current.Add(time.Minute)
	state.Revision++
	if os.Getenv(operatorChildBoundary) == "after-rename" {
		_, err = publishOperatorClock(t.Context(), openedDirectory, path, operatorTestSettings(), state)
		require.NoError(t, err)
	} else {
		// An orphaned complete temp file must never replace the old state.
		raw, marshalErr := json.Marshal(state)
		require.NoError(t, marshalErr)
		temporary, createErr := os.CreateTemp(directory, ".state-*")
		require.NoError(t, createErr)
		_, err = temporary.Write(raw)
		require.NoError(t, err)
		require.NoError(t, temporary.Sync())
		require.NoError(t, temporary.Close())
	}
	_, err = fmt.Fprintln(os.Stdout, "published-boundary")
	require.NoError(t, err)
	_, _ = io.ReadFull(os.Stdin, make([]byte, 1))
}

func TestRegistrationClockOperatorUnsupported(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "linux" {
		t.Skip("unsupported host guard")
	}
	_, err := operateRegistrationClockFile(
		t.Context(),
		operatorTestSettings(),
		filepath.Join(t.TempDir(), "state.json"),
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.ErrorContains(t, err, "requires Linux")
}

func TestRegistrationClockOperatorAllocationFallbacks(t *testing.T) {
	t.Parallel()
	for _, address := range []string{
		"postgres://zns_registration_operator@postgres/" + RegistrationFixtureDatabase,
		"host=postgres port=5432 dbname=" + RegistrationFixtureDatabase + " user=zns_registration_operator sslmode=prefer",
	} {
		pool, err := pgxpool.ParseConfig(address)
		require.NoError(t, err)
		require.NoError(t, registrationClockOperatorAllocation(pool, operatorTestSettings()))
	}
	for _, address := range []string{
		"host=postgres,other port=5432 dbname=" + RegistrationFixtureDatabase + " user=zns_registration_operator",
		"postgres://zns_registration_operator@other/" + RegistrationFixtureDatabase,
		"postgres://zns_registration_operator@postgres/other",
	} {
		pool, err := pgxpool.ParseConfig(address)
		require.NoError(t, err)
		require.Error(t, registrationClockOperatorAllocation(pool, operatorTestSettings()))
	}
}

func TestRegistrationClockOperatorRejectsMovedDirectory(t *testing.T) {
	t.Parallel()
	path, settings := operatorTestFile(t)
	initial, err := operateRegistrationClockFile(
		t.Context(),
		settings,
		path,
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.NoError(t, err)
	directory, lock, err := lockRegistrationClockDirectory(filepath.Dir(path))
	require.NoError(t, err)
	defer lock.Close()
	defer directory.Close()
	moved := filepath.Dir(path) + "-moved"
	require.NoError(t, os.Rename(filepath.Dir(path), moved))
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(moved)) })
	require.NoError(t, os.Mkdir(filepath.Dir(path), 0o700))
	initial.Current = initial.Current.Add(time.Minute)
	initial.Revision++
	_, err = publishOperatorClock(t.Context(), directory, path, settings, initial)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	old, err := readOperatorClock(filepath.Join(moved, "state.json"), settings)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), old.Revision)
}

// This suite owns a freshly bootstrapped private cluster and clock volume.
// Its URLs and compiled CLI are supplied only by the assigned local gate.
func TestRegistrationClockOperatorPrivateDatabaseAndCLI(t *testing.T) {
	ownerURL := os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_OWNER_URL")
	if runtime.GOOS != "linux" || ownerURL == "" {
		t.Skip("allocated private Linux/PostgreSQL operator gate required")
	}
	// One environment-bound case owns the cluster, roles and publication.
	t.Setenv("REGISTRATION_CLOCK_OPERATOR_TEST_OWNER_URL", ownerURL)
	owner := operatorDatabase(t, "OWNER")
	operator := operatorDatabase(t, "OPERATOR")
	admin := operatorDatabase(t, "ADMIN")
	ctx := t.Context()
	require.NoError(t, store.Migrate(ctx, owner))
	require.NoError(t, store.Seed(ctx, owner))
	require.NoError(t, ApplyProductFixture(ctx, owner))
	settings := operatorTestSettings()
	anchor, err := settings.AnchorTime()
	require.NoError(t, err)
	_, err = ApplyRegistrationFixture(ctx, owner, RegistrationFixture{
		Stand:       RegistrationFixtureStand,
		Action:      "init",
		OpensAt:     anchor.Add(3*time.Hour + 17*time.Minute),
		ClockAnchor: anchor,
	})
	require.NoError(t, err)
	acl, err := os.ReadFile("../../../docs/sandbox/fqa-stands/registration/runtime-roles.sql")
	require.NoError(t, err)
	_, err = owner.Exec(ctx, string(acl))
	require.NoError(t, err)
	operatorSetupBinding(t, owner, settings, anchor)
	operatorUnclockedAdmission(t, owner, settings, anchor)
	publication, err := operateRegistrationClockFile(
		ctx,
		settings,
		settings.File,
		RegistrationClockChange{Action: "init"},
		false,
	)
	require.NoError(t, err, "simulate interrupted publication before the marker commit")
	initial, err := operatorCLI(t, "OWNER", settings, "-action", "init")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), initial.Revision)
	assert.Equal(t, publication.Digest(), initial.Digest(), "retry commits marker without replacing initial state")
	operatorOriginalContractNegatives(t, admin, owner, operator, settings, initial)
	operatorPrivateDenials(t, admin, owner, operator, settings, initial)
	operatorCommittedAdmissionReplay(t, owner, settings, anchor, initial)
	read, err := operatorCLI(t, "OPERATOR", settings, "-action", "read")
	require.NoError(t, err)
	assert.Equal(t, initial.Digest(), read.Digest())
	advanced, err := operatorCLI(t, "OPERATOR", settings, "-action", "advance", "-expected-revision", "1",
		"-target", anchor.Add(time.Minute).Format(time.RFC3339Nano))
	require.NoError(t, err)
	assert.Equal(t, uint64(2), advanced.Revision)
	operatorCLIInvalidTargets(t, settings, advanced)
	operatorFixtureSerialization(t, owner, operator, settings)
	operatorManagedInventory(t, owner, operator)
}

func operatorSetupBinding(t *testing.T, owner *pgxpool.Pool, settings registrationclock.Settings, anchor time.Time) {
	t.Helper()
	f := RegistrationFixture{Stand: RegistrationFixtureStand, Action: "init",
		OpensAt: anchor.Add(3*time.Hour + 17*time.Minute), ClockAnchor: anchor}
	_, err := ApplyRegistrationFixture(t.Context(), owner, f)
	require.NoError(t, err, "identical setup replay preserves the established binding")
	for _, changed := range []RegistrationFixture{
		{Stand: f.Stand, Action: f.Action, OpensAt: f.OpensAt, ClockAnchor: anchor.Add(time.Microsecond)},
		{Stand: f.Stand, Action: f.Action, OpensAt: f.OpensAt.Add(time.Minute), ClockAnchor: anchor},
	} {
		_, err = ApplyRegistrationFixture(t.Context(), owner, changed)
		require.Error(t, err)
	}
	changed := settings
	changed.Anchor = anchor.Add(time.Microsecond).Format(time.RFC3339Nano)
	_, err = ApplyRegistrationClock(t.Context(), owner, config.Config{Env: "sandbox", SyntheticOnly: true}, changed,
		RegistrationClockChange{Action: "init"})
	require.Error(t, err, "later operator input is not the setup authority")
	marker := registrationClockSetupMarker(anchor, f.OpensAt)
	_, err = owner.Exec(t.Context(), `DELETE FROM public.zns_sandbox_fixtures WHERE name=$1`, marker)
	require.NoError(t, err)
	_, err = ApplyRegistrationFixture(t.Context(), owner, f)
	require.Error(t, err, "old unbound setup cannot acquire a retrospective binding")
	f.ClockAnchor = time.Time{}
	_, err = ApplyRegistrationFixture(t.Context(), owner, f)
	require.NoError(t, err, "ordinary unclocked replay stays unchanged")
	_, err = owner.Exec(t.Context(), `INSERT INTO public.zns_sandbox_fixtures(name) VALUES($1)`, marker)
	require.NoError(t, err)
	conflict := registrationClockSetupMarker(anchor.Add(time.Microsecond), f.OpensAt)
	_, err = owner.Exec(t.Context(), `INSERT INTO public.zns_sandbox_fixtures(name) VALUES($1)`, conflict)
	require.NoError(t, err)
	_, err = ApplyRegistrationClock(t.Context(), owner, config.Config{Env: "sandbox", SyntheticOnly: true}, settings,
		RegistrationClockChange{Action: "init"})
	require.Error(t, err, "conflicting durable bindings cannot select one arbitrary anchor")
	_, err = owner.Exec(t.Context(), `DELETE FROM public.zns_sandbox_fixtures WHERE name=$1`, conflict)
	require.NoError(t, err)
}

type operatorAdmissionClock struct{ now time.Time }

func (c operatorAdmissionClock) Now(context.Context) (time.Time, error) { return c.now, nil }

func operatorUnclockedAdmission(
	t *testing.T,
	owner *pgxpool.Pool,
	settings registrationclock.Settings,
	anchor time.Time,
) {
	t.Helper()
	service := passbooking.Service{DB: owner, RegistrationClock: operatorAdmissionClock{now: anchor}}
	for _, event := range []string{RegistrationFixtureEventA, RegistrationFixtureEventB} {
		admission, err := service.CaptureAdmission(t.Context(), "alice", passbooking.AdmissionRequest{
			Command: passbooking.Command{Name: "solo", Event: event, Key: "operator-prior-admission"},
		})
		require.NoError(t, err)
		require.Positive(t, admission.ID, "genuine domain admission was committed before publication")
		_, err = ApplyRegistrationClock(
			t.Context(),
			owner,
			config.Config{Env: "sandbox", SyntheticOnly: true},
			settings,
			RegistrationClockChange{Action: "init"},
		)
		require.ErrorContains(t, err, "untouched A/B admission")
		_, err = os.Stat(settings.File)
		require.ErrorIs(t, err, os.ErrNotExist)
		var untouched bool
		require.NoError(t, owner.QueryRow(t.Context(), `SELECT
 EXISTS(SELECT 1 FROM core.registration_intents WHERE id=$1)
 AND NOT EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name=$2)`, admission.ID, registrationclock.Marker).Scan(&untouched))
		assert.True(t, untouched, "failure preserves domain admission and does not activate the clock")
		_, err = owner.Exec(
			t.Context(),
			`DELETE FROM core.registration_intent_requests WHERE intent_id=$1`,
			admission.ID,
		)
		require.NoError(t, err)
		_, err = owner.Exec(t.Context(), `DELETE FROM core.registration_intents WHERE id=$1`, admission.ID)
		require.NoError(t, err)
	}
}

func operatorCommittedAdmissionReplay(
	t *testing.T,
	owner *pgxpool.Pool,
	settings registrationclock.Settings,
	anchor time.Time,
	initial registrationclock.State,
) {
	t.Helper()
	service := passbooking.Service{DB: owner, RegistrationClock: operatorAdmissionClock{now: anchor}}
	admission, err := service.CaptureAdmission(t.Context(), "alice", passbooking.AdmissionRequest{
		Command: passbooking.Command{Name: "solo", Event: RegistrationFixtureEventA, Key: "operator-committed-replay"},
	})
	require.NoError(t, err)
	require.Positive(t, admission.ID)
	state, err := ApplyRegistrationClock(
		t.Context(),
		owner,
		config.Config{Env: "sandbox", SyntheticOnly: true},
		settings,
		RegistrationClockChange{Action: "init"},
	)
	require.NoError(t, err, "committed publication replay does not reject existing domain data")
	assert.Equal(t, initial.Digest(), state.Digest())
	var retained bool
	require.NoError(t, owner.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM core.registration_intents WHERE id=$1)`,
		admission.ID).Scan(&retained))
	assert.True(t, retained)
}

func operatorOriginalContractNegatives(
	t *testing.T,
	admin, owner, operator *pgxpool.Pool,
	settings registrationclock.Settings,
	initial registrationclock.State,
) {
	t.Helper()
	before, err := os.ReadFile(settings.File)
	require.NoError(t, err)
	cfg := config.Config{Env: "sandbox", SyntheticOnly: true}
	for _, scenario := range []struct{ name, change, restore string }{
		{"opening-a", `UPDATE core.pass_event_tiers SET starts_at=starts_at+interval '1 hour' WHERE event_id='registration-fixture-a' AND position=0`, `UPDATE core.pass_event_tiers SET starts_at=starts_at-interval '1 hour' WHERE event_id='registration-fixture-a' AND position=0`},
		{"opening-b", `UPDATE core.pass_event_tiers SET starts_at=starts_at+interval '1 hour' WHERE event_id='registration-fixture-b' AND position=0`, `UPDATE core.pass_event_tiers SET starts_at=starts_at-interval '1 hour' WHERE event_id='registration-fixture-b' AND position=0`},
		{"second-tier-a", `UPDATE core.pass_event_tiers SET starts_at=starts_at+interval '1 hour' WHERE event_id='registration-fixture-a' AND position=1`, `UPDATE core.pass_event_tiers SET starts_at=starts_at-interval '1 hour' WHERE event_id='registration-fixture-a' AND position=1`},
		{"finish-a", `UPDATE core.pass_events SET finishes_at=finishes_at+interval '1 hour' WHERE id='registration-fixture-a'`, `UPDATE core.pass_events SET finishes_at=finishes_at-interval '1 hour' WHERE id='registration-fixture-a'`},
		{"finish-b", `UPDATE core.pass_events SET finishes_at=finishes_at+interval '1 hour' WHERE id='registration-fixture-b'`, `UPDATE core.pass_events SET finishes_at=finishes_at-interval '1 hour' WHERE id='registration-fixture-b'`},
		{"tier-owner", `ALTER TABLE core.pass_event_tiers OWNER TO postgres`, `ALTER TABLE core.pass_event_tiers OWNER TO zns_app; GRANT SELECT(event_id,position,starts_at) ON core.pass_event_tiers TO zns_registration_operator`},
		{"owner-init", `ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO postgres`, `ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO zns_app`},
		{"owner-powers", `ALTER ROLE zns_app SUPERUSER`, `ALTER ROLE zns_app NOSUPERUSER`},
		{"owner-createdb", `ALTER ROLE zns_app CREATEDB`, `ALTER ROLE zns_app NOCREATEDB`},
		{"owner-createrole", `ALTER ROLE zns_app CREATEROLE`, `ALTER ROLE zns_app NOCREATEROLE`},
		{"owner-replication", `ALTER ROLE zns_app REPLICATION`, `ALTER ROLE zns_app NOREPLICATION`},
		{"owner-bypassrls", `ALTER ROLE zns_app BYPASSRLS`, `ALTER ROLE zns_app NOBYPASSRLS`},
		{"owner-membership", `GRANT zns_registration_operator TO zns_app`, `REVOKE zns_registration_operator FROM zns_app`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, changeErr := admin.Exec(t.Context(), scenario.change)
			require.NoError(t, changeErr)
			t.Cleanup(func() {
				_, restoreErr := admin.Exec(context.WithoutCancel(t.Context()), scenario.restore)
				require.NoError(t, restoreErr)
			})
			client, action := operator, "read"
			if scenario.name == "owner-init" {
				client, action = admin, "init"
			} else if strings.HasPrefix(scenario.name, "owner-") {
				client, action = owner, "init"
			}
			_, rejected := ApplyRegistrationClock(
				t.Context(),
				client,
				cfg,
				settings,
				RegistrationClockChange{Action: action},
			)
			require.Error(t, rejected)
			after, readErr := os.ReadFile(settings.File)
			require.NoError(t, readErr)
			assert.Equal(t, before, after)
		})
	}
	t.Run("committed-marker-missing-file", func(t *testing.T) {
		t.Cleanup(func() { require.NoError(t, os.WriteFile(settings.File, before, 0o600)) })
		_, advanceErr := ApplyRegistrationClock(t.Context(), operator, cfg, settings,
			RegistrationClockChange{Action: "advance", ExpectedRevision: 1, Target: initial.Current.Add(time.Minute)})
		require.NoError(t, advanceErr)
		require.NoError(t, os.Remove(settings.File))
		_, initErr := ApplyRegistrationClock(t.Context(), owner, cfg, settings, RegistrationClockChange{Action: "init"})
		require.Error(t, initErr, "a committed publication cannot be recreated or reset")
		_, statErr := os.Stat(settings.File)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})
}

func operatorDatabase(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	db, err := pgxpool.New(t.Context(), os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_"+role+"_URL"))
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, db.Ping(t.Context()))
	return db
}

func operatorCLI(
	t *testing.T,
	role string,
	settings registrationclock.Settings,
	args ...string,
) (registrationclock.State, error) {
	t.Helper()
	path := os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_CLI")
	require.NotEmpty(t, path)
	command := exec.CommandContext(t.Context(), path, args...)
	command.Env = append(os.Environ(),
		"ZNS_ENV=sandbox", "ZNS_SYNTHETIC_ONLY=true",
		"ZNS_DATABASE__URL="+os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_"+role+"_URL"),
		"ZNS_CONFIG_FILE=/workspace/docs/sandbox/fqa-stands/registration/runtime.yaml",
		"TELEGRAM_TOKEN=synthetic-only", "REGISTRATION_CLOCK_FILE="+settings.File,
		"REGISTRATION_CLOCK_INSTALLATION="+settings.Installation, "REGISTRATION_CLOCK_CASE="+settings.Case,
		"REGISTRATION_CLOCK_DATABASE_ADDRESS="+settings.DatabaseAddress, "REGISTRATION_CLOCK_ANCHOR="+settings.Anchor)
	raw, err := command.CombinedOutput()
	if err != nil {
		return registrationclock.State{}, err
	}
	state, err := registrationclock.Decode(bytes.TrimSpace(raw))
	if err == nil {
		err = state.Validate(settings)
	}
	return state, err
}

func operatorPrivateDenials(
	t *testing.T,
	admin, owner, operator *pgxpool.Pool,
	settings registrationclock.Settings,
	initial registrationclock.State,
) {
	t.Helper()
	cfg := config.Config{Env: "sandbox", SyntheticOnly: true}
	before, err := os.ReadFile(settings.File)
	require.NoError(t, err)
	for _, denied := range []struct {
		db     *pgxpool.Pool
		change RegistrationClockChange
	}{
		{operator, RegistrationClockChange{Action: "init"}},
		{owner, RegistrationClockChange{Action: "read"}},
		{admin, RegistrationClockChange{Action: "read"}},
	} {
		_, err = ApplyRegistrationClock(t.Context(), denied.db, cfg, settings, denied.change)
		require.Error(t, err)
	}
	for _, scenario := range []struct{ change, restore string }{
		{`UPDATE core.users SET telegram_id=404 WHERE id='visitor'`, `UPDATE core.users SET telegram_id=303 WHERE id='visitor'`},
		{`INSERT INTO core.users(id,telegram_id,name) VALUES('fourth',404,'synthetic')`, `DELETE FROM core.users WHERE id='fourth'`},
		{`ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO postgres`, `ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO zns_app`},
		{`ALTER TABLE public.zns_sandbox_fixtures OWNER TO postgres`, `ALTER TABLE public.zns_sandbox_fixtures OWNER TO zns_app`},
		{`ALTER TABLE core.users OWNER TO postgres`, `ALTER TABLE core.users OWNER TO zns_app`},
		{`REVOKE SELECT ON public.zns_sandbox_fixtures FROM zns_app`, `GRANT SELECT ON public.zns_sandbox_fixtures TO zns_app`},
		{`REVOKE INSERT ON public.zns_sandbox_fixtures FROM zns_app`, `GRANT INSERT ON public.zns_sandbox_fixtures TO zns_app`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='product-v1'`, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES('product-v1')`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='product-passport-v1'`, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES('product-passport-v1')`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='registration-fqa-v1'`, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES('registration-fqa-v1')`},
		{`DELETE FROM public.zns_sandbox_fixtures WHERE name='registration-clock:010400000204:c-registration-clock-20261001-v1'`,
			`INSERT INTO public.zns_sandbox_fixtures(name) VALUES('registration-clock:010400000204:c-registration-clock-20261001-v1')`},
		{`GRANT zns_app TO zns_registration_operator`, `REVOKE zns_app FROM zns_registration_operator`},
		{`ALTER ROLE zns_registration_operator SUPERUSER`, `ALTER ROLE zns_registration_operator NOSUPERUSER`},
		{`ALTER ROLE zns_registration_operator CREATEDB`, `ALTER ROLE zns_registration_operator NOCREATEDB`},
		{`ALTER ROLE zns_registration_operator CREATEROLE`, `ALTER ROLE zns_registration_operator NOCREATEROLE`},
		{`ALTER ROLE zns_registration_operator REPLICATION`, `ALTER ROLE zns_registration_operator NOREPLICATION`},
		{`ALTER ROLE zns_registration_operator BYPASSRLS`, `ALTER ROLE zns_registration_operator NOBYPASSRLS`},
	} {
		_, err = admin.Exec(t.Context(), scenario.change)
		require.NoError(t, err)
		_, rejected := ApplyRegistrationClock(t.Context(), operator, cfg, settings,
			RegistrationClockChange{Action: "advance", ExpectedRevision: 1, Target: initial.Current.Add(time.Minute)})
		_, restoreErr := admin.Exec(t.Context(), scenario.restore)
		require.NoError(t, restoreErr)
		require.Error(t, rejected, scenario.change)
		after, readErr := os.ReadFile(settings.File)
		require.NoError(t, readErr)
		assert.Equal(t, before, after, "denied guard cannot touch clock", scenario.change)
	}
	_, err = operator.Exec(
		t.Context(),
		`INSERT INTO public.zns_sandbox_fixtures(name) VALUES('operator-forged-clock-marker')`,
	)
	require.Error(t, err)
}

func operatorCLIInvalidTargets(t *testing.T, settings registrationclock.Settings, current registrationclock.State) {
	t.Helper()
	for _, target := range []string{"2030-01-01T00:00:00Z", "2026-10-01T00:02:00.000001001Z",
		"2026-10-01T00:02:00.000001+01:00", "2026-10-01T00:02:00.000001+00:00", settings.Anchor} {
		_, err := operatorCLI(
			t,
			"OPERATOR",
			settings,
			"-action",
			"advance",
			"-expected-revision",
			"2",
			"-target",
			target,
		)
		require.Error(t, err, target)
	}
	_, err := operatorCLI(t, "OPERATOR", settings, "-action", "advance", "-expected-revision", "1",
		"-target", current.Current.Add(time.Minute).Format(time.RFC3339Nano))
	require.Error(t, err)
	_, err = operatorCLI(t, "OWNER", settings, "-action", "init")
	require.Error(t, err, "init cannot reset an advanced clock")
	read, err := operatorCLI(t, "OPERATOR", settings, "-action", "read")
	require.NoError(t, err)
	assert.Equal(t, current.Digest(), read.Digest())
}

func operatorFixtureSerialization(t *testing.T, owner, operator *pgxpool.Pool, settings registrationclock.Settings) {
	t.Helper()
	barrier, err := owner.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(t.Context()) }()
	_, err = barrier.Exec(t.Context(), `SELECT pg_advisory_xact_lock(918431003)`)
	require.NoError(t, err)
	blocked, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = ApplyRegistrationClock(blocked, operator, config.Config{Env: "sandbox", SyntheticOnly: true}, settings,
		RegistrationClockChange{Action: "read"})
	require.Error(t, err, "clock control must honor the genuine fixture advisory barrier")
	require.NoError(t, barrier.Commit(t.Context()))
	_, err = ApplyRegistrationFixture(t.Context(), operator,
		RegistrationFixture{Stand: RegistrationFixtureStand, Action: "grant-payment-b"})
	require.NoError(t, err)
	_, err = ApplyRegistrationClock(t.Context(), operator, config.Config{Env: "sandbox", SyntheticOnly: true}, settings,
		RegistrationClockChange{Action: "read"})
	require.NoError(t, err)
}

func operatorManagedInventory(t *testing.T, owner, operator *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()
	inventory, err := pgx.Connect(ctx, os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_INVENTORY_URL"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inventory.Close(context.WithoutCancel(ctx))) })
	connection, err := operator.Acquire(ctx)
	require.NoError(t, err)
	defer connection.Release()
	reader := replacement.PostgresSessions{Conn: inventory, Roles: []string{"zns_app", "zns_meter"}}
	names, err := reader.Names(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, names, "owner sessions must remain in the managed inventory")
	owner.Close()
	names, err = reader.Names(ctx)
	require.NoError(t, err)
	assert.Empty(t, names, "held private clock/fixture operator session is outside managed inventory")
}

// Run separately as UID1000 with the complete clock volume mounted read-only.
func TestRegistrationClockOperatorReadOnlyReader(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" || os.Getenv("REGISTRATION_CLOCK_OPERATOR_TEST_READER_ONLY") != "1" {
		t.Skip("separate actual read-only reader mount required")
	}
	require.Equal(t, 1000, os.Geteuid())
	settings := operatorTestSettings()
	before, err := os.ReadFile(settings.File)
	require.NoError(t, err)
	state, err := readOperatorClock(settings.File, settings)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, state.Revision, uint64(2), "reader observes the trusted writer's persisted advance")
	writable, err := os.OpenFile(settings.File, os.O_WRONLY, 0o600)
	if writable != nil {
		require.NoError(t, writable.Close())
	}
	require.Error(t, err, "in-place write unavailable")
	err = os.WriteFile(filepath.Join(filepath.Dir(settings.File), "reader-created"), []byte("denied"), 0o600)
	require.Error(t, err, "whole directory prevents new entries")
	err = os.Remove(settings.File)
	require.Error(t, err, "whole directory prevents unlink")
	err = os.Rename(settings.File, settings.File+"-reader-renamed")
	require.Error(t, err, "whole directory prevents replacement")
	after, err := os.ReadFile(settings.File)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
