package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func orderRoutes(mux *http.ServeMux, service orders.Service, logger *slog.Logger) {
	orderRefundRoutes(mux, service, logger)
	orderAgentReadRoutes(mux, service, logger)
	proofRoutes(mux, service, logger)
	mux.HandleFunc("GET /v1/orders/{order}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.GetByID(r.Context(), requestOwner(r), r.PathValue("order"))
		w.Header().Set("Cache-Control", "no-store")
		respond(logger, w, value, err)
	})
	exportRoute(mux, service, logger)
	mux.HandleFunc(
		"GET /v1/order-events/{event}/orders/{order}/payment-instructions",
		func(w http.ResponseWriter, r *http.Request) {
			value, err := service.Instructions(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("order"))
			w.Header().Set("Cache-Control", "no-store")
			respond(logger, w, value, err)
		},
	)
	mux.HandleFunc("GET /v1/order-events/{event}/orders/{order}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Get(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("order"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/history", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.History(r.Context(), requestOwner(r), r.PathValue("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/admins", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.PaymentAdmins(r.Context(), r.PathValue("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/payment-inbox", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ListPage(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("cursor"),
			true,
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Event(r.Context(), r.PathValue("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/orders", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ListPage(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("cursor"),
			false,
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/order-events/{event}/quote", func(w http.ResponseWriter, r *http.Request) {
		var input orders.ChoiceInput
		if DecodeOrderRequest(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Quote(r.Context(), requestOwner(r), r.PathValue("event"), input)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/order-actions", func(w http.ResponseWriter, r *http.Request) {
		var input orders.Command
		if DecodeOrderRequest(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Execute(r.Context(), requestOwner(r), input)
		respond(logger, w, value, err)
	})
}
