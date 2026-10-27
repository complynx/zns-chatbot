package api

import (
	"context"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Proof bytes stay behind owner, event and current session checks on both sides of Telegram IO.
func (s *Server) miniAppPassPaymentContent(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := miniAppPassPaymentID(r.PathValue("event"))
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	file, err := miniAppPassPaymentID(r.PathValue("file"))
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), fileTimeoutMillis*time.Millisecond)
	defer cancel()
	scope, session, err := s.miniAppPassPaymentSession(ctx, r)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	record, err := s.store.MiniAppPaymentEvidenceSource(ctx, scope, session, event, file)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	if _, err = s.miniAppPassPaymentRefresh(ctx, r, scope, session); err != nil {
		miniAppPaymentError(w, err)
		return
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	fresh, err := s.miniAppPassPaymentRefresh(ctx, r, scope, session)
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	current, err := s.store.MiniAppPaymentEvidenceSource(ctx, scope, fresh, event, file)
	if err == nil && current != record {
		err = db.ErrConflict
	}
	if err == nil {
		_, err = s.miniAppPassPaymentRefresh(ctx, r, scope, session)
	}
	if err != nil {
		miniAppPaymentError(w, err)
		return
	}
	w.Header().Set("Content-Type", fileContentType(raw))
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- bounded authorized attachment; nosniff and sandbox CSP apply.
}
