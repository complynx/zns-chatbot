package integration_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/replacement"
	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

// This engine controls process observations only. PostgreSQL sessions are real;
// the test does not claim physical Docker shutdown from these observations.
type replacementAdmissionEngine struct {
	inventory []replacement.Container
	created   int
	start     func() error
}

func (e *replacementAdmissionEngine) Identity(context.Context) (string, error) {
	return "test-daemon", nil
}
func (e *replacementAdmissionEngine) Inventory(context.Context) ([]replacement.Container, error) {
	return e.inventory, nil
}

func (e *replacementAdmissionEngine) Create(
	_ context.Context,
	instance runtimeapp.Instance,
) ([]replacement.Container, error) {
	e.created++
	for _, component := range replacement.Components() {
		e.inventory = append(e.inventory, replacement.Container{ID: component, Component: component,
			Launch: instance.Launch, Image: "test-image", Created: instance.Launch})
	}
	return e.inventory, nil
}
func (e *replacementAdmissionEngine) Start(context.Context, []replacement.Container) error {
	for i := range e.inventory {
		e.inventory[i].Running, e.inventory[i].PID, e.inventory[i].Health = true, 1, "healthy"
	}
	return e.start()
}
func (e *replacementAdmissionEngine) Stop(context.Context, []replacement.Container) error {
	for i := range e.inventory {
		e.inventory[i].Running, e.inventory[i].PID = false, 0
	}
	return nil
}
func (e *replacementAdmissionEngine) Remove(context.Context, []replacement.Container) error {
	e.inventory = nil
	return nil
}

type replacementAdmissionJournal struct {
	journal replacement.FileJournal
	running func() error
}

func (j replacementAdmissionJournal) Load() (replacement.Ledger, error) { return j.journal.Load() }

func (j replacementAdmissionJournal) Save(ledger replacement.Ledger) error {
	if err := j.journal.Save(ledger); err != nil {
		return err
	}
	if ledger.State == replacement.StateRunning {
		return j.running()
	}
	return nil
}

