package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const miniAppOrderReviewPath = "/app/order-reviews/"
const miniAppOrderReviewResultLimit = 256 << 10

func (s *Server) registerMiniAppOrderReviews() {
	s.public.HandleFunc("POST "+miniAppOrderReviewPath+"workspaces", s.miniAppOrderReviewSave)
	s.public.HandleFunc("POST "+miniAppOrderReviewPath+"cancellations", s.miniAppOrderReviewCancel)
	s.public.HandleFunc("POST "+miniAppOrderReviewPath+"confirmations", s.miniAppOrderReviewConfirm)
	s.public.HandleFunc("GET "+miniAppOrderReviewPath+"events", s.miniAppOrderReviewEvents)
	s.public.HandleFunc("GET "+miniAppOrderReviewPath+"events/{event}", s.miniAppOrderReviewRead)
	s.public.HandleFunc("GET "+miniAppOrderReviewPath+"events/{event}/evidence", s.miniAppOrderReviewRead)
	s.public.HandleFunc("GET "+miniAppOrderReviewPath+"events/{event}/evidence/{evidence}", s.miniAppOrderReviewRead)
	s.public.HandleFunc(
		"GET "+miniAppOrderReviewPath+"events/{event}/evidence/{evidence}/content",
		s.miniAppOrderReviewContent,
	)
	s.public.HandleFunc("GET "+miniAppOrderReviewPath+"operations/{operation}", s.miniAppOrderReviewOperation)
}

// Keep the serialized native response bounded before writing any bytes. Identity
// and privileges are server-derived; no IAM token or Telegram locator is exposed.
func (s *Server) miniAppOrderReviewResult(ctx context.Context, w http.ResponseWriter,
	r *http.Request, result any, err error,
) {
	if err != nil {
		if errors.Is(err, db.ErrMiniAppDenied) {
			miniAppError(w, err)
		} else {
			miniAppPaymentError(w, orderEvidenceError(err))
		}
		return
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > miniAppOrderReviewResultLimit {
		miniAppHTTPError(w, http.StatusServiceUnavailable, "order review response exceeds supported bound")
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (s *Server) miniAppOrderReviewEvents(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
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
	result, err := s.store.MiniAppOrderReviewEvents(ctx, scope, session, after)
	s.miniAppOrderReviewResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderReviewRead(w http.ResponseWriter, r *http.Request) {
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
	var result any
	switch {
	case evidence != uuid.Nil:
		result, err = s.store.MiniAppOrderReviewDetail(ctx, scope, session, event, evidence)
	case r.Pattern == "GET "+miniAppOrderReviewPath+"events/{event}":
		result, err = s.store.MiniAppOrderReviewEvent(ctx, scope, session, event)
	default:
		after, cursorOK := miniAppPaymentCursor(w, r)
		if !cursorOK {
			return
		}
		result, err = s.store.MiniAppOrderReviewQueue(ctx, scope, session, event, after, miniAppPaymentPageSize)
	}
	s.miniAppOrderReviewResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderReviewOperation(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	operation, err := uuid.Parse(r.PathValue("operation"))
	if err != nil || operation == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero operation UUID required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.MiniAppOrderReviewOperationByID(ctx, scope, session, operation)
	s.miniAppOrderReviewResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderReviewContent(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, evidence, ok := miniAppPaymentIDs(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), fileTimeoutMillis*time.Millisecond)
	defer cancel()
	scope, session, err := s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	record, err := s.store.MiniAppOrderReviewFile(ctx, scope, session, event, evidence)
	if err != nil {
		s.miniAppOrderReviewResult(ctx, w, r, nil, err)
		return
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		s.miniAppOrderReviewResult(ctx, w, r, nil, err)
		return
	}
	scope, session, err = s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	current, err := s.store.MiniAppOrderReviewFile(ctx, scope, session, event, evidence)
	if err == nil && (current.ID != record.ID || current.TelegramID != record.TelegramID) {
		err = db.ErrConflict
	}
	if err != nil {
		s.miniAppOrderReviewResult(ctx, w, r, nil, err)
		return
	}
	w.Header().Set("Content-Type", fileContentType(raw))
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- bounded authorized attachment; nosniff and sandbox CSP apply.
}
