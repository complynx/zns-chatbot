package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

type miniAppDetailsChange struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ExpectedVersion *int64    `json:"expected_version"`
	Value           string    `json:"value"`
}

func (s *Server) miniAppDetails(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	operation := uuid.Nil
	if raw := r.PathValue("operation"); raw != "" {
		var err error
		operation, err = uuid.Parse(raw)
		if err != nil || operation == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details operation")
			return
		}
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
	status, err := s.store.MiniAppRegistrationDetails(
		ctx,
		s.options.BotID,
		bearer,
		s.miniAppKey.Generation(),
		operation,
	)
	if errors.Is(err, pgx.ErrNoRows) && operation != uuid.Nil {
		// Session lookup and missing receipt both fail closed without disclosing data.
		if _, currentErr := s.currentMiniApp(ctx, r); currentErr != nil {
			miniAppError(w, currentErr)
			return
		}
		miniAppHTTPError(w, http.StatusNotFound, "registration details operation not found")
		return
	}
	if err != nil {
		miniAppError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) miniAppDetailsChange(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppDetailsChange
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details change")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) || input.ExpectedVersion == nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details change")
		return
	}
	field := registrationdetails.LegalName
	if r.URL.Path == "/app/registration-details/passport-number" {
		field = registrationdetails.PassportNumber
	}
	mutation := registrationdetails.Mutation{
		OperationID:     input.OperationID,
		ExpectedVersion: *input.ExpectedVersion,
		Field:           field,
		Value:           input.Value,
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
	status, err := s.store.MutateMiniAppRegistrationDetails(
		ctx,
		s.options.BotID,
		bearer,
		s.miniAppKey.Generation(),
		mutation,
		s.options.RegistrationDetails,
	)
	switch {
	case errors.Is(err, registrationdetails.ErrInvalid):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details change")
	case errors.Is(err, db.ErrConflict):
		miniAppHTTPError(w, http.StatusConflict, "registration details changed; check the current status")
	case err != nil:
		miniAppError(w, err)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}
}
