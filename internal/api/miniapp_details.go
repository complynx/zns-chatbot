package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

type miniAppDetailsChange struct {
	OperationID     uuid.UUID  `json:"operation_id"`
	ExpectedVersion *int64     `json:"expected_version"`
	DetailsRequest  *uuid.UUID `json:"details_request,omitempty"`
	Value           string     `json:"value"`
}

const photoJSONEnvelopeLimit = 1024

func (s *Server) miniAppDetailsRequest(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, err := uuid.Parse(r.PathValue("request"))
	if err != nil || id == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid details request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	_, err = s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	req, category, err := s.store.MiniAppDetailsForm(
		ctx,
		s.options.BotID,
		bearer,
		s.miniAppKey.Generation(),
		id,
		s.options.RegistrationDetails,
	)
	if err != nil {
		miniAppError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var countries []string
	if len(req.Request.Requirements.PassportCountries) > 0 {
		for _, region := range language.Supported.Regions() {
			if code := region.String(); region.IsCountry() && len(code) == registrationdetails.Alpha2Length {
				countries = append(countries, code)
			}
		}
	}
	_ = json.NewEncoder(w).Encode(struct {
		db.DetailsRequestStatus

		Countries       []string `json:"countries,omitempty"`
		CountryCategory string   `json:"country_category,omitempty"`
	}{req, countries, category})
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
	authCtx, cancelAuth := context.WithTimeout(r.Context(), miniAppTimeout)
	_, err := s.authenticatedMiniApp(authCtx, r)
	cancelAuth()
	if err != nil {
		miniAppError(w, err)
		return
	}
	limit := int64(miniAppBodyLimit)
	field := registrationdetails.LegalName
	switch r.URL.Path {
	case "/app/registration-details/passport-number":
		field = registrationdetails.PassportNumber
	case "/app/registration-details/passport-country":
		field = registrationdetails.PassportCountry
	case "/app/registration-details/passport-photo":
		field = registrationdetails.PassportPhoto
		limit = int64(base64.StdEncoding.EncodedLen(registrationdetails.MaxPhotoBytes)) + photoJSONEnvelopeLimit
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details change")
		return
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) || input.ExpectedVersion == nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details change")
		return
	}
	mutation := registrationdetails.Mutation{
		OperationID:     input.OperationID,
		ExpectedVersion: *input.ExpectedVersion,
		Field:           field,
		Value:           input.Value,
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	// Uploads can take longer than a database operation. The write rechecks the session.
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	status, err := s.store.MutateMiniAppRegistrationDetails(
		ctx,
		s.options.BotID,
		bearer,
		s.miniAppKey.Generation(),
		mutation,
		input.DetailsRequest,
		s.options.RegistrationDetails,
	)
	switch {
	case errors.Is(err, registrationdetails.ErrInvalid):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid registration details change")
	case errors.Is(err, db.ErrConflict):
		miniAppHTTPError(w, http.StatusConflict, "registration details changed; check the current status")
	case errors.Is(err, db.ErrDetailsRequestUnavailable):
		miniAppHTTPError(w, http.StatusGone, "registration details request unavailable")
	case err != nil:
		miniAppError(w, err)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}
}
