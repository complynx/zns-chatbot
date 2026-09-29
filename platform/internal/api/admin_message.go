package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func adminMessageRoutes(
	business, mux *http.ServeMux,
	service adminmessage.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	adminBroadcastRoutes(business, service, logger)
	business.HandleFunc("POST /v1/admin-messages/preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Key     string `json:"key"`
			Command string `json:"command"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		message, err := service.PreviewCommand(r.Context(), requestOwner(r), input.Key, input.Command)
		respond(logger, w, message, err)
	})
	business.HandleFunc("POST /v1/admin-messages/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		switch r.PathValue("action") {
		case "send":
			err = service.Enqueue(r.Context(), requestOwner(r), id)
		case "cancel":
			err = service.Cancel(r.Context(), requestOwner(r), id)
		case "resume":
			value, resumeErr := service.ResumePreview(r.Context(), requestOwner(r), id)
			respond(logger, w, value, resumeErr)
			return
		case "results":
			values, resultErr := service.Results(r.Context(), requestOwner(r), id)
			respond(logger, w, values, resultErr)
			return
		default:
			http.NotFound(w, r)
			return
		}
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	delivery := http.NewServeMux()
	adminBroadcastDeliveryRoutes(delivery, service, logger)
	delivery.HandleFunc("POST /internal/admin-messages/claim", func(w http.ResponseWriter, r *http.Request) {
		value, found, err := service.Claim(r.Context())
		respond(logger, w, struct {
			Delivery adminmessage.Delivery `json:"delivery"`
			Found    bool                  `json:"found"`
		}{value, found}, err)
	})
	delivery.HandleFunc("POST /internal/admin-messages/complete", func(w http.ResponseWriter, r *http.Request) {
		var input adminmessage.Completion
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteDelivery(r.Context(), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.Handle("/internal/admin-messages/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || signer.VerifyDelivery(token) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		delivery.ServeHTTP(w, r)
	}))
}
