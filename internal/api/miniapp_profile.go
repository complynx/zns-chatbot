package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type miniAppProfileChange struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ExpectedVersion *int64    `json:"expected_version"`
	FullName        string    `json:"full_name,omitempty"`
	BotLanguage     string    `json:"bot_language,omitempty"`
}

func (s *Server) miniAppProfile(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	p, err := s.store.GetProfile(
		ctx,
		db.ProfileScope{
			BotID:        attempt.User.BotID,
			Organization: attempt.User.Organization,
			Subject:      attempt.User.Subject,
			UserID:       attempt.User.ID,
		},
	)
	if err == nil {
		_, err = s.currentMiniApp(ctx, r)
	}
	if err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func (s *Server) miniAppProfileChange(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppProfileChange
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid profile change")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) || input.ExpectedVersion == nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid profile change")
		return
	}
	mutation := db.ProfileMutation{
		OperationID:     input.OperationID,
		ExpectedVersion: *input.ExpectedVersion,
		FullName:        input.FullName,
		BotLanguage:     input.BotLanguage,
	}
	switch r.URL.Path {
	case "/app/profile/name":
		mutation.Kind = "set_name"
	case "/app/profile/language":
		mutation.Kind = "set_language"
	default:
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	if _, err := s.authenticatedMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	p, err := s.store.MutateMiniAppProfile(ctx, s.options.BotID, bearer, s.miniAppKey.Generation(), mutation)
	switch {
	case errors.Is(err, db.ErrInvalidProfile):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid profile change")
	case errors.Is(err, db.ErrConflict):
		miniAppHTTPError(w, http.StatusConflict, "profile changed; refresh before editing")
	case err != nil:
		miniAppError(w, err)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p)
	}
}
