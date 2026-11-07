package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

// Caller holds the event lock and owner's SHARE lock until the effect commits.
// Details writers take that owner's UPDATE lock, so readiness cannot race a write.
func requireRegistrationDetails(ctx context.Context, q *sqlc.Queries, event events.Event, userID uuid.UUID) error {
	requirements := event.Document.Registration.DetailsRequirements
	if requirements == nil {
		return nil
	}
	status, err := registrationDetailsStatus(ctx, q, userID)
	if err != nil {
		return err
	}
	missing := requirements.Missing(status)
	if len(missing) == 0 {
		return nil
	}
	return &registrationdetails.MissingError{MissingFields: missing, DetailsVersion: status.Version}
}
