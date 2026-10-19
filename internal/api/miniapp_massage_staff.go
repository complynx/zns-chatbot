package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func (s *Server) registerMiniAppMassageStaff() {
	s.public.HandleFunc("POST /app/massage-staff/{event}/instant/prepare", s.miniAppMassageInstant)
	s.public.HandleFunc("POST /app/massage-staff/{event}/instant", s.miniAppMassageInstant)
	s.registerMiniAppMassageSpecialistPreferences()
	s.public.HandleFunc("POST /app/massage-staff/{event}/windows", s.miniAppMassageStaffWindows)
	s.public.HandleFunc("GET /app/massage-staff/events", s.miniAppMassageStaffRead)
	s.public.HandleFunc("GET /app/massage-staff/{event}/schedule", s.miniAppMassageStaffRead)
	s.public.HandleFunc("GET /app/massage-staff/{event}/clients/{booking}", s.miniAppMassageStaffRead)
	s.public.HandleFunc("POST /app/massage-staff/{event}/timetable", s.miniAppMassageStaffTimetable)
}

func (s *Server) miniAppMassageStaffRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var event uuid.UUID
	if r.PathValue("event") != "" {
		var ok bool
		event, ok = miniAppMassageEvent(w, r)
		if !ok {
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
	result, err := s.miniAppMassageStaffReadData(ctx, r, session, event)
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageStaffReadData(
	ctx context.Context,
	r *http.Request,
	session db.MiniAppMassageSession,
	event uuid.UUID,
) (any, error) {
	if raw := r.PathValue("booking"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, massage.ErrInvalid
		}
		return s.store.MiniAppMassageStaffClient(ctx, s.eventScope(), session, event, id)
	}
	if strings.HasSuffix(r.URL.Path, "/schedule") {
		return s.store.MiniAppMassageStaffSchedule(ctx, s.eventScope(), session, event)
	}
	after, limit, ok := miniAppOrderPage(r)
	if !ok {
		return nil, massage.ErrInvalid
	}
	return s.store.MiniAppMassageStaffEvents(ctx, s.eventScope(), session, after, int(limit))
}

func (s *Server) miniAppMassageStaffTimetable(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	var args db.MassageStaffQuery
	if !decodeMiniAppMassage(w, r, &args) {
		return
	}
	if args.EventID != event {
		miniAppHTTPError(w, http.StatusBadRequest, "timetable event must match the selected event")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppMassageSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppMassageStaffBookings(ctx, s.eventScope(), session, args)
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageStaffWindows(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	var args db.MassageStaffWindowsQuery
	if !decodeMiniAppMassage(w, r, &args) {
		return
	}
	if args.EventID != event {
		miniAppHTTPError(w, http.StatusBadRequest, "window discovery event must match the selected event")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppMassageSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppMassageStaffWindows(ctx, s.eventScope(), session, args)
	s.miniAppMassageResult(ctx, w, r, result, err)
}
