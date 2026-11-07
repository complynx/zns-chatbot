package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

func (s *Server) miniAppAvatarCropOpen(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, err := avatarDraftLocator(r)
	if err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero saved draft ID required")
		return
	}
	var input struct {
		OperationID uuid.UUID `json:"operation_id"`
	}
	if !decodeMiniAppAvatar(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "nonzero operation ID required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.RecordMiniAppAvatarCropOpen(ctx, miniAppOrdersScope(attempt), bearer,
		s.miniAppKey.Generation(), id, input.OperationID)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	s.writeMiniAppAvatar(ctx, w, r, result)
}
