package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Browsing historical photos never creates a draft or retains image bytes.
func (s *Server) miniAppAvatarHistoryPreview(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	archive, err := uuid.Parse(r.PathValue("archive"))
	if err != nil || archive == uuid.Nil {
		http.NotFound(w, r)
		return
	}
	record, err := uuid.Parse(r.PathValue("id"))
	if err != nil || record == uuid.Nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), historyAvatarReadTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	scope := miniAppOrdersScope(attempt)
	source := &db.AvatarHistorySource{ArchiveID: archive, RecordID: record}
	raw, err := s.readAvatarCreationSource(ctx, scope, db.AvatarDraftCreate{HistorySource: source})
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	rendered, err := s.validateAvatarOriginal(ctx, raw)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	// Recheck archive membership and current ownership after remote IO/rendering.
	if err = s.beginArchiveValidation(); err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	_, err = s.store.OwnerHistoryAvatarSource(ctx, scope, record, archive, func(context.Context) error { return nil })
	s.endArchiveValidation()
	if err != nil {
		s.miniAppAvatarError(w, historyError(err))
		return
	}
	current, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	if miniAppOrdersScope(current) != scope {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mimeJPEG)
	w.Header().Set("Content-Disposition", `attachment; filename="avatar-history-preview.jpg"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(rendered.JPEG) // #nosec G705 -- bounded renderer JPEG with attachment, nosniff and sandbox CSP.
}
