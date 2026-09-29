package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passExportRoute(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/passes/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		body, err := service.Export(r.Context(), requestOwner(r))
		if err != nil {
			respondPassProfile(logger, w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="passes.xlsx"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "passes.xlsx", time.Time{}, bytes.NewReader(body))
	})
}