func TestRuntimeReplacementActiveAdmissionLoss(t *testing.T) {
	t.Parallel()
	db := database(t)
	role := "synthetic_qa_replacement_" + strings.TrimPrefix(db.Config().ConnConfig.Database, "synthetic_qa_zns_")
	quoted := pgx.Identifier{role}.Sanitize()
	_, err := db.Exec(t.Context(), "CREATE ROLE "+quoted+" LOGIN PASSWORD 'synthetic-only-replacement'")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := db.Exec(context.WithoutCancel(t.Context()), "DROP ROLE "+quoted)
		require.NoError(t, dropErr)
	})
	config := db.Config().ConnConfig.Copy()
	config.User, config.Password = role, "synthetic-only-replacement"
	instance := runtimeapp.Instance{Installation: "abcdef012345", Launch: "123456789012345678901234"}
	app, err := instance.ApplicationName("app")
	require.NoError(t, err)
	admit, err := instance.ApplicationName("admit")
	require.NoError(t, err)
	dsn := (&url.URL{Scheme: "postgres", User: url.UserPassword(config.User, config.Password),
		Host: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), Path: config.Database, RawQuery: "sslmode=disable"}).String()
	inventory, err := pgx.ConnectConfig(t.Context(), db.Config().ConnConfig.Copy())
	require.NoError(t, err)
	defer inventory.Close(context.WithoutCancel(t.Context()))
	sessions := replacement.PostgresSessions{Conn: inventory, Roles: []string{role}}
	refreshInventory := func() {
		if !inventory.IsClosed() {
			return
		}
		inventory, err = pgx.ConnectConfig(t.Context(), db.Config().ConnConfig.Copy())
		require.NoError(t, err)
		sessions.Conn = inventory
		fresh := inventory
		t.Cleanup(func() { require.NoError(t, fresh.Close(context.WithoutCancel(t.Context()))) })
	}
	ctx, cancel := context.WithTimeout(t.Context(), admissionTestWait)
	defer cancel()
	engine := &replacementAdmissionEngine{}
	var oldPool *pgxpool.Pool
	var transaction pgx.Tx
	var admission *runtimeapp.Admission
	engine.start = func() error {
		if engine.created > 1 {
			cancel()
			return nil
		}
		oldPool, err = store.OpenNamed(ctx, dsn, app)
		if err != nil {
			return err
		}
		transaction, err = oldPool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = transaction.Exec(ctx, "SELECT 1"); err != nil {
			return err
		}
		config.RuntimeParams["application_name"] = admit
		admission, err = runtimeapp.Acquire(ctx, config, runtimeapp.App)
		return err
	}
	defer func() {
		if transaction != nil {
			_ = transaction.Rollback(context.WithoutCancel(t.Context()))
		}
		if oldPool != nil {
			oldPool.Close()
		}
		if admission != nil {
			_ = admission.Close(context.WithoutCancel(t.Context()))
		}
	}()
	journal := replacementAdmissionJournal{
		journal: replacement.FileJournal{Directory: t.TempDir()},
		running: func() error {
			_, terminateErr := db.Exec(
				ctx,
				"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1",
				admit,
			)
			return terminateErr
		},
	}
	coordinator := replacement.Coordinator{Engine: engine, Sessions: &sessions, Journal: journal,
		Installation: instance.Installation, Host: "test-host", StopTimeout: time.Second,
		VerifyTimeout: 50 * time.Millisecond, PollInterval: time.Millisecond, ReadyTimeout: time.Second,
		NewLaunch: func() (string, error) { return instance.Launch, nil }}
	err = coordinator.Run(ctx)
	require.ErrorIs(t, err, replacement.ErrStopped)
	require.ErrorIs(t, err, replacement.ErrDeadline)
	require.NoError(t, ctx.Err(), "the active monitor must detect loss before the enclosing test deadline")
	refreshInventory()
	ledger, err := journal.Load()
	require.NoError(t, err)
	require.Equal(t, replacement.StateBlocked, ledger.State)
	names, err := sessions.Names(ctx)
	require.NoError(t, err)
	require.Contains(t, names, app)
	require.NotContains(t, names, admit)
	require.Equal(t, 1, engine.created)
	require.ErrorIs(t, coordinator.Run(ctx), replacement.ErrDeadline)
	refreshInventory()
	require.Equal(t, 1, engine.created, "live old pool must block replacement creation")
	require.NoError(t, transaction.Rollback(ctx))
	transaction = nil
	oldPool.Close()
	oldPool = nil
	instance.Launch = "223456789012345678901234"
	require.NoError(t, coordinator.Run(ctx))
	require.Equal(t, 2, engine.created)
	names, err = sessions.Names(context.WithoutCancel(t.Context()))
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestRuntimeReplacementSessionInventoryIncludesOldTransaction(t *testing.T) {
	t.Parallel()
	db := database(t)
	role := "synthetic_qa_replacement_" + db.Config().ConnConfig.Database[len("synthetic_qa_zns_"):]
	quoted := pgx.Identifier{role}.Sanitize()
	_, err := db.Exec(t.Context(), "CREATE ROLE "+quoted+" LOGIN PASSWORD 'synthetic-only-replacement'")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := db.Exec(context.WithoutCancel(t.Context()), "DROP ROLE "+quoted)
		require.NoError(t, dropErr)
	})
	config := db.Config().ConnConfig.Copy()
	config.User = role
	config.Password = "synthetic-only-replacement"
	instance := runtimeapp.Instance{Installation: "abcdef012345", Launch: "123456789012345678901234"}
	name, err := instance.ApplicationName("app")
	require.NoError(t, err)
	// Only disposable synthetic credentials enter this connection.
	dsn := (&url.URL{Scheme: "postgres", User: url.UserPassword(config.User, config.Password),
		Host: net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), Path: config.Database, RawQuery: "sslmode=disable"}).String()
	old, err := store.OpenNamed(t.Context(), dsn, name)
	require.NoError(t, err)
	defer old.Close()
	transaction, err := old.Begin(t.Context())
	require.NoError(t, err)
	defer transaction.Rollback(context.WithoutCancel(t.Context()))
	_, err = transaction.Exec(t.Context(), "SELECT 1")
	require.NoError(t, err)
	inventory, err := pgx.ConnectConfig(t.Context(), db.Config().ConnConfig.Copy())
	require.NoError(t, err)
	defer inventory.Close(context.WithoutCancel(t.Context()))
	sessions := replacement.PostgresSessions{Conn: inventory, Roles: []string{role}}
	names, err := sessions.Names(t.Context())
	require.NoError(t, err)
	require.Contains(t, names, name, "dedicated admission loss must not hide a pooled old transaction")
	_, err = transaction.Exec(t.Context(), "SET application_name='unknown_owner'")
	require.NoError(t, err)
	names, err = sessions.Names(t.Context())
	require.NoError(t, err)
	require.Contains(t, names, "unknown_owner", "inventory must not filter unknown sessions away")
	require.NoError(t, transaction.Rollback(t.Context()))
	old.Close()
	require.Eventually(t, func() bool {
		active, readErr := sessions.Names(t.Context())
		return readErr == nil && len(active) == 0
	}, admissionTestWait, admissionTestWait/100)
}

