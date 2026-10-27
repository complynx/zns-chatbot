package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type miniAppMassagePreferenceWrite struct {
	OperationID uuid.UUID `json:"operation_id"`
	Arguments   struct {
		EventID         uuid.UUID `json:"event_id"`
		ExpectedVersion *int64    `json:"expected_version"`
		NotifyBookings  *bool     `json:"notify_bookings"`
		NotifyNext      *bool     `json:"notify_next"`
	} `json:"arguments"`
}

func (s *Server) registerMiniAppMassageSpecialistPreferences() {
	s.public.HandleFunc("GET /app/massage-staff/{event}/preferences", s.miniAppMassageSpecialistPreferences)
	s.public.HandleFunc("POST /app/massage-staff/{event}/preferences", s.miniAppSetMassageSpecialistPreferences)
	s.public.HandleFunc(
		"GET /app/massage-staff/{event}/preference-operations/{operation}",
		s.miniAppMassageSpecialistPreferences,
	)
}

func (s *Server) miniAppMassageSpecialistPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	operation := uuid.Nil
	if raw := r.PathValue("operation"); raw != "" {
		var err error
		operation, err = uuid.Parse(raw)
		if err != nil || operation == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid preference operation")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppMassageSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	var result db.MassageSpecialistPreferences
	if operation == uuid.Nil {
		result, err = s.store.MiniAppMassageSpecialistPreferences(ctx, s.eventScope(), session, event)
	} else {
		result, err = s.store.MiniAppMassageSpecialistPreferenceOperation(
			ctx,
			s.eventScope(),
			session,
			event,
			operation,
		)
	}
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppSetMassageSpecialistPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	var input miniAppMassagePreferenceWrite
	if !decodeMiniAppMassage(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil || input.Arguments.EventID != event || input.Arguments.ExpectedVersion == nil {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"matching event, explicit expected version and nonzero preference operation required",
		)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppMassageSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	args := db.MassageSpecialistPreferenceChange{
		EventID:         input.Arguments.EventID,
		ExpectedVersion: *input.Arguments.ExpectedVersion,
		NotifyBookings:  input.Arguments.NotifyBookings,
		NotifyNext:      input.Arguments.NotifyNext,
	}
	result, err := s.store.SetMiniAppMassageSpecialistPreferences(
		ctx,
		s.eventScope(),
		session,
		input.OperationID,
		args,
	)
	s.miniAppMassageResult(ctx, w, r, result, err)
}
