package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

type miniAppMassageWriteInput[A any] struct {
	OperationID uuid.UUID         `json:"operation_id"`
	Arguments   A                 `json:"arguments"`
	ProposalID  massageProposalID `json:"proposal_id,omitzero"`
	Confirmed   bool              `json:"confirmed,omitempty"`
}

func (s *Server) registerMiniAppMassage() {
	s.registerMiniAppMassageStaff()
	s.registerMiniAppMassageBindings()
	s.public.HandleFunc("GET /app/massage/events", s.miniAppMassageRead)
	s.public.HandleFunc("GET /app/massage/{event}/schedule", s.miniAppMassageRead)
	s.public.HandleFunc("GET /app/massage/{event}/pending", s.miniAppMassageRead)
	s.public.HandleFunc("GET /app/massage/{event}/bookings", s.miniAppMassageRead)
	s.public.HandleFunc("GET /app/massage/{event}/bookings/{booking}", s.miniAppMassageRead)
	s.public.HandleFunc("GET /app/massage/{event}/operations/{operation}", s.miniAppMassageRead)
	s.public.HandleFunc("POST /app/massage/{event}/availability", s.miniAppMassageAvailability)
	s.public.HandleFunc("POST /app/massage/{event}/book/prepare", s.miniAppMassageBook)
	s.public.HandleFunc("POST /app/massage/{event}/book", s.miniAppMassageBook)
	s.public.HandleFunc("POST /app/massage/{event}/cancel/prepare", s.miniAppMassageCancel)
	s.public.HandleFunc("POST /app/massage/{event}/cancel", s.miniAppMassageCancel)
}

func miniAppMassageEvent(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid massage event")
		return uuid.Nil, false
	}
	return event, true
}

func decodeMiniAppMassage(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid massage request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid massage request")
		return false
	}
	return true
}

func (s *Server) miniAppMassageSession(ctx context.Context, r *http.Request) (db.MiniAppMassageSession, error) {
	// The existing extractor authenticates IAM and returns the current bearer/key generation.
	session, err := s.miniAppOrderSession(ctx, r)
	return db.MiniAppMassageSession(session), err
}

func (s *Server) miniAppMassageResult(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	result any,
	err error,
) {
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, sessionErr := s.currentMiniApp(ctx, r); sessionErr != nil {
				miniAppError(w, sessionErr)
				return
			}
			miniAppHTTPError(w, http.StatusNotFound, "massage event, record or current access not found")
			return
		}
		switch {
		case errors.Is(err, db.ErrConflict),
			errors.Is(err, massage.ErrUnavailable),
			errors.Is(err, massage.ErrBookingTopology):
			miniAppHTTPError(
				w,
				http.StatusConflict,
				"massage terms, approval or availability changed; read current state",
			)
		case errors.Is(err, massage.ErrInvalid):
			miniAppHTTPError(w, http.StatusBadRequest, "invalid massage selection or page")
		case errors.Is(err, db.ErrMiniAppDenied):
			miniAppError(w, err)
		default:
			w.Header().Set("Retry-After", "1")
			miniAppHTTPError(
				w,
				http.StatusServiceUnavailable,
				"massage temporarily unavailable; retry the original operation",
			)
		}
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		miniAppHTTPError(
			w,
			http.StatusServiceUnavailable,
			"massage response unavailable; recover the original operation",
		)
		return
	}
	// Delivery is rechecked after the transaction. A lost reply never authorizes a new effect.
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func handleMiniAppMassageWrite[A any](s *Server, w http.ResponseWriter, r *http.Request,
	requestEvent func(A) uuid.UUID,
	execute func(context.Context, db.MiniAppMassageSession, miniAppMassageWriteInput[A], bool) (any, error),
) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	var input miniAppMassageWriteInput[A]
	if !decodeMiniAppMassage(w, r, &input) {
		return
	}
	prepare := strings.HasSuffix(r.URL.Path, "/prepare")
	if input.OperationID == uuid.Nil || requestEvent(input.Arguments) != event || (prepare && input.Confirmed) ||
		(!prepare && (input.ProposalID.UUID == uuid.Nil || !input.Confirmed)) {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"matching event, operation, and exact proposal confirmation required",
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
	result, err := execute(ctx, session, input, prepare)
	s.miniAppMassageResult(ctx, w, r, result, err)
}