type replacementPhysicalEngine struct {
	replacement.Docker

	sessions  replacement.PostgresSessions
	cancel    context.CancelFunc
	creations int
}

func (e *replacementPhysicalEngine) Create(
	ctx context.Context,
	instance runtimeapp.Instance,
) ([]replacement.Container, error) {
	names, err := e.sessions.Names(ctx)
	if err != nil || len(names) != 0 {
		return nil, replacement.ErrUnknown
	}
	inventory, err := e.Docker.Inventory(ctx)
	if err != nil || len(inventory) != 0 {
		return nil, replacement.ErrUnknown
	}
	e.creations++
	return e.Docker.Create(ctx, instance)
}

func (e *replacementPhysicalEngine) Start(ctx context.Context, containers []replacement.Container) error {
	if err := e.Docker.Start(ctx, containers); err != nil {
		return err
	}
	e.cancel()
	return nil
}

type replacementDiagnosticCommand struct {
	t *testing.T
}

func (c replacementDiagnosticCommand) Run(ctx context.Context, args, environment []string) ([]byte, error) {
	started := time.Now()
	output, err := (replacement.DockerCommand{}).Run(ctx, args, environment)
	c.t.Logf("Docker operation=%s elapsed=%s error=%v context=%v", args[0], time.Since(started), err, ctx.Err())
	return output, err
}

