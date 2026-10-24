package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massagePendingCancelKind = "cancel"

// MassagePendingProposal is informational. Its presence is not consent or a capacity hold.
// Source and CreatedAt describe original creation, not later interface adoption.
type MassagePendingProposal struct {
	ID           uuid.UUID                     `json:"proposal_id"`
	EventID      uuid.UUID                     `json:"event_id"`
	Kind         string                        `json:"kind"`
	Source       MassageBookingSource          `json:"source"`
	CreatedAt    time.Time                     `json:"created_at"`
	ExpiresAt    time.Time                     `json:"expires_at"`
	Booking      *MassageBookingArguments      `json:"booking,omitempty"`
	Instant      *MassageInstantArguments      `json:"instant,omitempty"`
	Cancellation *MassageCancellationArguments `json:"cancellation,omitempty"`
}

type MassagePendingPage struct {
	Proposals []MassagePendingProposal `json:"proposals"`
	NextAfter *uuid.UUID               `json:"next_after,omitempty"`
}

// OwnPendingMassageProposals reads active own choices without requiring current
// booking entitlement. Adoption and apply recheck current terms and permissions.
func (s *Store) OwnPendingMassageProposals(ctx context.Context, scope ProfileScope,
	eventID, after uuid.UUID, limit int,
) (MassagePendingPage, error) {
	if eventID == uuid.Nil || limit < 0 || limit > 20 {
		return MassagePendingPage{}, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, eventID)
	if err != nil {
		return MassagePendingPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readPendingMassageProposals(ctx, q, scope, eventID, after, limit)
}

func (s *Store) OwnMiniAppPendingMassageProposals(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, eventID, after uuid.UUID, limit int,
) (MassagePendingPage, error) {
	if eventID == uuid.Nil || limit < 0 || limit > 20 {
		return MassagePendingPage{}, massage.ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	tx, q, _, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, eventID)
	if err != nil {
		return MassagePendingPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readPendingMassageProposals(ctx, q, scope, eventID, after, limit)
}

func readPendingMassageProposals(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	eventID, after uuid.UUID, limit int,
) (MassagePendingPage, error) {
	if limit < 1 || limit > 20 {
		return MassagePendingPage{}, massage.ErrInvalid
	}
	result := MassagePendingPage{Proposals: []MassagePendingProposal{}}
	rows, err := q.ListOwnPendingMassagePreparations(ctx, sqlc.ListOwnPendingMassagePreparationsParams{
		EventID: eventID, UserID: scope.UserID, ID: after, Limit: int32(limit + 1),
	})
	if err != nil {
		return result, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[len(rows)-1].ID
		result.NextAfter = &next
	}
	result.Proposals = make([]MassagePendingProposal, len(rows))
	for i, row := range rows {
		result.Proposals[i], err = massagePendingProposal(row)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func massagePendingProposal(row sqlc.ShuttleMassagePreparation) (MassagePendingProposal, error) {
	var err error
	item := MassagePendingProposal{
		ID:        row.ID,
		EventID:   row.EventID,
		Kind:      row.Kind,
		Source:    MassageBookingSource(row.Source),
		CreatedAt: row.CreatedAt.Time.UTC(),
		ExpiresAt: row.ExpiresAt.Time.UTC(),
	}
	switch row.Kind {
	case "book":
		item.Booking = &MassageBookingArguments{}
		err = json.Unmarshal(row.Arguments, item.Booking)
	case massagePendingCancelKind:
		item.Cancellation = &MassageCancellationArguments{}
		err = json.Unmarshal(row.Arguments, item.Cancellation)
	case massageInstantKind:
		item.Instant = &MassageInstantArguments{}
		err = json.Unmarshal(row.Arguments, item.Instant)
	default:
		return item, massage.ErrInvalid
	}
	if err != nil {
		return item, err
	}
	return item, nil
}
