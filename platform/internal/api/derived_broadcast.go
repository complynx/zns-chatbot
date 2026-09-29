package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func derivedBroadcastRoutes(
	mux *http.ServeMux,
	service adminmessage.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	handlers := map[string]http.HandlerFunc{
		"preview": func(w http.ResponseWriter, r *http.Request) {
			var command struct {
				Key     string `json:"key"`
				Command string `json:"command"`
			}
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.PreviewDerivedCommand(
				r.Context(),
				requestOwner(r),
				command.Key,
				command.Command,
				source,
			)
			respond(logger, w, value, err)
		},
		"input/start": func(w http.ResponseWriter, r *http.Request) {
			var command struct {
				Key     string `json:"key"`
				Command string `json:"command"`
				ChatID  int64  `json:"chat_id"`
			}
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.BeginDerivedInput(
				r.Context(),
				requestOwner(r),
				command.Key,
				command.Command,
				command.ChatID,
				source,
			)
			respond(logger, w, value, err)
		},
		"input/attach": func(w http.ResponseWriter, r *http.Request) {
			var command adminmessage.Attachment
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.AttachDerivedInput(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		},
		"input/cancel": func(w http.ResponseWriter, r *http.Request) {
			var command struct {
				ID int64 `json:"id"`
			}
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			err = service.CancelDerivedInput(r.Context(), requestOwner(r), command.ID, source)
			respond(logger, w, map[string]bool{"ok": err == nil}, err)
		},
	}
	for path, handler := range handlers {
		mux.Handle(
			"POST /internal/derived/admin-messages/"+path,
			authenticatedDerivation(authorizer, signer, handler, logger),
		)
	}
}
