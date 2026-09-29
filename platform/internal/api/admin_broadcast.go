package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func adminBroadcastRoutes(mux *http.ServeMux, service adminmessage.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /v1/admin-messages/audience", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Cursor string `json:"cursor"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Audience(r.Context(), requestOwner(r), input.Cursor)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/profile", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			UserID string `json:"user_id"`
			Cursor string `json:"cursor"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.AudienceProfile(r.Context(), requestOwner(r), input.UserID, input.Cursor)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/capabilities", func(w http.ResponseWriter, r *http.Request) {
		allowed, err := service.CanBroadcast(r.Context(), requestOwner(r))
		respond(logger, w, map[string]bool{"allowed": allowed}, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/input/start", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Key     string `json:"key"`
			Command string `json:"command"`
			ChatID  int64  `json:"chat_id"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.BeginInput(r.Context(), requestOwner(r), input.Key, input.Command, input.ChatID)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/input/prompt", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID       int64 `json:"id"`
			ChatID   int64 `json:"chat_id"`
			PromptID int64 `json:"prompt_id"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.RegisterPrompt(r.Context(), requestOwner(r), input.ID, input.ChatID, input.PromptID)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/input/pending", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ChatID int64 `json:"chat_id"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.PendingInputs(r.Context(), requestOwner(r), input.ChatID)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/input/attach", func(w http.ResponseWriter, r *http.Request) {
		var input adminmessage.Attachment
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.AttachInput(r.Context(), requestOwner(r), input)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/input/cancel", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID int64 `json:"id"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CancelInput(r.Context(), requestOwner(r), input.ID)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.HandleFunc("POST /v1/admin-messages/review", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID     int64 `json:"id"`
			Offset int64 `json:"offset"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Review(r.Context(), requestOwner(r), input.ID, input.Offset)
		respond(logger, w, value, err)
	})
}

func adminBroadcastDeliveryRoutes(mux *http.ServeMux, service adminmessage.Service, logger *slog.Logger) {
	adminInputExpiryCurrentRoute(mux, service, logger)
	mux.HandleFunc("POST /internal/admin-messages/source", func(w http.ResponseWriter, r *http.Request) {
		var source adminmessage.Source
		if Decode(w, r, &source) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.RegisterSource(r.Context(), source)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.HandleFunc("POST /internal/admin-messages/input-expiry/claim", func(w http.ResponseWriter, r *http.Request) {
		value, found, err := service.ClaimInputExpiry(r.Context())
		respond(logger, w, struct {
			Expiry adminmessage.InputExpiry `json:"expiry"`
			Found  bool                     `json:"found"`
		}{value, found}, err)
	})
	mux.HandleFunc("POST /internal/admin-messages/input-expiry/complete", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID      int64 `json:"id"`
			Attempt int64 `json:"attempt"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteInputExpiry(r.Context(), input.ID, input.Attempt)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
