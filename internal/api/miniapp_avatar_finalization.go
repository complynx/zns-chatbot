package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type miniAppAvatarFinalize struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ExpectedVersion int64     `json:"expected_version"`
}

func (s *Server) registerMiniAppAvatarFinalization() {
	s.public.HandleFunc("POST /app/avatars/drafts/{id}/finalize", s.miniAppAvatarFinalize)
	s.public.HandleFunc("GET /app/avatars/finalizations/operations/{operation}", s.miniAppAvatarFinalizationRead)
	s.public.HandleFunc("GET /app/avatars/finalized", s.miniAppAvatarFinalizationRead)
	s.public.HandleFunc("GET /app/avatars/finalized/{id}", s.miniAppAvatarFinalizationRead)
	s.public.HandleFunc("GET /app/avatars/finalized/{id}/content", s.miniAppFinalizedAvatarContent)
}
func (s *Server) miniAppAvatarFinalize(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, err := avatarDraftLocator(r)
	if err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero saved draft ID required")
		return
	}
	var input miniAppAvatarFinalize
	if !decodeMiniAppAvatar(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil || input.ExpectedVersion < 1 {
		miniAppHTTPError(w, http.StatusBadRequest, "operation ID and positive draft version required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), avatarUploadTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	scope := miniAppOrdersScope(attempt)
	a := db.AvatarDraftFinalize{OperationID: input.OperationID, DraftID: id, ExpectedVersion: input.ExpectedVersion}
	rendered, err := s.renderAvatarFinalization(ctx, scope, a)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	current, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	if miniAppOrdersScope(current) != scope {
		miniAppHTTPError(w, http.StatusNotFound, "avatar owner unavailable")
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	receipt, err := s.store.CommitMiniAppAvatarDraft(ctx, scope, bearer, s.miniAppKey.Generation(), a, rendered)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	s.writeMiniAppAvatar(ctx, w, r, receipt)
}
func (s *Server) miniAppAvatarFinalizationRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	scope := miniAppOrdersScope(attempt)
	var result any
	if raw := r.PathValue("operation"); raw != "" {
		op, parseErr := uuid.Parse(raw)
		if parseErr != nil || op == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "nonzero finalization operation required")
			return
		}
		result, err = s.store.AvatarDraftFinalizationOperation(ctx, scope, op)
	} else if r.PathValue("id") == "" {
		rows, listErr := s.store.ListFinalizedAvatars(ctx, scope)
		result, err = avatarFinalizationList{Avatars: rows}, listErr
	} else {
		id, parseErr := avatarDraftLocator(r)
		if parseErr != nil {
			miniAppHTTPError(w, http.StatusBadRequest, "nonzero artifact ID required")
			return
		}
		result, err = s.store.FinalizedAvatarStatus(ctx, scope, id)
	}
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	s.writeMiniAppAvatar(ctx, w, r, result)
}
func (s *Server) miniAppFinalizedAvatarContent(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, err := avatarDraftLocator(r)
	if err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero artifact ID required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	_, raw, err := s.store.GetFinalizedAvatar(ctx, miniAppOrdersScope(attempt), id)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	writeFinalizedAvatar(w, raw)
}
