package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type OwnedPair struct {
	Pair      registrations.Pair `json:"pair"`
	EventName string             `json:"event_name"`
	Timezone  string             `json:"timezone"`
	StartsAt  time.Time          `json:"starts_at"`
	EndsAt    time.Time          `json:"ends_at"`
}

// OwnedPairs lists active relationships under the current participant identity.
// Closed or disabled events remain visible for cleanup; admin rights do not widen ownership.
func (s *Store) OwnedPairs(
	ctx context.Context,
	scope PaymentScope,
	eventID, after uuid.UUID,
	limit int32,
) ([]OwnedPair, error) {
	if !paymentScopeValid(scope, "read") || limit < 1 || limit > pairReadPageLimit {
		return nil, registrations.ErrInvalid
	}
	rows, err := s.q.ListOwnedPairs(ctx, sqlc.ListOwnedPairsParams{
		UserID: scope.UserID, Subject: scope.Subject, BotID: scope.BotID, OrganizationID: scope.Organization,
		EventID: eventID, AfterID: after, PageLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]OwnedPair, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			OwnedPair{Pair: pairRecord(row.ShuttlePair), EventName: row.EventName, Timezone: row.Timezone,
				StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC()},
		)
	}
	return result, nil
}
