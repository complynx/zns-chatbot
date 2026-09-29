package assistantsource

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/config"
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
func (r Runner) Start(ctx context.Context) (func(), error) {
	staticIdentity := ""
	if r.StaticPath != "" {
		absolute, err := filepath.Abs(r.StaticPath)
		if err != nil {
			return nil, ErrInvalid
		}
		staticIdentity = Digest([]byte("static\x00" + absolute))
	}
	driveIdentity := ""
	if r.Drive != nil {
		driveIdentity = r.Drive.Identity()
	}
	for slot, identity := range map[string]string{knowledge.AssistantQA: staticIdentity, knowledge.AssistantAbout: driveIdentity} {
		if err := r.Store.ConfigureSource(ctx, slot, identity); err != nil {
			return nil, errors.New("assistant source configuration failed")
		}
	}
	if staticIdentity != "" {
		r.loadStatic(ctx, staticIdentity)
	}
	runContext, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if r.Drive == nil {
			return
		}
		r.Refresh(runContext)
		ticker := time.NewTicker(RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runContext.Done():
				return
			case <-ticker.C:
				r.Refresh(runContext)
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

func (r Runner) loadStatic(ctx context.Context, identity string) {
	file, err := os.Open(r.StaticPath)
	if err != nil {
		r.failure(ctx, knowledge.AssistantQA, identity, ErrFetch)
		return
	}
	defer file.Close()
	data, err := readBounded(file)
	if err != nil {
		r.failure(ctx, knowledge.AssistantQA, identity, err)
		return
	}
	entries, err := ParseQA(data)
	if err != nil {
		r.failure(ctx, knowledge.AssistantQA, identity, err)
		return
	}
	r.publish(ctx, knowledge.AssistantQA, identity, data, entries)
}

func (r Runner) Refresh(ctx context.Context) {
	if r.Drive == nil {
		return
	}
	data, err := r.Drive.Fetch(ctx)
	if err != nil {
		r.failure(ctx, knowledge.AssistantAbout, r.Drive.Identity(), err)
		return
	}
	r.publish(ctx, knowledge.AssistantAbout, r.Drive.Identity(), data, []string{UnescapeMarkdown(string(data))})
}

func (r Runner) publish(ctx context.Context, slot, identity string, data []byte, texts []string) {
	if err := r.Store.ReplaceSource(ctx, slot, identity, Digest(data), texts); err != nil {
		r.failure(ctx, slot, identity, ErrFetch)
		return
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
}

func (r Runner) failure(ctx context.Context, slot, identity string, err error) {
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
	if ctx.Err() == nil {
		_ = r.Store.SourceFailure(ctx, slot, identity, code)
	}
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
