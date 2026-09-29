package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func legacyFoodRoutes(
	business, mux *http.ServeMux,
	service legacyfood.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	legacyFoodReadRoutes(business, service, logger)
	business.HandleFunc("POST /v1/food/legacy-menu", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Event string                   `json:"event_id"`
			Key   string                   `json:"key"`
			Meals legacyfood.MealSelection `json:"meals"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		order, err := service.SaveLegacyMenu(r.Context(), requestOwner(r), input.Event, input.Key, input.Meals)
		respond(logger, w, order, err)
	})
	business.HandleFunc("POST /v1/food/quote", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Revision string                   `json:"catalog_revision"`
			Event    string                   `json:"event_id"`
			Meals    legacyfood.MealSelection `json:"meals"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		quote, err := service.QuoteObserved(r.Context(), requestOwner(r), input.Event, input.Meals, input.Revision)
		respond(logger, w, quote, err)
	})
	business.HandleFunc("GET /v1/food/review", func(w http.ResponseWriter, r *http.Request) {
		view, err := service.ReviewView(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("event"),
			r.URL.Query().Get("order"),
		)
		respond(logger, w, view, err)
	})
	business.HandleFunc("GET /v1/food/view", func(w http.ResponseWriter, r *http.Request) {
		view, err := service.View(r.Context(), requestOwner(r), r.URL.Query().Get("event"), r.URL.Query().Get("order"))
		respond(logger, w, view, err)
	})
	business.HandleFunc("POST /v1/food/commands", func(w http.ResponseWriter, r *http.Request) {
		var command legacyfood.Command
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		order, err := service.Execute(r.Context(), requestOwner(r), command)
		respond(logger, w, order, err)
	})
	business.HandleFunc("POST /v1/food/legacy-callbacks/resolve", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Event string `json:"event"`
			Data  string `json:"data"`
		}
		if Decode(w, r, &request) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		binding, err := service.ResolveCallback(r.Context(), requestOwner(r), request.Event, request.Data)
		respond(logger, w, binding, err)
	})
	business.HandleFunc("GET /v1/food/events/{event}/export", func(w http.ResponseWriter, r *http.Request) {
		exported, err := service.Export(r.Context(), requestOwner(r), r.PathValue("event"))
		respond(logger, w, exported, err)
	})
	business.HandleFunc(
		"GET /v1/food/events/{event}/orders/{order}/proof",
		func(w http.ResponseWriter, r *http.Request) {
			generation, err := strconv.ParseInt(r.URL.Query().Get("generation"), 10, 64)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			proof, err := foodRequestedProof(service, r, generation)
			if err != nil {
				respond(logger, w, nil, err)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(proof.Body)
		},
	)
	foodDeliveryRoutes(mux, service, signer, logger)
}

func foodDeliveryRoutes(mux *http.ServeMux, service legacyfood.Service, signer identity.Signer, logger *slog.Logger) {
	delivery := http.NewServeMux()
	delivery.HandleFunc("GET /internal/food/notifications", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.PendingNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	delivery.HandleFunc(
		"POST /internal/food/notifications/{id}/complete",
		func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err == nil {
				err = service.MarkNotification(r.Context(), id)
			}
			respond(logger, w, map[string]bool{"ok": err == nil}, err)
		},
	)
	mux.Handle("/internal/food/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || signer.VerifyDelivery(token) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		delivery.ServeHTTP(w, r)
	}))
}
