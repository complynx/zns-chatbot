package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

// miniAppEventList shares pagination and current-session checks; each module
// supplies its own entitlement query and storage-error mapping.
func (s *Server) miniAppEventList(w http.ResponseWriter, r *http.Request,
	list func(context.Context, db.ProfileScope, uuid.UUID) (events.ContextList, error),
	handleError func(context.Context, http.ResponseWriter, *http.Request, error),
) {
	if !s.miniAppRequest(w, r) {
		return
	}
	after := uuid.Nil
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = uuid.Parse(raw)
		if err != nil || after == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid event cursor")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := list(ctx, miniAppOrdersScope(attempt), after)
	if err != nil {
		handleError(ctx, w, r, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
