package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type miniAppOrderEvidenceInput struct {
	OperationID          uuid.UUID `json:"operation_id"`
	OrderID              uuid.UUID `json:"order_id"`
	ExpectedOrderVersion *int64    `json:"expected_order_version"`
	FileID               uuid.UUID `json:"file_id"`
	AmountMinor          *int64    `json:"amount_minor"`
}

func (s *Server) registerMiniAppOrderEvidence() {
	s.public.HandleFunc("GET /app/orders/{event}/payment-evidence", s.miniAppOrderProofList)
	s.public.HandleFunc("GET /app/orders/{event}/proof-files/{file}/content", s.miniAppOrderProofContent)
	s.public.HandleFunc("GET /app/orders/{event}/proof-files", s.miniAppOrderProofFiles)
	s.public.HandleFunc("POST /app/orders/{event}/payment-evidence", s.miniAppOrderProofSubmit)
	s.public.HandleFunc("GET /app/orders/{event}/payment-evidence/{evidence}", s.miniAppOrderProofRead)
	s.public.HandleFunc("GET /app/orders/{event}/payment-evidence-operations/{operation}", s.miniAppOrderProofRead)
}

func (s *Server) miniAppOrderProofResult(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	result any,
	err error,
) {
	if errors.Is(err, db.ErrMiniAppDenied) {
		miniAppError(w, err)
		return
	}
	if err != nil {
		miniAppPaymentError(w, orderEvidenceError(err))
		return
	}
	s.miniAppOrderResult(ctx, w, r, result, nil)
}

func (s *Server) miniAppOrderProofSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	var input miniAppOrderEvidenceInput
	if !decodeMiniAppOrders(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil || input.OrderID == uuid.Nil || input.FileID == uuid.Nil ||
		input.ExpectedOrderVersion == nil ||
		input.AmountMinor == nil {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"explicit operation, order, version, file and reported amount required",
		)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	args := db.OrderEvidenceSubmission{
		EventID:              event,
		OrderID:              input.OrderID,
		ExpectedOrderVersion: *input.ExpectedOrderVersion,
		FileID:               input.FileID,
		AmountMinor:          *input.AmountMinor,
	}
	result, err := s.store.SubmitMiniAppOrderEvidence(ctx, s.eventScope(), session, input.OperationID, args)
	s.miniAppOrderProofResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderProofFiles(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	after, ok := miniAppPaymentCursor(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppOrderEvidenceFileList(
		ctx,
		s.eventScope(),
		session,
		event,
		after,
		miniAppPaymentPageSize,
	)
	s.miniAppOrderProofResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderProofRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	operation := r.PathValue("operation")
	rawID := r.PathValue("evidence")
	if operation != "" {
		rawID = operation
	}
	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero evidence or operation UUID required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	if operation != "" {
		result, readErr := s.store.MiniAppOrderEvidenceSubmissionReceipt(ctx, s.eventScope(), session, event, id)
		s.miniAppOrderProofResult(ctx, w, r, result, readErr)
		return
	}
	result, err := s.store.MiniAppOwnOrderEvidence(ctx, s.eventScope(), session, event, id)
	s.miniAppOrderProofResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderProofList(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	after, ok := miniAppPaymentCursor(w, r)
	if !ok {
		return
	}
	order := uuid.Nil
	if value := r.URL.Query().Get("order_id"); value != "" {
		var parseErr error
		order, parseErr = uuid.Parse(value)
		if parseErr != nil || order == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "nonzero order UUID required")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppOwnOrderEvidenceList(
		ctx,
		s.eventScope(),
		session,
		event,
		order,
		after,
		miniAppPaymentPageSize,
	)
	s.miniAppOrderProofResult(ctx, w, r, result, err)
}
