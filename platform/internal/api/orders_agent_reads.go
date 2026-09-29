package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func orderAgentReadRoutes(mux *http.ServeMux, service orders.Service, logger *slog.Logger) {
	for _, route := range []string{"catalog-read", "catalog-transport"} {
		mux.HandleFunc("GET /v1/order-events/{event}/"+route, func(w http.ResponseWriter, r *http.Request) {
			value, err := service.CatalogRead(r.Context(), requestOwner(r), r.PathValue("event"),
				r.URL.Query().Get("cursor"), route == "catalog-transport")
			respond(logger, w, value, err)
		})
	}
	mux.HandleFunc("GET /v1/order-events/{event}/history-recent", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.RecentHistory(r.Context(), requestOwner(r), r.PathValue("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/history-recent/{entry}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.HistoryTransportDetail(
			r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("entry"), r.URL.Query().Get("cursor"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.EventsPage(r.Context(), requestOwner(r), r.URL.Query().Get("cursor"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/history-page", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.HistoryPage(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("cursor"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/history/{entry}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.HistoryDetail(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.PathValue("entry"),
			r.URL.Query().Get("cursor"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/review/{order}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ReviewOrder(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("order"))
		respond(logger, w, value, err)
	})
}
