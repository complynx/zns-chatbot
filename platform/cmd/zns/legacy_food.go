package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func startFoodMaintenance(ctx context.Context, service legacyfood.Service, logger *slog.Logger) (func(), error) {
	if err := service.QueueReminders(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(foodReminderInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := service.QueueReminders(ctx); err != nil {
					logger.WarnContext(ctx, "food reminder scan pending", "error", err)
				}
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

const foodReminderInterval = 30 * time.Second

func registerMiniAppAliases(mux *http.ServeMux, handler http.Handler) {
	for _, path := range []string{"/massage_timetable", "/menu"} {
		mux.Handle(path, handler)
	}
}