func TestRuntimeReplacementPhysicalBarrier(t *testing.T) {
	t.Parallel()
	if os.Getenv("ZNS_REPLACEMENT_DOCKER_TEST") != "1" {
		t.Skip("requires explicitly owned synthetic Linux Docker stand")
	}
	image := os.Getenv("ZNS_REPLACEMENT_TEST_IMAGE")
	network := os.Getenv("ZNS_REPLACEMENT_TEST_NETWORK")
	host := os.Getenv("ZNS_REPLACEMENT_TEST_PG_HOST")
	require.Contains(t, image, "@sha256:")
	require.True(t, strings.HasPrefix(network, "synthetic-qa-zns-"))
	require.True(t, strings.HasPrefix(host, "synthetic-qa-zns-"))
	db := database(t)
	suffix := strings.TrimPrefix(db.Config().ConnConfig.Database, "synthetic_qa_zns_")
	role := "synthetic_qa_replacement_" + suffix
	password := "synthetic-only-replacement"
	quoted := pgx.Identifier{role}.Sanitize()
	_, err := db.Exec(t.Context(), "CREATE ROLE "+quoted+" LOGIN PASSWORD '"+password+"'")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := db.Exec(context.WithoutCancel(t.Context()), "DROP OWNED BY "+quoted)
		require.NoError(t, dropErr)
		_, dropErr = db.Exec(context.WithoutCancel(t.Context()), "DROP ROLE "+quoted)
		require.NoError(t, dropErr)
	})
	_, err = db.Exec(t.Context(), "CREATE TABLE public.replacement_effects(component text)")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), "GRANT INSERT ON public.replacement_effects TO "+quoted)
	require.NoError(t, err)
	instance := runtimeapp.Instance{Installation: suffix[:12], Launch: "111111111111111111111111"}
	file := filepath.Join(t.TempDir(), "compose.json")
	writeReplacementPhysicalCompose(t, file, image, network, host, role, password, db.Config().ConnConfig.Database)
	docker := replacement.Docker{Command: replacementDiagnosticCommand{t: t}, Installation: instance.Installation,
		Project: "synthetic-qa-zns-replacement-" + suffix, Files: []string{file}, ManagedRoles: []string{role},
		Database: db.Config().ConnConfig.Database, DatabaseHost: host}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), admissionTestWait)
		defer stop()
		current, readErr := docker.Inventory(cleanup)
		require.NoError(t, readErr)
		require.NoError(t, docker.Kill(cleanup, current))
		require.NoError(t, docker.Remove(cleanup, current))
	})
	requireReplacementDependenciesRejected(t, docker, instance, file, image)
	old, err := docker.Create(t.Context(), instance)
	require.NoError(t, err)
	require.NoError(t, docker.Start(t.Context(), old))
	appName, err := instance.ApplicationName("app")
	require.NoError(t, err)
	admitName, err := instance.ApplicationName("admit")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var count int
		queryErr := db.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name=ANY($1::text[])", []string{appName, admitName}).
			Scan(&count)
		return queryErr == nil && count == 2
	}, admissionTestWait, 20*time.Millisecond)
	// Terminate only this fixture's dedicated admission backend; the old writer is separate.
	_, err = db.Exec(
		t.Context(),
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1",
		admitName,
	)
	require.NoError(t, err)
	inventory, err := pgx.ConnectConfig(t.Context(), db.Config().ConnConfig.Copy())
	require.NoError(t, err)
	defer inventory.Close(context.WithoutCancel(t.Context()))
	sessions := replacement.PostgresSessions{Conn: inventory, Roles: []string{role}}
	names, err := sessions.Names(t.Context())
	require.NoError(t, err)
	require.Contains(t, names, appName)
	daemon, err := docker.Identity(t.Context())
	require.NoError(t, err)
	journal := replacement.FileJournal{Directory: t.TempDir()}
	require.NoError(
		t,
		journal.Save(replacement.Ledger{Version: 1, Installation: instance.Installation, Host: "synthetic-host",
			Daemon: daemon, Generation: 1, Launch: instance.Launch, State: replacement.StateRunning, Containers: old}),
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine := &replacementPhysicalEngine{Docker: docker, sessions: sessions, cancel: cancel}
	coordinator := replacement.Coordinator{
		Engine:        engine,
		Sessions:      sessions,
		Journal:       journal,
		Installation:  instance.Installation,
		Host:          "synthetic-host",
		StopTimeout:   5 * time.Second,
		VerifyTimeout: 5 * time.Second,
		PollInterval:  20 * time.Millisecond,
		NewLaunch:     func() (string, error) { return "222222222222222222222222", nil },
	}

	// A stopped client with a long server query can leave its backend alive.
	// The barrier must block until that exact fixture session is gone.
	runErr := coordinator.Run(ctx)
	require.Error(t, runErr)
	require.Zero(t, engine.creations)
	blocked, err := journal.Load()
	require.NoError(t, err)
	require.Equal(t, replacement.StateBlocked, blocked.State)
	retired, err := docker.Inventory(t.Context())
	require.NoError(t, err)
	require.Len(t, retired, len(replacement.Components()))
	for _, item := range retired {
		require.False(t, item.Running)
		require.False(t, item.Restarting)
		require.False(t, item.Paused)
		require.Zero(t, item.PID)
	}

	names, err = sessions.Names(t.Context())
	require.NoError(t, err)
	require.Contains(t, names, appName)
	_, err = db.Exec(
		t.Context(),
		"SELECT pg_cancel_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND usename=$1 AND application_name=ANY($2::text[])",
		role,
		names,
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, readErr := sessions.Names(t.Context())
		return readErr == nil && len(current) == 0
	}, admissionTestWait, 20*time.Millisecond)
	// New synthetic sessions detect a stopped client while a long query runs.
	_, err = db.Exec(t.Context(), "ALTER ROLE "+quoted+" SET client_connection_check_interval='100ms'")
	require.NoError(t, err)
	requireReplacementConnectionChecks(t, db.Config().ConnConfig, role, password)
	require.NoError(t, coordinator.Run(ctx))
	require.Equal(t, 1, engine.creations, "replacement must start only after old process and DB session disappearance")
	names, err = sessions.Names(t.Context())
	require.NoError(t, err)
	require.Empty(t, names)
	stopped, err := journal.Load()
	require.NoError(t, err)
	require.Equal(t, replacement.StateStopped, stopped.State)
}

