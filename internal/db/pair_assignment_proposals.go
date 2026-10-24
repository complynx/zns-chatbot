package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// PairAssignmentProposalView contains original terms and an observed historical
// outcome. Neither a pending proposal nor a historical result grants consent.
type PairAssignmentProposalView struct {
	OriginalOperationID string                    `json:"original_operation_id"`
	Preparation         PairAssignmentPreparation `json:"preparation"`
	State               AssignmentProposalState   `json:"state"`
	Result              *PairAssignmentResult     `json:"result,omitempty"`
	ObservedAt          time.Time                 `json:"observed_at"`
	// Current display identities are bound to the original pair, not frozen terms.
	Leader   *PairAssignmentParticipant `json:"leader,omitempty"`
	Follower *PairAssignmentParticipant `json:"follower,omitempty"`
}

type PairAssignmentProposalPage struct {
	Proposals []PairAssignmentProposalView `json:"proposals"`
	Next      *uuid.UUID                   `json:"next,omitempty"`
}

// PairAssignmentProposal checks both actor ownership and current event authority,
// including for a completed receipt after the proposal expires or sales close.
func (s *Store) PairAssignmentProposal(ctx context.Context, scope PaymentScope,
	proposal uuid.UUID,
) (PairAssignmentProposalView, error) {
	if !paymentScopeValid(scope, "proposal") || proposal == uuid.Nil {
		return PairAssignmentProposalView{}, registrations.ErrInvalid
	}
	row, err := s.q.GetPairAssignmentProposal(ctx, sqlc.GetPairAssignmentProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, ProposalID: proposal,
	})
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	prep, err := decodePairAssignmentPreparation(row)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockPairAssignment(ctx, q, scope, "proposal:"+proposal.String(), prep.Arguments.EventID); err != nil {
		return PairAssignmentProposalView{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	view, err := pairAssignmentProposalView(ctx, q, row, now.Time)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	if err = enrichPairAssignmentView(ctx, tx, q, &view); err != nil {
		return PairAssignmentProposalView{}, err
	}
	return view, nil
}

func pairAssignmentProposalView(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttlePairPreparation,
	now time.Time,
) (PairAssignmentProposalView, error) {
	prep, err := decodePairAssignmentPreparation(row)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	view := PairAssignmentProposalView{OriginalOperationID: row.OperationID, Preparation: prep,
		State: AssignmentProposalPending, ObservedAt: now.UTC()}
	if !now.Before(row.ExpiresAt.Time) {
		view.State = AssignmentProposalExpired
	}
	receipt, err := q.GetPairOperation(ctx, sqlc.GetPairOperationParams{
		BotID: row.BotID, OrganizationID: row.OrganizationID, Actor: row.Actor, OperationID: row.OperationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	if receipt.Kind != pairAssign || !bytes.Equal(receipt.RequestHash, row.RequestHash) {
		return PairAssignmentProposalView{}, ErrConflict
	}
	var result PairAssignmentResult
	if err = json.Unmarshal(receipt.Result, &result); err != nil {
		return PairAssignmentProposalView{}, err
	}
	if result.Pair.ID != prep.Arguments.PairID || result.Pair.EventID != prep.Arguments.EventID {
		return PairAssignmentProposalView{}, ErrConflict
	}
	view.State, view.Result = AssignmentProposalCompleted, &result
	return view, nil
}

// PendingPairAssignmentProposals filters current event authority before pagination.
// It holds one actor read lock and one pool lease, with no later event locks.
func (s *Store) PendingPairAssignmentProposals(ctx context.Context, scope PaymentScope,
	after uuid.UUID, limit int,
) (PairAssignmentProposalPage, error) {
	if !paymentScopeValid(scope, "proposals") || limit < 1 || limit > pairReadPageLimit {
		return PairAssignmentProposalPage{}, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockPairAssignmentActor(ctx, q, scope); err != nil {
		return PairAssignmentProposalPage{}, err
	}
	return pendingPairAssignmentProposals(ctx, tx, q, scope, after, limit)
}

func pendingPairAssignmentProposals(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope PaymentScope,
	after uuid.UUID, limit int,
) (PairAssignmentProposalPage, error) {
	if limit < 1 || limit > pairReadPageLimit {
		return PairAssignmentProposalPage{}, registrations.ErrInvalid
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	rows, err := q.ListPendingPairAssignmentProposals(ctx, sqlc.ListPendingPairAssignmentProposalsParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		UserID:         scope.UserID,
		GlobalAdmin:    scope.GlobalAdmin,
		AfterID:        after,
		NowAt:          pgtype.Timestamptz{Time: now.Time, Valid: true},
		PageLimit:      int32(limit + 1),
	})
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	page := PairAssignmentProposalPage{Proposals: make([]PairAssignmentProposalView, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].ProposalID
		page.Next = &last
	}
	for _, row := range rows {
		view, e := pairAssignmentProposalView(ctx, q, row, now.Time)
		if e != nil {
			return PairAssignmentProposalPage{}, e
		}
		if e = enrichPairAssignmentView(ctx, tx, q, &view); e != nil {
			return PairAssignmentProposalPage{}, e
		}
		page.Proposals = append(page.Proposals, view)
	}
	return page, nil
}
