package assistantsource

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const RefreshInterval = time.Hour

type Store interface {
	ConfigureSource(context.Context, string, string) error
	ReplaceSource(context.Context, string, string, string, []string) error
	SourceFailure(context.Context, string, string, string) error
}

type Runner struct {
	Store      Store
	Logger     *slog.Logger
	Drive      *Drive
	StaticPath string
}

// Start configures both source identities before consumers start. Static QA is
// loaded once; Drive refresh starts immediately and repeats hourly until stopped.
// A startup database failure is returned. A later refresh database failure is
// reported once through onFatal and ends the refresh loop; provider failures
// stay optional and retry on the next interval.
func (r Runner) Start(ctx context.Context, onFatal func(error)) (func(), error) {
	staticIdentity, err := r.configure(ctx)
	if err != nil {
		return nil, err
	}
	if staticIdentity != "" {
		if err = r.loadStatic(ctx, staticIdentity); err != nil {
			return nil, err
		}
	}
	runContext, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.refreshLoop(runContext, onFatal)
	}()
	return func() { cancel(); <-done }, nil
}

// configure records both source identities and returns the static identity.
// A database failure returns the safe marker; other failures stay generic.
func (r Runner) configure(ctx context.Context) (string, error) {
	staticIdentity := ""
	if r.StaticPath != "" {
		absolute, err := filepath.Abs(r.StaticPath)
		if err != nil {
			return "", ErrInvalid
		}
		staticIdentity = Digest([]byte("static\x00" + absolute))
	}
	driveIdentity := ""
	if r.Drive != nil {
		driveIdentity = r.Drive.Identity()
	}
	identities := map[string]string{
		knowledge.AssistantQA:    staticIdentity,
		knowledge.AssistantAbout: driveIdentity,
	}
	for slot, identity := range identities {
		if err := r.Store.ConfigureSource(ctx, slot, identity); err != nil {
			if core.IsDatabaseFailure(err) {
				return "", core.ErrDatabase
			}
			return "", errors.New("assistant source configuration failed")
		}
	}
	return staticIdentity, nil
}

// refreshLoop refreshes Drive immediately and then every RefreshInterval until
// ctx ends. A database failure is reported once through onFatal and ends it.
func (r Runner) refreshLoop(ctx context.Context, onFatal func(error)) {
	if r.Drive == nil {
		return
	}
	if err := r.Refresh(ctx); err != nil {
		onFatal(err)
		return
	}
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Refresh(ctx); err != nil {
				onFatal(err)
				return
			}
		}
	}
}

// loadStatic returns only a safe database failure; file and parse failures are
// recorded as optional source failures.
func (r Runner) loadStatic(ctx context.Context, identity string) error {
	file, err := os.Open(r.StaticPath)
	if err != nil {
		return r.failure(ctx, knowledge.AssistantQA, identity, ErrFetch)
	}
	defer file.Close()
	data, err := readBounded(file)
	if err != nil {
		return r.failure(ctx, knowledge.AssistantQA, identity, err)
	}
	entries, err := ParseQA(data)
	if err != nil {
		return r.failure(ctx, knowledge.AssistantQA, identity, err)
	}
	return r.publish(ctx, knowledge.AssistantQA, identity, data, entries)
}

// Refresh returns only a safe database failure. A Drive failure keeps the last
// good content, records a finite code and returns nil.
func (r Runner) Refresh(ctx context.Context) error {
	if r.Drive == nil {
		return nil
	}
	data, err := r.Drive.Fetch(ctx)
	if err != nil {
		return r.failure(ctx, knowledge.AssistantAbout, r.Drive.Identity(), err)
	}
	return r.publish(ctx, knowledge.AssistantAbout, r.Drive.Identity(), data, []string{UnescapeMarkdown(string(data))})
}

func (r Runner) publish(ctx context.Context, slot, identity string, data []byte, texts []string) error {
	if err := r.Store.ReplaceSource(ctx, slot, identity, Digest(data), texts); err != nil {
		if core.IsDatabaseFailure(err) {
			r.warn(ctx, slot, "database_unavailable")
			return core.ErrDatabase
		}
		return r.failure(ctx, slot, identity, ErrFetch)
	}
	if r.Logger != nil {
		r.Logger.InfoContext(
			ctx,
			"assistant source refreshed",
			"source",
			slot,
			"bytes",
			len(data),
			"entries",
			len(texts),
		)
	}
	return nil
}

// failure records a finite code while retaining last-good content. It returns
// a safe database failure only when recording the code itself fails in SQL.
func (r Runner) failure(ctx context.Context, slot, identity string, err error) error {
	code := "fetch_failed"
	switch {
	case errors.Is(err, ErrInvalid):
		code = "invalid_source"
	case errors.Is(err, ErrLimit):
		code = "source_limit"
	case errors.Is(err, context.DeadlineExceeded):
		code = "source_timeout"
	case errors.Is(err, context.Canceled):
		code = "source_canceled"
	}
	var recordErr error
	if ctx.Err() == nil {
		recordErr = r.Store.SourceFailure(ctx, slot, identity, code)
	}
	r.warn(ctx, slot, code)
	if core.IsDatabaseFailure(recordErr) {
		return core.ErrDatabase
	}
	return nil
}

func (r Runner) warn(ctx context.Context, slot, code string) {
	if r.Logger != nil {
		r.Logger.WarnContext(ctx, "assistant source refresh failed", "source", slot, "code", code)
	}
}

func New(settings config.AssistantSources, store Store, logger *slog.Logger) (Runner, error) {
	runner := Runner{Store: store, Logger: logger, StaticPath: settings.StaticQAPath}
	if settings.AboutDocument != "" {
		drive, err := NewDrive(settings, nil)
		if err != nil {
			return runner, ErrInvalid
		}
		runner.Drive = drive
	}
	return runner, nil
}
