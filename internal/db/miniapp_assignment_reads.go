package db

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type MiniAppAssignmentEvents struct {
	Events []events.Event `json:"events"`
	Next   *uuid.UUID     `json:"next,omitempty"`
}

type MiniAppAssignmentParticipant struct {
	Registration   registrations.Registration `json:"registration"`
	TelegramUserID int64                      `json:"telegram_user_id"`
	DisplayName    string                     `json:"display_name"`
}

type MiniAppAssignmentRegistrations struct {
	Registrations []MiniAppAssignmentParticipant `json:"registrations"`
	Next          *uuid.UUID                     `json:"next,omitempty"`
}

func (s *Store) beginAssignmentRead(ctx context.Context, scope EventScope, actor string,
	event uuid.UUID, session MiniAppAssignmentSession,
) (pgx.Tx, *sqlc.Queries, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	if event != uuid.Nil {
		err = lockAssignmentEvent(ctx, q, scope, event)
	}
	if err == nil {
		err = authorizeAssignmentMiniApp(ctx, q, scope, actor, &session)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, q, nil
}

// MiniAppAssignmentEvents lists the deployment's events for a current global
// administrator. It does not imply a participant pass or reserve any capacity.
func (s *Store) MiniAppAssignmentEvents(ctx context.Context, scope EventScope, actor string,
	session MiniAppAssignmentSession, after uuid.UUID,
) (MiniAppAssignmentEvents, error) {
	tx, q, err := s.beginAssignmentRead(ctx, scope, actor, uuid.Nil, session)
	if err != nil {
		return MiniAppAssignmentEvents{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListEventConfigurations(ctx, sqlc.ListEventConfigurationsParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, ID: after, Limit: assignmentProposalPageLimit + 1,
	})
	if err != nil {
		return MiniAppAssignmentEvents{}, err
	}
	page := MiniAppAssignmentEvents{Events: make([]events.Event, 0, len(rows))}
	if len(rows) > assignmentProposalPageLimit {
		last := rows[assignmentProposalPageLimit-1].ShuttleEvent.ID
		page.Next = &last
		rows = rows[:assignmentProposalPageLimit]
	}
	for _, row := range rows {
		event, e := eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
		if e != nil {
			return MiniAppAssignmentEvents{}, e
		}
		page.Events = append(page.Events, event)
	}
	return page, authorizeAssignmentMiniApp(ctx, q, scope, actor, &session)
}

// MiniAppAssignmentRegistrations exposes display names and Telegram identifiers,
// never legal details or passport data. UUID pagination is not waitlist priority.
func (s *Store) MiniAppAssignmentRegistrations(ctx context.Context, scope EventScope, actor string,
	session MiniAppAssignmentSession, event, after uuid.UUID,
) (MiniAppAssignmentRegistrations, error) {
	if event == uuid.Nil {
		return MiniAppAssignmentRegistrations{}, registrations.ErrInvalid
	}
	tx, q, err := s.beginAssignmentRead(ctx, scope, actor, event, session)
	if err != nil {
		return MiniAppAssignmentRegistrations{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListEventRegistrations(ctx, sqlc.ListEventRegistrationsParams{
		EventID:    event,
		BotID:      scope.BotID,
		ZitadelOrg: scope.Organization,
		ID:         after,
		Limit:      assignmentProposalPageLimit + 1,
	})
	if err != nil {
		return MiniAppAssignmentRegistrations{}, err
	}
	page := MiniAppAssignmentRegistrations{Registrations: make([]MiniAppAssignmentParticipant, 0, len(rows))}
	if len(rows) > assignmentProposalPageLimit {
		last := rows[assignmentProposalPageLimit-1].ID
		page.Next = &last
		rows = rows[:assignmentProposalPageLimit]
	}
	for _, row := range rows {
		user, e := q.GetUser(ctx, row.UserID)
		if e != nil {
			return MiniAppAssignmentRegistrations{}, e
		}
		if user.Revoked {
			continue
		}
		if row.State == registrations.Waitlisted {
			if e = pairAssignmentAvailable(ctx, q, row.ID); errors.Is(e, ErrConflict) {
				continue
			} else if e != nil {
				return MiniAppAssignmentRegistrations{}, e
			}
		}
		profile, e := readProfile(ctx, tx, user.ID)
		if e != nil {
			return MiniAppAssignmentRegistrations{}, e
		}
		registration, e := registrationRecord(ctx, q, row)
		if e != nil {
			return MiniAppAssignmentRegistrations{}, e
		}
		page.Registrations = append(page.Registrations, MiniAppAssignmentParticipant{
			Registration: registration, TelegramUserID: user.TgUserID, DisplayName: strings.TrimSpace(profile.FullName),
		})
	}
	return page, authorizeAssignmentMiniApp(ctx, q, scope, actor, &session)
}

// MiniAppAssignmentProposal resolves exactly one actor-owned selector. All reads
// use this transaction's queries; a one-connection pool needs no nested lease.
func (s *Store) MiniAppAssignmentProposal(ctx context.Context, scope EventScope, actor string,
	session MiniAppAssignmentSession, proposal uuid.UUID, operation string,
) (AssignmentProposalView, error) {
	if (proposal == uuid.Nil) == (operation == "") || len(operation) > 128 {
		return AssignmentProposalView{}, registrations.ErrInvalid
	}
	var row sqlc.ShuttleAssignmentPreparation
	var err error
	if proposal != uuid.Nil {
		row, err = s.q.GetAssignmentProposal(ctx, sqlc.GetAssignmentProposalParams{
			BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, ProposalID: proposal,
		})
	} else {
		row, err = s.q.GetAssignmentPreparation(ctx, sqlc.GetAssignmentPreparationParams{
			BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, OperationID: operation,
		})
	}
	if err != nil {
		return AssignmentProposalView{}, err
	}
	prep, err := decodeAssignmentPreparation(row)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	tx, q, err := s.beginAssignmentRead(ctx, scope, actor, prep.Arguments.EventID, session)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err = q.LockAssignmentCanonicalPreparation(ctx, sqlc.LockAssignmentCanonicalPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, OperationID: row.OperationID,
	})
	if err != nil {
		return AssignmentProposalView{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	view, err := assignmentProposalView(ctx, q, row, now.Time)
	if err != nil {
		return AssignmentProposalView{}, err
	}
	return view, authorizeAssignmentMiniApp(ctx, q, scope, actor, &session)
}

func (s *Store) PendingMiniAppAssignmentProposals(ctx context.Context, scope EventScope, actor string,
	session MiniAppAssignmentSession, after uuid.UUID,
) (AssignmentProposalPage, error) {
	tx, q, err := s.beginAssignmentRead(ctx, scope, actor, uuid.Nil, session)
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	page, err := pendingAssignmentProposals(ctx, q, scope, actor, after, assignmentProposalPageLimit)
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	return page, authorizeAssignmentMiniApp(ctx, q, scope, actor, &session)
}

// RecentMiniAppAssignmentProposals recovers the latest twenty native operation
// results even after the browser journal is lost. It grants no new consent.
func (s *Store) RecentMiniAppAssignmentProposals(ctx context.Context, scope EventScope, actor string,
	session MiniAppAssignmentSession,
) (AssignmentProposalPage, error) {
	tx, q, err := s.beginAssignmentRead(ctx, scope, actor, uuid.Nil, session)
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.RecentNativeAssignmentProposals(ctx, sqlc.RecentNativeAssignmentProposalsParams{
		UserID: session.UserID, BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor,
	})
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return AssignmentProposalPage{}, err
	}
	page := AssignmentProposalPage{Proposals: make([]AssignmentProposalView, 0, len(rows))}
	for _, row := range rows {
		view, e := assignmentProposalView(ctx, q, row, now.Time)
		if e != nil {
			return AssignmentProposalPage{}, e
		}
		page.Proposals = append(page.Proposals, view)
	}
	return page, authorizeAssignmentMiniApp(ctx, q, scope, actor, &session)
}