//nolint:dupl // Keep booking and cancellation adapters typed to their distinct business arguments.
func (s *Server) miniAppMassageBook(w http.ResponseWriter, r *http.Request) {
	handleMiniAppMassageWrite(
		s,
		w,
		r,
		func(args db.MassageBookingArguments) uuid.UUID { return args.EventID },
		func(ctx context.Context,
			session db.MiniAppMassageSession,
			input miniAppMassageWriteInput[db.MassageBookingArguments],
			prepare bool) (any, error) {
			if prepare {
				if input.ProposalID.UUID != uuid.Nil {
					return s.store.AdoptMiniAppMassageBooking(
						ctx,
						s.eventScope(),
						session,
						input.OperationID,
						input.ProposalID.UUID,
						input.Arguments,
					)
				}
				return s.store.PrepareMiniAppMassageBooking(
					ctx,
					s.eventScope(),
					session,
					input.OperationID,
					input.Arguments,
				)
			}
			return s.store.BookMiniAppMassage(
				ctx,
				s.eventScope(),
				session,
				input.OperationID,
				input.ProposalID.UUID,
				input.Arguments,
			)
		},
	)
}

//nolint:dupl // Keep booking and cancellation adapters typed to their distinct business arguments.
func (s *Server) miniAppMassageCancel(w http.ResponseWriter, r *http.Request) {
	handleMiniAppMassageWrite(
		s,
		w,
		r,
		func(args db.MassageCancellationArguments) uuid.UUID { return args.EventID },
		func(ctx context.Context,
			session db.MiniAppMassageSession,
			input miniAppMassageWriteInput[db.MassageCancellationArguments],
			prepare bool) (any, error) {
			if prepare {
				if input.ProposalID.UUID != uuid.Nil {
					return s.store.AdoptMiniAppMassageCancellation(
						ctx,
						s.eventScope(),
						session,
						input.OperationID,
						input.ProposalID.UUID,
						input.Arguments,
					)
				}
				return s.store.PrepareMiniAppMassageCancellation(
					ctx,
					s.eventScope(),
					session,
					input.OperationID,
					input.Arguments,
				)
			}
			return s.store.CancelMiniAppMassage(
				ctx,
				s.eventScope(),
				session,
				input.OperationID,
				input.ProposalID.UUID,
				input.Arguments,
			)
		},
	)
}

func (s *Server) miniAppMassageAvailability(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppMassageEvent(w, r)
	if !ok {
		return
	}
	var args db.MassageAvailabilityArguments
	if !decodeMiniAppMassage(w, r, &args) {
		return
	}
	if args.EventID != event {
		miniAppHTTPError(w, http.StatusBadRequest, "availability event must match the selected event")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppMassageSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.OwnMiniAppMassageAvailability(ctx, s.eventScope(), session, args)
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event := uuid.Nil
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
	result, err := s.miniAppMassageReadData(ctx, r, session, event)
	s.miniAppMassageResult(ctx, w, r, result, err)
}

func (s *Server) miniAppMassageReadData(
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
		return s.store.OwnMiniAppMassageBooking(ctx, s.eventScope(), session, event, id)
	}
	if raw := r.PathValue("operation"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, massage.ErrInvalid
		}
		return s.store.OwnMiniAppMassageOperation(ctx, s.eventScope(), session, event, id)
	}
	if strings.HasSuffix(r.URL.Path, "/schedule") {
		return s.store.OwnMiniAppMassageSchedule(ctx, s.eventScope(), session, event)
	}
	after, limit, ok := miniAppOrderPage(r)
	if !ok {
		return nil, massage.ErrInvalid
	}
	if strings.HasSuffix(r.URL.Path, "/pending") {
		page, err := s.store.OwnMiniAppPendingMassageProposals(ctx, s.eventScope(), session, event, after, int(limit))
		if err != nil {
			return nil, err
		}
		return boundedMassagePendingPage(page)
	}
	if event == uuid.Nil {
		return s.store.OwnMiniAppMassageEvents(ctx, s.eventScope(), session, after, int(limit))
	}
	page, err := s.store.OwnMiniAppMassageBookings(ctx, s.eventScope(), session, event, after, int(limit))
	if err != nil {
		return nil, err
	}
	return boundedMassagePage(page)
}
