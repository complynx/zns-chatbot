package replacement

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

type observationStage string
type observationPredicate string

type observationFailure struct {
	stage      observationStage
	predicate  observationPredicate
	elapsed    time.Duration
	deadline   string
	containers int
	sessions   int
	admissions int
	cause      error
}

func (e *observationFailure) Error() string { return "replacement observation failed" }
func (e *observationFailure) Unwrap() error { return e.cause }

func observationError(ctx context.Context, start time.Time, stage observationStage,
	predicate observationPredicate, err error) *observationFailure {
	deadline := "none"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		deadline = "deadline_exceeded"
	} else if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		deadline = "canceled"
	} else if errors.Is(err, ErrDeadline) {
		deadline = "readiness"
	}
	return &observationFailure{stage: stage, predicate: predicate, elapsed: time.Since(start),
		deadline: deadline, containers: -1, sessions: -1, admissions: -1, cause: err}
}

func errorCategory(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrDeadline):
		return "readiness_deadline"
	case errors.Is(err, ErrStopped):
		return "runtime_stopped"
	case errors.Is(err, ErrUnknown):
		return "ownership_unknown"
	case errors.Is(err, ErrConfiguration):
		return "configuration"
	default:
		return "operation_failed"
	}
}

// recordFailure emits only fixed classifications, never underlying error text or identities.
func recordFailure(ctx context.Context, err error) {
	var failure *observationFailure
	if !errors.As(err, &failure) {
		failure = &observationFailure{stage: "monitor", predicate: "unclassified",
			deadline: "none", containers: -1, sessions: -1, admissions: -1}
	}
	slog.LogAttrs(ctx, slog.LevelError, "replacement monitor failure",
		slog.String("stage", string(failure.stage)), slog.String("predicate", string(failure.predicate)),
		slog.String("error_category", errorCategory(err)), slog.String("deadline_class", failure.deadline),
		slog.Int64("elapsed_ms", failure.elapsed.Milliseconds()), slog.Int("containers", failure.containers),
		slog.Int("sessions", failure.sessions), slog.Int("admissions", failure.admissions))
}

// Run retires any previous generation, starts one generation, and monitors the whole group.
// The service manager restarts Run after failure, through the same retirement barrier.
func (c *Coordinator) Run(ctx context.Context) error {
	c.DefaultBudgets()
	ledger, err := c.reconcile(ctx)
	if err != nil {
		return err
	}
	// Once ownership is known, signals must not interrupt bounded retirement.
	if err = c.cleanup(&ledger); err != nil {
		return errors.Join(ctx.Err(), err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = c.launch(ctx, &ledger); err != nil {
		return errors.Join(err, c.cleanup(&ledger))
	}
	monitorStart := time.Now()
	runErr := c.monitor(ctx, ledger)
	if runErr != nil {
		recordFailure(ctx, runErr)
		slog.LogAttrs(ctx, slog.LevelInfo, "replacement monitor duration",
			slog.Int64("elapsed_ms", time.Since(monitorStart).Milliseconds()))
	}
	cleanupStart := time.Now()
	stopErr := c.cleanup(&ledger)
	slog.LogAttrs(ctx, slog.LevelInfo, "replacement cleanup outcome",
		slog.String("error_category", errorCategory(stopErr)),
		slog.Int64("elapsed_ms", time.Since(cleanupStart).Milliseconds()))
	if errors.Is(runErr, context.Canceled) && stopErr == nil {
		return nil
	}
	return errors.Join(runErr, stopErr)
}

func (c *Coordinator) reconcile(ctx context.Context) (Ledger, error) {
	ledger, err := c.Journal.Load()
	if err != nil {
		return ledger, err
	}
	daemon, err := c.Engine.Identity(ctx)
	if err != nil || daemon == "" || c.Host == "" {
		return ledger, ErrUnknown
	}
	if ledger.Version == 0 {
		containers, inventoryErr := c.Engine.Inventory(ctx)
		if inventoryErr != nil || len(containers) != 0 {
			return ledger, ErrUnknown
		}
		ledger = Ledger{Version: 1, Installation: c.Installation, Host: c.Host, Daemon: daemon, State: StateStopped}
	} else if ledger.Installation != c.Installation || ledger.Host != c.Host || ledger.Daemon != daemon {
		return ledger, ErrUnknown
	}
	return ledger, nil
}

func (c *Coordinator) cleanup(ledger *Ledger) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.StopTimeout+c.VerifyTimeout)
	defer cancel()
	return c.retire(ctx, ledger)
}

