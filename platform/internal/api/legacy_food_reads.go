package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func legacyFoodReadRoutes(business *http.ServeMux, service legacyfood.Service, logger *slog.Logger) {
	business.HandleFunc("GET /v1/food/capabilities", func(w http.ResponseWriter, r *http.Request) {
		var result legacyfood.OwnerCapabilities
		var err error
		if event := r.URL.Query().Get("event"); event != "" {
			result, err = service.EventCapabilities(r.Context(), requestOwner(r), event)
		} else {
			result, err = service.OwnerCapabilities(r.Context(), requestOwner(r))
		}
		respond(logger, w, result, err)
	})
	business.HandleFunc("GET /v1/food/review-queue", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.ReviewQueue(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("event"),
			r.URL.Query().Get("cursor"),
		)
		respond(logger, w, result, err)
	})
}

func foodRequestedProof(service legacyfood.Service, r *http.Request, generation int64) (orders.Proof, error) {
	event, id, kind := r.PathValue("event"), r.PathValue("order"), r.URL.Query().Get("kind")
	if raw, ok := r.URL.Query()["version"]; ok {
		version, err := strconv.ParseInt(raw[0], 10, 64)
		if err != nil || version < 1 {
			return orders.Proof{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidJSON}
		}
		return service.ReviewProof(r.Context(), requestOwner(r), event, id, kind, generation, version)
	}
	return service.Proof(r.Context(), requestOwner(r), event, id, kind, generation)
}
