package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const assignmentProposalPageLimit = 20

type AssignmentProposalState string

const (
	AssignmentProposalPending   AssignmentProposalState = "pending"
	AssignmentProposalExpired   AssignmentProposalState = "expired"
	AssignmentProposalCompleted AssignmentProposalState = "completed"
)

// AssignmentProposalView separates immutable approved terms from observed outcome.
// State and result are snapshots, never consent to assign or adopt.
type AssignmentProposalView struct {
	OriginalOperationID string                      `json:"original_operation_id"`
	Preparation         AssignmentPreparation       `json:"preparation"`
	State               AssignmentProposalState     `json:"state"`
	Result              *registrations.Registration `json:"result,omitempty"`
	ObservedAt          time.Time                   `json:"observed_at"`
}

type AssignmentProposalPage struct {
	Proposals []AssignmentProposalView `json:"proposals"`
	Next      *uuid.UUID               `json:"next,omitempty"`
}

// AssignmentProposal returns only a proposal owned by this authenticated actor.
// The caller must enforce current administrator permission.
func (s *Store) AssignmentProposal(ctx context.Context, scope EventScope, actor string,
	proposal uuid.UUID,
) (AssignmentProposalView, error) {
	if scope.BotID <= 0 || scope.Organization == "" || actor == "" || proposal == uuid.Nil {
		return AssignmentProposalView{}, registrations.ErrInvalid
	}
	row, err := s.q.GetAssignmentProposal(ctx, sqlc.GetAssignmentProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, ProposalID: proposal,
	})
	if err != nil {
		return AssignmentProposalView{}, err
	}
	now, err := s.q.RegistrationNow(ctx)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	return assignmentProposalView(ctx, s.q, row, now.Time)
}

func assignmentProposalView(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttleAssignmentPreparation,
	now time.Time,
) (AssignmentProposalView, error) {
	preparation, err := decodeAssignmentPreparation(row)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	view := AssignmentProposalView{
		OriginalOperationID: row.OperationID,
		Preparation:         preparation,
		State:               AssignmentProposalPending,
		ObservedAt:          now.UTC(),
	}
	if !now.Before(row.ExpiresAt.Time) {
		view.State = AssignmentProposalExpired
	}
	receipt, err := q.GetRegistrationOperation(ctx, sqlc.GetRegistrationOperationParams{
		BotID: row.BotID, OrganizationID: row.OrganizationID, Actor: row.Actor, OperationID: row.OperationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return AssignmentProposalView{}, err
	}
	result, err := decodeRegistrationReceipt(receipt, row.RequestHash)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	view.State = AssignmentProposalCompleted
	view.Result = &result
	return view, nil
}

// PendingAssignmentProposals lists bounded actor-owned pending preparations in UUID order.
func (s *Store) PendingAssignmentProposals(ctx context.Context, scope EventScope, actor string,
	after uuid.UUID, limit int,
) (AssignmentProposalPage, error) {
	if scope.BotID <= 0 || scope.Organization == "" || actor == "" || limit < 1 || limit > assignmentProposalPageLimit {
		return AssignmentProposalPage{}, registrations.ErrInvalid
	}
	now, err := s.q.RegistrationNow(ctx)
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	rows, err := s.q.ListPendingAssignmentProposals(ctx, sqlc.ListPendingAssignmentProposalsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, AfterID: after,
		NowAt: pgtype.Timestamptz{Time: now.Time, Valid: true}, PageLimit: int32(limit + 1),
	})
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	page := AssignmentProposalPage{Proposals: make([]AssignmentProposalView, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].ProposalID
		page.Next = &last
	}
	for _, row := range rows {
		view, readErr := assignmentProposalView(ctx, s.q, row, now.Time)
		if readErr != nil {
			return AssignmentProposalPage{}, readErr
		}
		page.Proposals = append(page.Proposals, view)
	}
	return page, nil
}