func (c *Coordinator) retire(ctx context.Context, ledger *Ledger) error {
	known, err := c.knownInventory(ctx, *ledger)
	if err != nil {
		return c.block(ledger, err)
	}
	ledger.State = StateStopping
	if err = c.Journal.Save(*ledger); err != nil {
		return err
	}
	stopCtx, cancel := context.WithTimeout(ctx, c.StopTimeout)
	// Stop is a request, never completion proof. Verification follows even on timeout.
	_ = c.Engine.Stop(stopCtx, known)
	cancel()
	verifyCtx, finish := context.WithTimeout(ctx, c.VerifyTimeout)
	defer finish()
	if err = c.Engine.Kill(verifyCtx, known); err != nil {
		return c.block(ledger, err)
	}
	if err = c.waitStopped(verifyCtx, *ledger); err != nil {
		return c.block(ledger, err)
	}
	ledger.State = StateStopped
	if err = c.Journal.Save(*ledger); err != nil {
		return err
	}
	if err = c.Engine.Remove(verifyCtx, known); err != nil {
		return c.block(ledger, err)
	}
	ledger.Containers = nil
	return c.Journal.Save(*ledger)
}

func (c *Coordinator) knownInventory(ctx context.Context, ledger Ledger) ([]Container, error) {
	start := time.Now()
	current, err := c.Engine.Inventory(ctx)
	if err != nil {
		var classified *observationFailure
		if errors.As(err, &classified) {
			return nil, err
		}
		return nil, observationError(ctx, start, "docker_inventory", "operation", err)
	}
	for _, item := range current {
		index := slices.IndexFunc(ledger.Containers, func(old Container) bool {
			return item.ID == old.ID && item.Component == old.Component && item.Launch == old.Launch &&
				item.Image == old.Image && item.Created == old.Created
		})
		if index < 0 {
			return nil, observationError(ctx, start, "docker_identity", "unknown_container", ErrUnknown)
		}
	}
	return current, nil
}

func (c *Coordinator) waitStopped(ctx context.Context, ledger Ledger) error {
	for {
		inventory, err := c.knownInventory(ctx, ledger)
		if err != nil {
			return err
		}
		stopped := true
		for _, item := range inventory {
			if item.Running || item.Restarting || item.Paused || item.PID != 0 {
				stopped = false
			}
		}
		names, err := c.Sessions.Names(ctx)
		if err != nil {
			return err
		}
		if err = knownSessions(ledger, names); err != nil {
			return err
		}
		if stopped && len(names) == 0 {
			return nil
		}
		if err = c.pause(ctx); err != nil {
			return errors.Join(ErrDeadline, err)
		}
	}
}

func knownSessions(ledger Ledger, names []string) error {
	if len(names) == 0 {
		return nil
	}
	instance := runtimeapp.Instance{Installation: ledger.Installation, Launch: ledger.Launch}
	components := []string{componentApp, "admit", "media"}
	allowed := make([]string, 0, len(components))
	for _, component := range components {
		name, err := instance.ApplicationName(component)
		if err != nil {
			return ErrUnknown
		}
		allowed = append(allowed, name)
	}
	for _, name := range names {
		if !slices.Contains(allowed, name) {
			return ErrUnknown
		}
	}
	return nil
}

