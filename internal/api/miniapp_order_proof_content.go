package api

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) miniAppOrderProofContent(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	file, err := uuid.Parse(r.PathValue("file"))
	if err != nil || file == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero file UUID required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), fileTimeoutMillis*time.Millisecond)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	record, err := s.store.MiniAppOrderEvidenceSource(ctx, s.eventScope(), session, event, file)
	if err != nil {
		s.miniAppOrderProofResult(ctx, w, r, nil, err)
		return
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		s.miniAppOrderProofResult(ctx, w, r, nil, err)
		return
	}
	// Refresh IAM/session as well as owner/event entitlement after Telegram IO.
	session, err = s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	current, err := s.store.MiniAppOrderEvidenceSource(ctx, s.eventScope(), session, event, file)
	if err == nil && (current.ID != record.ID || current.TelegramID != record.TelegramID) {
		err = db.ErrConflict
	}
	if err != nil {
		s.miniAppOrderProofResult(ctx, w, r, nil, err)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", fileContentType(raw))
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- bounded authorized attachment; nosniff and sandbox CSP apply.
}
