package task

import (
	"context"

	"github.com/complynx/zns-chatbot/internal/db"
)

const communicationAdmissionJitter = 4

// Wait for service capacity before IAM and recipient checks. They still run
// after admission and immediately before the message is reserved for sending.
func (s *Service) admitCommunicationPersonalization(
	ctx context.Context,
	d db.CommunicationDelivery,
	c db.Communication,
) (db.CommunicationDelivery, error) {
	if !c.Arguments.Personalized || d.Kind != kindMessage || d.Text != "" || d.PersonalizationStartedAt != nil {
		return d, nil
	}
	admitted, err := s.store.AdmitCommunicationPersonalization(
		ctx,
		s.notificationScope(),
		d.ID,
		communicationPersonalizationLimit,
	)
	if err != nil {
		return d, err
	}
	if !admitted {
		return d, s.snoozeCommunicationPersonalization(ctx, d)
	}
	current, _, err := s.store.CommunicationDelivery(ctx, s.notificationScope(), d.ID)
	return current, err
}