func (c *Coordinator) launch(ctx context.Context, ledger *Ledger) error {
	launch, err := c.NewLaunch()
	if err != nil {
		return err
	}
	instance := runtimeapp.Instance{Installation: c.Installation, Launch: launch}
	if err = instance.Validate(); err != nil || launch == ledger.Launch {
		return ErrConfiguration
	}
	ledger.Generation++
	if ledger.Generation == 0 {
		return ErrConfiguration
	}
	ledger.Launch, ledger.State = launch, StateStarting
	if err = c.Journal.Save(*ledger); err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, c.ReadyTimeout)
	defer cancel()
	containers, err := c.Engine.Create(startup, instance)
	if err != nil {
		return err
	}
	ledger.Containers = containers
	if err = c.Journal.Save(*ledger); err != nil {
		return err
	}
	return c.Engine.Start(startup, containers)
}

func (c *Coordinator) monitor(ctx context.Context, ledger Ledger) error {
	start := time.Now()
	readyDeadline := time.Now().Add(c.ReadyTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return observationError(ctx, start, "monitor", "context", err)
		}
		ready, err := c.observe(ctx, ledger)
		if err != nil {
			return err
		}
		if ready && ledger.State != StateRunning {
			ledger.State = StateRunning
			if err = c.Journal.Save(ledger); err != nil {
				return observationError(ctx, start, "journal", "save_running", err)
			}
		}
		if !ready && time.Now().After(readyDeadline) {
			return observationError(ctx, start, "readiness", "not_ready", ErrDeadline)
		}
		if err = c.pause(ctx); err != nil {
			return observationError(ctx, start, "monitor", "pause", err)
		}
	}
}

// observe checks one complete process and session snapshot without changing ownership.
func (c *Coordinator) observe(ctx context.Context, ledger Ledger) (bool, error) {
	start := time.Now()
	fail := func(stage observationStage, predicate observationPredicate, err error, containers, sessions, admissions int) (bool, error) {
		failure := observationError(ctx, start, stage, predicate, err)
		failure.containers, failure.sessions, failure.admissions = containers, sessions, admissions
		return false, failure
	}
	inventory, err := c.knownInventory(ctx, ledger)
	if err != nil {
		return false, err
	}
	if len(inventory) != len(Components()) {
		return fail("process", "container_count", ErrStopped, len(inventory), -1, -1)
	}
	ready := true
	for _, item := range inventory {
		if !item.Running || item.Restarting || item.Paused || item.Health == "unhealthy" {
			predicate := observationPredicate("not_running")
			switch {
			case item.Restarting:
				predicate = "restarting"
			case item.Paused:
				predicate = "paused"
			case item.Health == "unhealthy":
				predicate = "unhealthy"
			}
			return fail("process", predicate, ErrStopped, len(inventory), -1, -1)
		}
		if item.Component == componentApp && item.Health != "healthy" {
			ready = false
		}
	}
	names, err := c.Sessions.Names(ctx)
	if err != nil {
		var classified *observationFailure
		if errors.As(err, &classified) {
			classified.containers = len(inventory)
			return false, err
		}
		return fail("sessions", "operation", err, len(inventory), -1, -1)
	}
	if err = knownSessions(ledger, names); err != nil {
		return fail("sessions", "unknown_session", err, len(inventory), len(names), -1)
	}
	admit, err := (runtimeapp.Instance{Installation: ledger.Installation, Launch: ledger.Launch}).
		ApplicationName("admit")
	if err != nil {
		return fail("admission", "identity", ErrUnknown, len(inventory), len(names), -1)
	}
	admissions := 0
	for _, name := range names {
		if name == admit {
			admissions++
		}
	}
	if admissions > 1 {
		return fail("admission", "multiple", ErrUnknown, len(inventory), len(names), admissions)
	}
	if admissions == 0 {
		if ledger.State == StateRunning {
			return fail("admission", "missing_running", ErrStopped, len(inventory), len(names), admissions)
		}
		return false, nil
	}
	return ready, nil
}
func (c *Coordinator) block(ledger *Ledger, err error) error {
	ledger.State = StateBlocked
	return errors.Join(err, c.Journal.Save(*ledger))
}

func (c *Coordinator) pause(ctx context.Context) error {
	timer := time.NewTimer(c.PollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
