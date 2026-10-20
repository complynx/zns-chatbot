package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const (
	miniAppPaymentPath     = "/app/payment-reviews/"
	miniAppPaymentPageSize = 20
)

func (s *Server) registerMiniAppPayments() {
	s.public.HandleFunc("GET "+miniAppPaymentPath+"events", s.miniAppPaymentEvents)
	s.public.HandleFunc("GET "+miniAppPaymentPath+"events/{event}", s.miniAppPaymentEvent)
	s.public.HandleFunc("GET "+miniAppPaymentPath+"events/{event}/evidence", s.miniAppPaymentEvidenceList)
	s.public.HandleFunc("GET "+miniAppPaymentPath+"events/{event}/evidence/{evidence}", s.miniAppPaymentDetail)
	s.public.HandleFunc("GET "+miniAppPaymentPath+"events/{event}/evidence/{evidence}/content", s.miniAppPaymentContent)
	s.public.HandleFunc("GET "+miniAppPaymentPath+"operations/{operation}", s.miniAppPaymentOperation)
	s.public.HandleFunc("POST "+miniAppPaymentPath+"workspaces", s.miniAppPaymentSave)
	s.public.HandleFunc("POST "+miniAppPaymentPath+"cancellations", s.miniAppPaymentCancel)
	s.public.HandleFunc("POST "+miniAppPaymentPath+"confirmations", s.miniAppPaymentConfirm)
}

// miniAppPaymentScope keeps IAM tokens server-side and derives privileges from
// the verified principal. The browser supplies neither identity nor role flags.
func (s *Server) miniAppPaymentScope(
	ctx context.Context,
	r *http.Request,
) (db.PaymentScope, db.MiniAppPaymentSession, error) {
	var session db.MiniAppPaymentSession
	attempt, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return db.PaymentScope{}, session, err
	}
	principal, err := s.miniAppVerifiedPrincipal(ctx, attempt.User)
	if err != nil {
		return db.PaymentScope{}, session, err
	}
	current, err := s.currentMiniApp(ctx, r)
	if err != nil {
		return db.PaymentScope{}, session, err
	}
	if current.User.ID != attempt.User.ID || current.User.Subject != principal.Subject ||
		current.User.Organization != principal.OrganizationID {
		return db.PaymentScope{}, session, db.ErrMiniAppDenied
	}
	session.Bearer, err = miniAppBearer(r)
	if err != nil {
		return db.PaymentScope{}, session, err
	}
	session.Generation = s.miniAppKey.Generation()
	_, admin := principal.Roles["zinusya.admin"][s.options.OrganizationID]
	return db.PaymentScope{
		EventScope:  s.eventScope(),
		UserID:      current.User.ID,
		Subject:     principal.Subject,
		GlobalAdmin: admin,
	}, session, nil
}

func miniAppPaymentJSON(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func miniAppPaymentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrMiniAppDenied), errors.Is(err, identity.ErrDenied):
		miniAppError(w, err)
	case errors.Is(err, payments.ErrDenied), errors.Is(err, payments.ErrUnavailable):
		miniAppHTTPError(w, http.StatusForbidden, "current event payment authority required")
	case errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusNotFound, "payment record not found")
	case errors.Is(err, db.ErrConflict):
		miniAppHTTPError(w, http.StatusConflict, "payment decision, balance or operation changed; read current state")
	case errors.Is(err, payments.ErrInvalid):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment decision")
	default:
		if status, ok := errors.AsType[huma.StatusError](err); ok {
			miniAppHTTPError(w, status.GetStatus(), "payment proof unavailable")
			return
		}
		w.Header().Set("Retry-After", "1")
		miniAppHTTPError(
			w,
			http.StatusServiceUnavailable,
			"payment request temporarily unavailable; retry the same operation",
		)
	}
}

func decodeMiniAppPayment(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment request")
		return false
	}
	return true
}

func miniAppPaymentCursor(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	value := r.URL.Query().Get("after")
	if value == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment cursor")
		return uuid.Nil, false
	}
	return id, true
}

func miniAppPaymentIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment event")
		return uuid.Nil, uuid.Nil, false
	}
	if r.PathValue("evidence") == "" {
		return event, uuid.Nil, true
	}
	evidence, err := uuid.Parse(r.PathValue("evidence"))
	if err != nil || evidence == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment evidence")
		return uuid.Nil, uuid.Nil, false
	}
	return event, evidence, true
}

func (s *Server) miniAppPaymentEvents(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	after, ok := miniAppPaymentCursor(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, _, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.PaymentReviewEvents(ctx, scope, after)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}

func (s *Server) miniAppPaymentEvidenceList(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, _, ok := miniAppPaymentIDs(w, r)
	if !ok {
		return
	}
	after, ok := miniAppPaymentCursor(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	rows, err := s.store.PaymentEvidenceList(ctx, scope, event, after, miniAppPaymentPageSize, true)
	if err == nil {
		err = s.store.CheckMiniAppPaymentReview(ctx, scope, session, event)
	}
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	result := paymentListResult{Evidence: rows}
	if len(rows) == miniAppPaymentPageSize {
		result.Next = &rows[len(rows)-1].ID
	}
	miniAppPaymentJSON(w, result)
}

type miniAppPaymentDetail struct {
	Payer     db.PaymentReviewPayer      `json:"payer"`
	Evidence  payments.Evidence          `json:"evidence"`
	Balance   payments.Balance           `json:"balance"`
	Workspace *db.PaymentReviewWorkspace `json:"workspace"`
}

func (s *Server) miniAppPaymentDetail(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, evidence, ok := miniAppPaymentIDs(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.readMiniAppPaymentDetail(ctx, scope, session, event, evidence)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}

func (s *Server) readMiniAppPaymentDetail(
	ctx context.Context,
	scope db.PaymentScope,
	session db.MiniAppPaymentSession,
	event, evidence uuid.UUID,
) (miniAppPaymentDetail, error) {
	var result miniAppPaymentDetail
	if err := s.store.CheckMiniAppPaymentReview(ctx, scope, session, event); err != nil {
		return result, err
	}
	var err error
	result.Evidence, err = s.store.PaymentEvidence(ctx, scope, event, evidence)
	if err != nil {
		return result, err
	}
	result.Payer, err = s.store.MiniAppPaymentReviewPayer(ctx, scope, session, event, evidence)
	if err != nil {
		return result, err
	}
	result.Balance, err = s.store.PaymentBalance(ctx, scope, event, result.Evidence.PassID)
	if err != nil {
		return result, err
	}
	workspace, err := s.store.CurrentPaymentReviewWorkspace(ctx, scope, event, evidence)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err == nil {
		result.Workspace = &workspace
	}
	return result, s.store.CheckMiniAppPaymentReview(ctx, scope, session, event)
}

func (s *Server) miniAppPaymentOperation(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	operation := r.PathValue("operation")
	if operation == "" || len(operation) > 128 {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid payment operation")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppPaymentReviewWorkspaceOperation(ctx, scope, session, operation)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}

func (s *Server) miniAppPaymentEvent(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, _, ok := miniAppPaymentIDs(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppPaymentReviewEvent(ctx, scope, session, event)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	miniAppPaymentJSON(w, result)
}
