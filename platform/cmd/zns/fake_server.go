package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

// Only the external fake can opt into the synthetic pre-header connection budget.
// All other modes keep their existing serve/serveListener path and settings.
func serveFake(
	ctx context.Context,
	fake *sandbox.Fake,
	handler http.Handler,
	logger *slog.Logger,
	cfg config.Config,
) error {
	listener, err := (&net.ListenConfig{}).Listen(
		ctx,
		"tcp",
		net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
	)
	if err != nil {
		return err
	}
	defer listener.Close()
	return serveListener(ctx, fake.DelayListener(ctx, listener), handler, logger, cfg)
}
