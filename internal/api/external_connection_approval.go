package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
)

type externalConnectionApprovalDelivery interface {
	DeliverExternalConnectionApproval(context.Context, loom.ExternalConnectionApproval) error
}

func (s *Server) deliverExternalConnectionApproval(
	w http.ResponseWriter,
	r *http.Request,
	approval loom.ExternalConnectionApproval,
) {
	delivery, ok := s.tasks.(externalConnectionApprovalDelivery)
	if !ok {
		http.Error(w, "connection approval unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := delivery.DeliverExternalConnectionApproval(r.Context(), approval); err != nil {
		if errors.Is(err, identity.ErrDenied) {
			http.Error(w, "connection recipient denied", http.StatusForbidden)
			return
		}
		http.Error(w, "connection approval unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}
