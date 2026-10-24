package api

import (
	"context"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
)

// miniAppPaymentContent checks event authority/session before and after bounded
// Telegram IO. General file endpoints remain owner-only. No DB locks cross IO.
func (s *Server) miniAppPaymentContent(w http.ResponseWriter, r *http.Request) {
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
	if err = s.store.CheckMiniAppPaymentReview(ctx, scope, session, event); err != nil {
		miniAppPaymentError(w, err)
		return
	}
	record, err := s.store.PaymentEvidenceFile(ctx, scope, event, evidence)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	// Refresh IAM as well as the local role/session after the remote source read.
	scope, session, err = s.miniAppPaymentScope(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	current, err := s.store.PaymentEvidenceFile(ctx, scope, event, evidence)
	if err == nil && (current.ID != record.ID || current.TelegramID != record.TelegramID) {
		err = db.ErrConflict
	}
	if err == nil {
		err = s.store.CheckMiniAppPaymentReview(ctx, scope, session, event)
	}
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	w.Header().Set("Content-Type", fileContentType(raw))
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- authorized bounded attachment uses nosniff and sandbox CSP.
}
