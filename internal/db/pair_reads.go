package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const pairReadPageLimit = 20

type PairAssignmentCandidate struct {
	Arguments      PairAssignment     `json:"arguments"`
	Pair           registrations.Pair `json:"pair"`
	LeaderUserID   uuid.UUID          `json:"leader_user_id"`
	FollowerUserID uuid.UUID          `json:"follower_user_id"`
}

// PairAssignmentEvents lists event metadata under current account and assignment authority.
func (s *Store) PairAssignmentEvents(
	ctx context.Context,
	scope PaymentScope,
	after uuid.UUID,
	limit int32,
) ([]events.Event, error) {
	if !paymentScopeValid(scope, "read") || limit < 1 || limit > pairReadPageLimit {
		return nil, registrations.ErrInvalid
	}
	rows, err := s.q.ListPairAssignmentEvents(ctx, sqlc.ListPairAssignmentEventsParams{
		UserID: scope.UserID, Subject: scope.Subject, BotID: scope.BotID, OrganizationID: scope.Organization,
		AfterID: after, GlobalAdmin: scope.GlobalAdmin, PageLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]events.Event, 0, len(rows))
	for _, row := range rows {
		event, decodeErr := eventRecord(row, nil, nil)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, event)
	}
	return result, nil
}

// PairAssignmentCandidates returns current IDs and versions without partner financial data.
// A candidate is a queue snapshot, not a capacity reservation or an eligibility guarantee.
func (s *Store) PairAssignmentCandidates(
	ctx context.Context,
	scope PaymentScope,
	eventID, after uuid.UUID,
	limit int32,
) ([]PairAssignmentCandidate, error) {
	if !paymentScopeValid(scope, "read") || eventID == uuid.Nil || limit < 1 || limit > pairReadPageLimit {
		return nil, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockPairAssignment(ctx, q, scope, "read-pair-candidates", eventID); err != nil {
		return nil, err
	}
	event, err := q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListPairAssignmentCandidates(
		ctx,
		sqlc.ListPairAssignmentCandidatesParams{EventID: eventID, AfterID: after, PageLimit: limit},
	)
	if err != nil {
		return nil, err
	}
	result := make([]PairAssignmentCandidate, 0, len(rows))
	for _, row := range rows {
		result = append(result, PairAssignmentCandidate{
			Arguments: PairAssignment{
				EventID:                 eventID,
				PairID:                  row.ShuttlePair.ID,
				ExpectedEventVersion:    event.Version,
				ExpectedPairVersion:     row.ShuttlePair.Version,
				ExpectedLeaderVersion:   row.LeaderVersion,
				ExpectedFollowerVersion: row.FollowerVersion,
			},
			Pair: pairRecord(row.ShuttlePair), LeaderUserID: row.LeaderUserID, FollowerUserID: row.FollowerUserID,
		})
	}
	return result, nil
}
