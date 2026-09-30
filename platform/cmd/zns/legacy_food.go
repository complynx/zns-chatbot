package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func startFoodMaintenance(
	ctx context.Context, service legacyfood.Service, logger *slog.Logger, onFatal func(error),
) (func(), error) {
	if err := service.QueueReminders(ctx); err != nil {
		return nil, err
	}
	return startTicks(ctx, foodReminderInterval, foodReminderTick(service, logger), onFatal), nil
}

// foodReminderTick returns only a safe positive database failure; ordinary
// scan failures are logged and retried on the next tick.
func foodReminderTick(service legacyfood.Service, logger *slog.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		err := service.QueueReminders(ctx)
		if fatal := databaseFatal(err); fatal != nil {
			return fatal
		}
		if err != nil {
			logger.WarnContext(ctx, "food reminder scan pending", "error", err)
		}
		return nil
	}
}

const foodReminderInterval = 30 * time.Second

func registerMiniAppAliases(mux *http.ServeMux, handler http.Handler) {
	for _, path := range []string{"/massage_timetable", "/menu"} {
		mux.Handle(path, handler)
	}
}
