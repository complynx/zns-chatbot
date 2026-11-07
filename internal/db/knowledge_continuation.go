package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const (
	knowledgeActionCompleted = "completed"
	knowledgeActionPrepared  = "prepared"
	knowledgeActionExpired   = "expired"
	knowledgeActionUnknown   = "unknown"
)

type KnowledgeActionView struct {
	Kind        string                `json:"kind"`
	OperationID string                `json:"operation_id"`
	State       string                `json:"state"`
	Preparation *KnowledgePreparation `json:"preparation,omitempty"`
	Result      *KnowledgeResult      `json:"result,omitempty"`
	ObservedAt  time.Time             `json:"observed_at"`
}

type KnowledgeRecentAction struct {
	Kind             string     `json:"kind"`
	OperationID      string     `json:"operation_id"`
	ProposalID       uuid.UUID  `json:"proposal_id"`
	ArticleID        *uuid.UUID `json:"article_id,omitempty"`
	SuggestionID     *uuid.UUID `json:"suggestion_id,omitempty"`
	ExpectedRevision int64      `json:"expected_revision"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	State            string     `json:"state"`
	ObservedAt       time.Time  `json:"observed_at"`
}

// KnowledgeAction reads only the active caller's original action. The operation
// lock waits out concurrent preparation/confirmation before observing expiry.
// Unknown is nonterminal: a preparation can still arrive under this same key.
// Prepared is recoverable consent, not a promise that current roles/versions pass.
func (s *Store) KnowledgeAction(
	ctx context.Context,
	scope PaymentScope,
	kind, operation string,
) (KnowledgeActionView, error) {
	if !kbActionKind(kind) {
		return KnowledgeActionView{}, kbInvalid("unknown knowledge action")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgeActionView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = kbOperationLock(ctx, q, scope, kind, operation); err != nil {
		return KnowledgeActionView{}, err
	}
	if err = kbActor(ctx, q, scope); err != nil {
		return KnowledgeActionView{}, err
	}
	result := KnowledgeActionView{Kind: kind, OperationID: operation, State: knowledgeActionUnknown}
	receipt, err := q.ReadKnowledgeOperation(
		ctx,
		sqlc.ReadKnowledgeOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     kind,
			OperationID:    operation,
		},
	)
	if err == nil {
		result.Result, err = kbActionReceipt(scope, receipt)
		if err != nil {
			return KnowledgeActionView{}, err
		}
		result.State = knowledgeActionCompleted
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeActionView{}, err
	}
	prep, err := q.ReadKnowledgePreparation(
		ctx,
		sqlc.ReadKnowledgePreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     kind,
			OperationID:    operation,
		},
	)
	if err == nil {
		if prep.ActorID != scope.UserID {
			return KnowledgeActionView{}, ErrKnowledgeDenied
		}
		var document kbPreparationDocument
		if err = json.Unmarshal(prep.Document, &document); err != nil {
			return KnowledgeActionView{}, err
		}
		document.Preparation.ExpiresAt = prep.ExpiresAt.Time
		result.Preparation = &document.Preparation
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeActionView{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return KnowledgeActionView{}, err
	}
	result.ObservedAt = now.Time
	if result.Result == nil && result.Preparation != nil {
		result.State = knowledgeActionPrepared
		if !now.Time.Before(prep.ExpiresAt.Time) {
			result.State = knowledgeActionExpired
		}
	}
	return result, nil
}

func kbActionReceipt(scope PaymentScope, receipt sqlc.ShuttleKnowledgeOperation) (*KnowledgeResult, error) {
	if receipt.ActorID != scope.UserID {
		return nil, ErrKnowledgeDenied
	}
	var value KnowledgeResult
	if err := json.Unmarshal(receipt.Result, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func kbActionKind(kind string) bool {
	return kind == kbSuggest || kind == kbEdit || kind == kbReview || kind == kbSetModerator
}

// RecentKnowledgeActions supplies at most five actor-owned action locators,
// without article bodies. A nonzero cutoff excludes actions created by the
// current task; zero uses database time. Each state is observed under its own
// operation lock and may have a different observation time.
func (s *Store) RecentKnowledgeActions(
	ctx context.Context,
	scope PaymentScope,
	before time.Time,
) ([]KnowledgeRecentAction, error) {
	rows, err := s.kbRecentPreparations(ctx, scope, before)
	if err != nil {
		return nil, err
	}
	items := make([]KnowledgeRecentAction, 0, len(rows))
	for _, row := range rows {
		action, actionErr := s.KnowledgeAction(ctx, scope, row.ActionKind, row.OperationID)
		if actionErr != nil {
			return nil, actionErr
		}
		if action.Preparation == nil {
			continue
		}
		args := action.Preparation.Arguments
		item := KnowledgeRecentAction{
			Kind:             row.ActionKind,
			OperationID:      row.OperationID,
			ProposalID:       row.ProposalID,
			ArticleID:        kbUUIDPointer(kbUUID(args.ArticleID)),
			SuggestionID:     kbUUIDPointer(kbUUID(args.SuggestionID)),
			ExpectedRevision: args.ExpectedRevision,
			CreatedAt:        row.CreatedAt.Time,
			ExpiresAt:        action.Preparation.ExpiresAt,
			State:            action.State,
			ObservedAt:       action.ObservedAt,
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) kbRecentPreparations(
	ctx context.Context,
	scope PaymentScope,
	before time.Time,
) ([]sqlc.RecentKnowledgePreparationsRow, error) {
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if before.IsZero() {
		now, timeErr := q.RegistrationNow(ctx)
		if timeErr != nil {
			return nil, timeErr
		}
		before = now.Time
	}
	return q.RecentKnowledgePreparations(
		ctx,
		sqlc.RecentKnowledgePreparationsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActorID:        scope.UserID,
			BeforeAt:       pgtype.Timestamptz{Time: before, Valid: true},
		},
	)
}
