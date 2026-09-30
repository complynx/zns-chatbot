package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func orderRefundRoutes(mux *http.ServeMux, service orders.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/order-events/{event}/refunds", func(w http.ResponseWriter, r *http.Request) {
		var before int64
		var err error
		if raw := r.URL.Query().Get("before"); raw != "" {
			before, err = strconv.ParseInt(raw, 10, 64)
		}
		if err != nil || before < 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: "refund_invalid"})
			return
		}
		value, err := service.RefundTasks(r.Context(), requestOwner(r), r.PathValue("event"), before)
		w.Header().Set("Cache-Control", "no-store")
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/order-refunds/{refund}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("refund"), 10, 64)
		if err != nil || id <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: "refund_invalid"})
			return
		}
		value, err := service.Refund(r.Context(), requestOwner(r), id)
		w.Header().Set("Cache-Control", "no-store")
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/order-refunds/confirm", func(w http.ResponseWriter, r *http.Request) {
		var input orders.RefundConfirmation
		if DecodeOrderRequest(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.ConfirmRefund(r.Context(), requestOwner(r), input)
		respond(logger, w, value, err)
	})
}
