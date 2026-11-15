package task

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func (s *Service) failCommunication(ctx context.Context, d db.CommunicationDelivery) error {
	current, _, err := s.store.CommunicationDelivery(ctx, s.notificationScope(), d.ID)
	if err != nil {
		return err
	}
	if current.Version != d.Version || (current.State != deliveryPending && current.State != deliveryRetryable) {
		return nil
	}
	d = current
	if d.PersonalizationRunID != "" && d.Text == "" {
		// Cancellation is bounded and best effort. Failure remains terminal locally
		// even if Loom cannot be reached; the run also has its own credit budget.
		cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		service, tokenErr := s.communicationServiceToken(cancelCtx)
		if tokenErr == nil {
			_, _ = s.loom.Cancel(cancelCtx, loom.Auth{ServiceToken: service.Token}, d.PersonalizationRunID,
				"communication-cancel:"+d.ID.String())
		}
	}
	return s.store.FailCommunication(ctx, d)
}
