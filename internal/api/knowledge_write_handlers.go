package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

func knowledgePrepareHandler[T any](
	s *Server,
	kind string,
	toChange func(T) db.KnowledgeChange,
	fromChange func(db.KnowledgeChange) T,
) func(context.Context, *agentReadRequest[T]) (*knowledgePreparationResponse[T], error) {
	return func(ctx context.Context, in *agentReadRequest[T]) (*knowledgePreparationResponse[T], error) {
		scope, err := s.paymentScope(ctx, in.AuthInput)
		if err != nil {
			return nil, err
		}
		if err = knowledgeOperationID(in.Body.OperationID); err != nil {
			return nil, err
		}
		change := toChange(in.Body.Arguments)
		change.Kind = kind
		if (kind == "suggest" || kind == "edit") && change.ArticleID == uuid.Nil && change.ExpectedRevision == 0 {
			// Repeated preparation of a new article must retain one target ID.
			change.ArticleID = uuid.NewSHA1(scope.UserID, []byte("knowledge:"+kind+":"+in.Body.OperationID))
		}
		prep, err := s.store.PrepareKnowledgeForContract(ctx, scope, in.Body.OperationID, change, s.knowledgeContract())
		if err != nil {
			return nil, knowledgeError(err)
		}
		out := &knowledgePreparationResponse[T]{}
		out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
		out.Body.Arguments = fromChange(prep.Arguments)
		out.Body.Preconditions.ProposalID = prep.ID
		out.Body.Description, out.Body.ExpiresAt = prep.Description, prep.ExpiresAt
		return out, nil
	}
}

func knowledgeConfirmHandler[T any](
	s *Server,
	kind string,
	toChange func(T) db.KnowledgeChange,
) func(context.Context, *paymentWriteRequest[T]) (*paymentWriteResponse[knowledgeResultView], error) {
	return func(ctx context.Context, in *paymentWriteRequest[T]) (*paymentWriteResponse[knowledgeResultView], error) {
		scope, err := s.paymentScope(ctx, in.AuthInput)
		if err != nil {
			return nil, err
		}
		body := in.Body
		if err = knowledgeOperationID(body.OperationID); err != nil {
			return nil, err
		}
		if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
			body.Confirmation.Mode != assignmentConfirmationMode ||
			body.Confirmation.Preconditions == nil ||
			body.Confirmation.Preconditions.ProposalID == uuid.Nil {
			return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
		}
		change := toChange(body.Arguments)
		change.Kind = kind
		result, err := s.store.ConfirmKnowledge(
			ctx,
			scope,
			body.OperationID,
			change,
			body.Confirmation.Preconditions.ProposalID,
		)
		return knowledgeWriteResult(body.OperationID, result, err)
	}
}

func knowledgeStatusHandler(
	s *Server,
	kind string,
) func(context.Context, *agentRegistrationStatusRequest) (*paymentWriteResponse[knowledgeResultView], error) {
	return func(ctx context.Context, in *agentRegistrationStatusRequest) (*paymentWriteResponse[knowledgeResultView], error) {
		scope, err := s.paymentScope(ctx, in.AuthInput)
		if err != nil {
			return nil, err
		}
		if err = knowledgeOperationID(in.Body.OperationID); err != nil {
			return nil, err
		}
		result, err := s.store.KnowledgeOperation(ctx, scope, kind, in.Body.OperationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return paymentStatusResult[knowledgeResultView](in.Body.OperationID, nil, err)
		}
		if err != nil {
			return nil, knowledgeError(err)
		}
		view := knowledgeResultProject(result)
		return paymentStatusResult(in.Body.OperationID, &view, nil)
	}
}
