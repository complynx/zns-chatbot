package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

// MassageSpecialistNotificationSummary excludes client names and outbound payloads.
type MassageSpecialistNotificationSummary struct {
	ID                uuid.UUID `json:"id"`
	BookingID         uuid.UUID `json:"booking_id"`
	EventID           uuid.UUID `json:"event_id"`
	RecipientID       uuid.UUID `json:"recipient_id"`
	Kind              string    `json:"kind"`
	State             string    `json:"state"`
	Version           int64     `json:"version"`
	BindingVersion    int64     `json:"binding_version"`
	PreferenceVersion int64     `json:"preference_version"`
	NextAttemptAt     time.Time `json:"next_attempt_at"`
	CreatedAt         time.Time `json:"created_at"`
}

type MassageSpecialistNotificationDetail struct {
	MassageSpecialistNotificationSummary

	AttemptID uuid.UUID       `json:"attempt_id"`
	Receipt   json.RawMessage `json:"receipt"`
}

// ListAdminMassageSpecialistNotifications requires administrator authorization
// at the transport boundary. It scopes every row to the current deployment.
func (s *Store) ListAdminMassageSpecialistNotifications(
	ctx context.Context,
	scope EventScope,
	state string,
	after uuid.UUID,
	limit int32,
) ([]MassageSpecialistNotificationSummary, error) {
	if limit < 1 || limit > 20 {
		return nil, payments.ErrInvalid
	}
	rows, err := s.q.ListAdminMassageSpecialistNotifications(
		ctx,
		sqlc.ListAdminMassageSpecialistNotificationsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			FilterState:    state,
			AfterID:        after,
			PageLimit:      limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]MassageSpecialistNotificationSummary, len(rows))
	for i, row := range rows {
		result[i] = MassageSpecialistNotificationSummary{
			ID:                row.ID,
			BookingID:         row.BookingID,
			EventID:           row.EventID,
			RecipientID:       row.RecipientID,
			Kind:              row.Kind,
			State:             row.State,
			Version:           row.Version,
			BindingVersion:    row.BindingVersion,
			PreferenceVersion: row.PreferenceVersion,
			NextAttemptAt:     row.NextAttemptAt.Time.UTC(),
			CreatedAt:         row.CreatedAt.Time.UTC(),
		}
	}
	return result, nil
}

// AdminMassageSpecialistNotification exposes attempt/receipt diagnostics without
// returning the stored Telegram payload or a client's contact/profile.
func (s *Store) AdminMassageSpecialistNotification(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (MassageSpecialistNotificationDetail, error) {
	n, err := readMassageSpecialistNotification(ctx, s.q, scope, id)
	if err != nil {
		return MassageSpecialistNotificationDetail{}, err
	}
	summary := MassageSpecialistNotificationSummary{
		ID:                n.ID,
		BookingID:         n.Booking.ID,
		EventID:           n.Booking.EventID,
		RecipientID:       n.RecipientID,
		Kind:              n.Kind,
		State:             n.State,
		Version:           n.Version,
		BindingVersion:    n.BindingVersion,
		PreferenceVersion: n.PreferenceVersion,
		NextAttemptAt:     n.NextAttemptAt,
		CreatedAt:         n.CreatedAt,
	}
	return MassageSpecialistNotificationDetail{
		MassageSpecialistNotificationSummary: summary,
		AttemptID:                            n.AttemptID,
		Receipt:                              n.Receipt,
	}, nil
}
