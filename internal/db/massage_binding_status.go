package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

// MassageBindingProposalStatus describes the canonical proposal, independent of
// which interface operation adopted it. A receipt is historical, not new consent.
type MassageBindingProposalStatus struct {
	ID          uuid.UUID                 `json:"proposal_id"`
	Arguments   MassageBindingChange      `json:"arguments"`
	ExpiresAt   time.Time                 `json:"expires_at"`
	OperationID uuid.UUID                 `json:"operation_id"`
	State       string                    `json:"state"`
	Result      *MassageSpecialistBinding `json:"result,omitempty"`
}

func massageBindingStatus(
	row sqlc.ShuttleMassageBindingOperation,
	now time.Time,
) (MassageBindingProposalStatus, error) {
	result := MassageBindingProposalStatus{
		OperationID: row.OperationID,
		ID:          row.ProposalID, ExpiresAt: row.ExpiresAt.Time.UTC(),
		State: miniAppPending,
	}
	if err := json.Unmarshal(row.Arguments, &result.Arguments); err != nil {
		return result, err
	}
	if len(row.Result) > 0 {
		result.State = detailsRequestCompleted
		result.Result = &MassageSpecialistBinding{}
		if err := json.Unmarshal(row.Result, result.Result); err != nil {
			return result, err
		}
	} else if !row.ExpiresAt.Time.After(now) {
		result.State = orderReviewExpired
	}
	return result, nil
}

// MassageBindingStatus requires current global administrator authorization at the
// API boundary. Exactly one selector is accepted; both are scoped to this actor.
func (s *Store) MassageBindingStatus(ctx context.Context, scope ProfileScope, session *MiniAppMassageSession,
	operation, proposal uuid.UUID,
) (MassageBindingProposalStatus, error) {
	var result MassageBindingProposalStatus
	if (operation == uuid.Nil) == (proposal == uuid.Nil) {
		return result, massage.ErrInvalid
	}
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, session, uuid.Nil)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	var row sqlc.ShuttleMassageBindingOperation
	if operation != uuid.Nil {
		row, err = q.ReadMassageBindingOperation(
			ctx,
			sqlc.ReadMassageBindingOperationParams{ActorID: scope.UserID, OperationID: operation},
		)
	} else {
		row, err = q.ReadMassageBindingProposal(
			ctx,
			sqlc.ReadMassageBindingProposalParams{ActorID: scope.UserID, ProposalID: proposal},
		)
	}
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	return massageBindingStatus(row, now.Time)
}

type MassageBindingPendingPage struct {
	Proposals []MassageBindingProposalStatus `json:"proposals"`
	NextAfter string                         `json:"next_after,omitempty"`
}

// PendingMassageBindings pages canonical proposals, twenty at a time, regardless
// of how many interface operations adopted each. No event selection is required.
func (s *Store) PendingMassageBindings(ctx context.Context, scope ProfileScope, session *MiniAppMassageSession,
	eventID, after uuid.UUID,
) (MassageBindingPendingPage, error) {
	result := MassageBindingPendingPage{Proposals: []MassageBindingProposalStatus{}}
	tx, q, _, err := s.massageBindingReadTx(ctx, scope, session, eventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if after != uuid.Nil {
		cursor, cursorErr := q.ReadMassageBindingProposal(
			ctx,
			sqlc.ReadMassageBindingProposalParams{ActorID: scope.UserID, ProposalID: after},
		)
		if cursorErr != nil {
			return result, cursorErr
		}
		if eventID != uuid.Nil && cursor.EventID != eventID {
			return result, massage.ErrInvalid
		}
	}
	rows, err := q.ListPendingMassageBindingProposals(
		ctx,
		sqlc.ListPendingMassageBindingProposalsParams{ActorID: scope.UserID, EventID: eventID, AfterID: after},
	)
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	const pageSize = 20
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		result.NextAfter = rows[len(rows)-1].ProposalID.String()
	}
	for _, row := range rows {
		status, statusErr := massageBindingStatus(row, now.Time)
		if statusErr != nil {
			return result, statusErr
		}
		if status.State == miniAppPending {
			result.Proposals = append(result.Proposals, status)
		}
	}
	return result, nil
}
