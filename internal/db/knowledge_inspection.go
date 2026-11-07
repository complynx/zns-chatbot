package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// KnowledgeContract is supplied by the authenticated API, never request JSON.
// Subject and organization are stored in the preparation row's original key.
type KnowledgeContract struct {
	Issuer   string `json:"issuer"`
	Actor    string `json:"actor"`
	Revision string `json:"revision"`
}

type KnowledgeInspection struct {
	Contract              KnowledgeContract
	Subject, Organization string
	Preparation           KnowledgePreparation
	Result                *KnowledgeResult
}

// InspectKnowledgeOperation reads original terms without preparing or extending
// anything. It checks current authority even when returning a completed receipt.
func (s *Store) InspectKnowledgeOperation(ctx context.Context, scope PaymentScope, kind, op string,
	contract KnowledgeContract,
) (KnowledgeInspection, error) {
	var result KnowledgeInspection
	if !kbActionKind(kind) || contract.Issuer == "" || contract.Actor == "" || contract.Revision == "" {
		return result, ErrKnowledgeDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = kbOperationLock(ctx, q, scope, kind, op); err != nil {
		return result, err
	}
	row, err := q.ReadKnowledgePreparation(
		ctx,
		sqlc.ReadKnowledgePreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     kind,
			OperationID:    op,
		},
	)
	if err != nil {
		return result, err
	}
	if row.ActorID != scope.UserID {
		return result, ErrKnowledgeDenied
	}
	var document kbPreparationDocument
	if err = json.Unmarshal(row.Document, &document); err != nil {
		return result, err
	}
	if document.Contract != contract {
		return result, ErrConflict
	}
	if document.Preparation.Arguments.Kind != kind || document.Preparation.ID != row.ProposalID {
		return result, ErrConflict
	}
	if err = authorizeKnowledgeInspection(ctx, q, scope, document.Preparation.Arguments); err != nil {
		return result, err
	}
	document.Preparation.ExpiresAt = row.ExpiresAt.Time
	result = KnowledgeInspection{
		Contract:     document.Contract,
		Subject:      row.Actor,
		Organization: row.OrganizationID,
		Preparation:  document.Preparation,
	}
	receipt, err := q.ReadKnowledgeOperation(
		ctx,
		sqlc.ReadKnowledgeOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     kind,
			OperationID:    op,
		},
	)
	if err == nil {
		result.Result, err = kbActionReceipt(scope, receipt)
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return result, err
}

// Current authority is independent of the original preconditions. A completed
// review or edit no longer has its old target revision/pending state. Execution
// still validates those preconditions on the canonical original operation ID.
func authorizeKnowledgeInspection(ctx context.Context, q *sqlc.Queries, scope PaymentScope, c KnowledgeChange) error {
	if c.Kind == kbSetModerator {
		if err := kbActor(ctx, q, scope); err != nil {
			return err
		}
		if !scope.GlobalAdmin {
			return ErrKnowledgeDenied
		}
		return nil
	}
	articleID, eventID, err := kbResolveOrigin(ctx, q, scope, c)
	if err != nil {
		return err
	}
	event, err := kbLockEvent(ctx, q, scope, eventID)
	if err != nil {
		return err
	}
	if err = kbActor(ctx, q, scope); err != nil {
		return err
	}
	if c.Kind == kbSuggest {
		return nil
	}
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("knowledge-article:%d:%s:%s", scope.BotID, scope.Organization, articleID),
	); err != nil {
		return err
	}
	article, err := kbCurrentArticle(ctx, q, scope, articleID)
	if err != nil {
		return err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	return kbAuthorize(ctx, q, scope, event, article, now.Time)
}
