package db

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type MiniAppPairAssignmentCandidate struct {
	Arguments PairAssignment            `json:"arguments"`
	Pair      registrations.Pair        `json:"pair"`
	Leader    PairAssignmentParticipant `json:"leader"`
	Follower  PairAssignmentParticipant `json:"follower"`
}

type MiniAppPairAssignmentCandidates struct {
	Candidates []MiniAppPairAssignmentCandidate `json:"candidates"`
	Next       *uuid.UUID                       `json:"next,omitempty"`
}

// Single-event reads lock event then actor then session. Cross-event lists
// take no event locks after the actor barrier; SQL filters current authority.
func (s *Store) beginPairAssignmentRead(ctx context.Context, scope PaymentScope,
	event uuid.UUID, session MiniAppPairAssignmentSession,
) (pgx.Tx, *sqlc.Queries, error) {
	if !paymentScopeValid(scope, "read") {
		return nil, nil, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	if event != uuid.Nil {
		err = lockPairAssignmentAuthority(ctx, q, scope, event)
	}
	if err == nil {
		err = authorizePairAssignmentMiniApp(ctx, q, scope, &session)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, q, nil
}

func (s *Store) MiniAppPairAssignmentEvents(ctx context.Context, scope PaymentScope,
	session MiniAppPairAssignmentSession, after uuid.UUID,
) (MiniAppAssignmentEvents, error) {
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, uuid.Nil, session)
	if err != nil {
		return MiniAppAssignmentEvents{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListPairAssignmentEvents(ctx, sqlc.ListPairAssignmentEventsParams{
		UserID: scope.UserID, Subject: scope.Subject, BotID: scope.BotID, OrganizationID: scope.Organization,
		AfterID: after, GlobalAdmin: scope.GlobalAdmin, PageLimit: pairReadPageLimit + 1,
	})
	if err != nil {
		return MiniAppAssignmentEvents{}, err
	}
	page := MiniAppAssignmentEvents{Events: make([]events.Event, 0, len(rows))}
	if len(rows) > pairReadPageLimit {
		last := rows[pairReadPageLimit-1].ID
		page.Next = &last
		rows = rows[:pairReadPageLimit]
	}
	for _, row := range rows {
		event, e := eventRecord(row, nil, nil)
		if e != nil {
			return MiniAppAssignmentEvents{}, e
		}
		page.Events = append(page.Events, event)
	}
	return page, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}

// MiniAppPairAssignmentCandidates returns a queue snapshot, not reserved capacity or an eligibility
// promise. Only display names and Telegram IDs are exposed, never legal data.
func (s *Store) MiniAppPairAssignmentCandidates(ctx context.Context, scope PaymentScope,
	session MiniAppPairAssignmentSession, event, after uuid.UUID,
) (MiniAppPairAssignmentCandidates, error) {
	if event == uuid.Nil {
		return MiniAppPairAssignmentCandidates{}, registrations.ErrInvalid
	}
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, event, session)
	if err != nil {
		return MiniAppPairAssignmentCandidates{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	current, err := q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return MiniAppPairAssignmentCandidates{}, err
	}
	if !slices.Contains(current.Modules, "registrations") {
		return MiniAppPairAssignmentCandidates{}, registrations.ErrUnavailable
	}
	rows, err := q.ListPairAssignmentCandidates(ctx, sqlc.ListPairAssignmentCandidatesParams{
		EventID: event, AfterID: after, PageLimit: pairReadPageLimit + 1,
	})
	if err != nil {
		return MiniAppPairAssignmentCandidates{}, err
	}
	page := MiniAppPairAssignmentCandidates{Candidates: make([]MiniAppPairAssignmentCandidate, 0, len(rows))}
	if len(rows) > pairReadPageLimit {
		last := rows[pairReadPageLimit-1].ShuttlePair.ID
		page.Next = &last
		rows = rows[:pairReadPageLimit]
	}
	for _, row := range rows {
		leader, active, e := pairAssignmentParticipant(ctx, tx, q, row.LeaderUserID)
		if e != nil {
			return MiniAppPairAssignmentCandidates{}, e
		}
		if !active {
			continue
		}
		follower, active, e := pairAssignmentParticipant(ctx, tx, q, row.FollowerUserID)
		if e != nil {
			return MiniAppPairAssignmentCandidates{}, e
		}
		if !active {
			continue
		}
		page.Candidates = append(page.Candidates, MiniAppPairAssignmentCandidate{
			Arguments: PairAssignment{
				EventID:                 event,
				PairID:                  row.ShuttlePair.ID,
				ExpectedEventVersion:    current.Version,
				ExpectedPairVersion:     row.ShuttlePair.Version,
				ExpectedLeaderVersion:   row.LeaderVersion,
				ExpectedFollowerVersion: row.FollowerVersion,
			},
			Pair: pairRecord(row.ShuttlePair), Leader: leader, Follower: follower,
		})
	}
	return page, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}

// MiniAppPairAssignmentProposal resolves an actor-owned selector before beginning the transaction;
// the lookup releases its lease, so even a one-connection pool can serve reads.
// Current event authority and the session apply to historical results as well.
func (s *Store) MiniAppPairAssignmentProposal(ctx context.Context, scope PaymentScope,
	session MiniAppPairAssignmentSession, proposal uuid.UUID, operation string,
) (PairAssignmentProposalView, error) {
	if !paymentScopeValid(scope, "read") || (proposal == uuid.Nil) == (operation == "") || len(operation) > 128 {
		return PairAssignmentProposalView{}, registrations.ErrInvalid
	}
	var row sqlc.ShuttlePairPreparation
	var err error
	if proposal != uuid.Nil {
		row, err = s.q.GetPairAssignmentProposal(ctx, sqlc.GetPairAssignmentProposalParams{
			BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, ProposalID: proposal,
		})
	} else {
		row, err = s.q.GetPairPreparation(ctx, sqlc.GetPairPreparationParams{
			BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: operation,
		})
	}
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	prep, err := decodePairAssignmentPreparation(row)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, prep.Arguments.EventID, session)
	if err != nil {
		return PairAssignmentProposalView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err = q.LockPairAssignmentCanonicalPreparation(ctx, sqlc.LockPairAssignmentCanonicalPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: row.OperationID,
	})
	if err != nil {
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
	return view, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}

func (s *Store) PendingMiniAppPairAssignmentProposals(ctx context.Context, scope PaymentScope,
	session MiniAppPairAssignmentSession, after uuid.UUID,
) (PairAssignmentProposalPage, error) {
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, uuid.Nil, session)
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	page, err := pendingPairAssignmentProposals(ctx, tx, q, scope, after, pairReadPageLimit)
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	return page, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}

// RecentMiniAppPairAssignmentProposals recovers canonical proposals after a lost browser
// journal. Current event access is filtered before LIMIT; results grant no consent.
func (s *Store) RecentMiniAppPairAssignmentProposals(ctx context.Context, scope PaymentScope,
	session MiniAppPairAssignmentSession,
) (PairAssignmentProposalPage, error) {
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, uuid.Nil, session)
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.RecentNativePairAssignmentProposals(ctx, sqlc.RecentNativePairAssignmentProposalsParams{
		UserID:         scope.UserID,
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		GlobalAdmin:    scope.GlobalAdmin,
	})
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return PairAssignmentProposalPage{}, err
	}
	page := PairAssignmentProposalPage{Proposals: make([]PairAssignmentProposalView, 0, len(rows))}
	for _, row := range rows {
		view, e := pairAssignmentProposalView(ctx, q, row, now.Time)
		if e != nil {
			return PairAssignmentProposalPage{}, e
		}
		page.Proposals = append(page.Proposals, view)
	}
	for i := range page.Proposals {
		if err = enrichPairAssignmentView(ctx, tx, q, &page.Proposals[i]); err != nil {
			return PairAssignmentProposalPage{}, err
		}
	}
	return page, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}