func writeReplacementPhysicalCompose(
	t *testing.T,
	path string,
	image, network, host, role, password, databaseName string,
) {
	t.Helper()
	script := `children=""
trap 'for child in $children; do kill "$child" 2>/dev/null || :; done; wait; exit 0' TERM
touch /tmp/started
if [ "$COMPONENT" = app ] || [ "$COMPONENT" = media-broker ]; then
 tag=media
 if [ "$COMPONENT" = app ]; then
  tag=app
  PGAPPNAME="zns:$ZNS_INSTALLATION_ID:$ZNS_LAUNCH_ID:admit" psql "$DATABASE_URL" -c 'SELECT pg_advisory_lock(918274,1); SELECT pg_sleep(120)' &
  children="$children $!"
 fi
 PGAPPNAME="zns:$ZNS_INSTALLATION_ID:$ZNS_LAUNCH_ID:$tag" psql "$DATABASE_URL" -c "BEGIN; INSERT INTO public.replacement_effects VALUES ('$COMPONENT'); SELECT pg_sleep(120); COMMIT;" &
 children="$children $!"
fi
sleep 120 &
children="$children $!"
wait
`
	script = strings.ReplaceAll(script, "$", "$$")
	dsn := "postgres://" + role + ":" + password + "@" + host + ":5432/" + databaseName + "?sslmode=disable"
	services := map[string]any{}
	for _, component := range replacement.Components() {
		services[component] = map[string]any{
			"image":      image,
			"entrypoint": []string{"/bin/sh", "-c", script, "--"},
			"command":    []string{"app"},
			"restart":    "no",
			"cap_drop":   []string{"ALL"},
			"networks":   []string{"stand"},
			"environment": map[string]string{"COMPONENT": component, "DATABASE_URL": dsn, "ZNS_DATABASE__URL": dsn,
				"ZNS_INSTALLATION_ID": "${ZNS_INSTALLATION_ID}", "ZNS_LAUNCH_ID": "${ZNS_LAUNCH_ID}"},
			"labels": map[string]string{replacement.LabelInstallation: "${ZNS_INSTALLATION_ID}",
				replacement.LabelLaunch: "${ZNS_LAUNCH_ID}", replacement.LabelComponent: component},
			"healthcheck": map[string]any{
				"test":     []string{"CMD-SHELL", "test -f /tmp/started"},
				"interval": "1s",
				"timeout":  "1s",
				"retries":  3,
			},
		}
	}
	document := map[string]any{
		"services": services,
		"networks": map[string]any{"stand": map[string]any{"external": true, "name": network}},
	}
	data, err := json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
}

func requireReplacementDependenciesRejected(
	t *testing.T,
	docker replacement.Docker,
	instance runtimeapp.Instance,
	path, image string,
) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var project map[string]any
	require.NoError(t, json.Unmarshal(data, &project))
	services := project["services"].(map[string]any)
	services["app"].(map[string]any)["depends_on"] = map[string]any{
		"postgres": map[string]any{"condition": "service_started"},
	}
	services["postgres"] = map[string]any{"image": image}
	unsafeData, err := json.Marshal(project)
	require.NoError(t, err)
	unsafePath := filepath.Join(t.TempDir(), "unsafe-compose.json")
	require.NoError(t, os.WriteFile(unsafePath, unsafeData, 0600))
	unsafeDocker := docker
	unsafeDocker.Files = []string{unsafePath}
	_, err = unsafeDocker.Create(t.Context(), instance)
	require.ErrorIs(
		t,
		err,
		replacement.ErrUnknown,
		"resolved dependency must be rejected before any container creation",
	)
	output, err := (replacement.DockerCommand{}).Run(
		t.Context(),
		[]string{"ps", "-aq", "--filter", "label=com.docker.compose.project=" + docker.Project},
		nil,
	)
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(string(output)), "PostgreSQL and runtime containers must remain uncreated")
}

func requireReplacementConnectionChecks(t *testing.T, config *pgx.ConnConfig, role, password string) {
	t.Helper()
	limited := config.Copy()
	limited.User, limited.Password = role, password
	conn, err := pgx.ConnectConfig(t.Context(), limited)
	require.NoError(t, err)
	defer conn.Close(context.WithoutCancel(t.Context()))
	var user, setting, unit, settingContext string
	require.NoError(t, conn.QueryRow(t.Context(),
		"SELECT current_user,setting,unit,context FROM pg_settings WHERE name='client_connection_check_interval'").
		Scan(&user, &setting, &unit, &settingContext))
	require.Equal(t, role, user)
	require.Equal(t, "100", setting)
	require.Equal(t, "ms", unit)
	require.Equal(t, "user", settingContext)
	t.Logf(
		"Limited runtime role verified client_connection_check_interval=%s%s context=%s",
		setting,
		unit,
		settingContext,
	)
}
