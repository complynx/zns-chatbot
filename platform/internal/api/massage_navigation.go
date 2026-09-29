package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func massageNavigationRoutes(mux *http.ServeMux, service massage.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/massage/slots/page", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		length, err := strconv.Atoi(query.Get("length"))
		if err != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.SlotsPage(
			r.Context(),
			requestOwner(r),
			query.Get("event"),
			query.Get("party"),
			length,
			query.Get("cursor"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/providers/{provider}/detail", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		value, err := service.ProviderDetail(
			r.Context(),
			requestOwner(r),
			query.Get("event"),
			r.PathValue("provider"),
			query.Get("cursor"),
		)
		respond(logger, w, value, err)
	})
}
