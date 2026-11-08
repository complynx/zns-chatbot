package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/descriptor"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/observability"
	"github.com/complynx/zns-chatbot/internal/symboldescribe"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

// Descriptor failure remains isolated, as it was with a separate service.
func waitApplication(ctx context.Context, cancel context.CancelFunc,
	engine, descriptorFailures, servers <-chan error, logger *slog.Logger,
) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case failure := <-engine:
			cancel()
			return fmt.Errorf("task worker stopped: %w", failure)
		case failure := <-descriptorFailures:
			logger.ErrorContext(ctx, "descriptor worker stopped", "error", failure)
			descriptorFailures = nil
		case failure := <-servers:
			cancel()
			if failure != nil && !errors.Is(failure, http.ErrServerClosed) {
				return errors.New("HTTP server stopped unexpectedly")
			}
			return nil
		}
	}
}

//nolint:nilnil // A nil worker intentionally disables this optional background service.
func embeddedDescriptor(c config.Config, store *db.Store, obs *observability.Telemetry) (*descriptor.Worker, error) {
	if !c.Descriptor.Enabled {
		if obs != nil {
			return nil, obs.ObserveDescriptor(false, nil)
		}
		return nil, nil
	}
	h := &http.Client{Timeout: c.HTTPTimeout}
	machine, err := identity.NewMachineTokenSource(identity.MachineOptions{
		Issuer: c.Identity.Issuer, OrganizationID: c.Identity.OrganizationID,
		Audience: c.Identity.Audience, Subject: c.Descriptor.Subject,
		Credentials: identity.ClientCredentials{ID: c.Descriptor.ClientID, Secret: c.Descriptor.ClientSecret},
	}, h)
	if err != nil {
		return nil, err
	}
	tg, err := telegram.New(c.Telegram.BaseURL, c.Telegram.Token, h)
	if err != nil {
		return nil, err
	}
	lc, err := loom.New(c.Loom.BaseURL, &http.Client{Timeout: c.HTTPTimeout,
		Transport: propagationTransport{base: http.DefaultTransport, propagator: obs.Propagator()}})
	if err != nil {
		return nil, err
	}
	worker, err := descriptor.New(descriptor.Options{Store: store, BotID: c.Telegram.BotID,
		Flow: symboldescribe.Flow{Telegram: tg, Loom: lc, Tracer: obs.Tracer()}, Machine: machine,
		Journal: c.Descriptor.Journal, Concurrency: c.Descriptor.Concurrency})
	if err != nil {
		return nil, err
	}
	if err = obs.ObserveDescriptor(true, worker.Snapshot); err != nil {
		return nil, err
	}
	return worker, nil
}
