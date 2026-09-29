package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func exportRoute(mux *http.ServeMux, service orders.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/order-events/{event}/export", func(w http.ResponseWriter, r *http.Request) {
		body, err := service.Export(r.Context(), requestOwner(r), r.PathValue("event"))
		if err != nil {
			respond(logger, w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="orders.xlsx"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "orders.xlsx", time.Time{}, bytes.NewReader(body))
	})
}
