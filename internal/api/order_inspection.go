package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) orderContract() db.OrderPreparationContract {
	return db.OrderPreparationContract{
		Issuer:   s.options.Issuer,
		Actor:    s.options.ActorID,
		Revision: agentContractRevision,
	}
}

func (s *Server) prepareOwnOrderWithContract(ctx context.Context, scope db.ProfileScope,
	operation uuid.UUID, args db.OrderSubmission, source db.OrderSource,
) (db.OrderPreparation, error) {
	language, err := s.orderPresentationLanguage(ctx, scope)
	if err != nil {
		return db.OrderPreparation{}, err
	}
	return s.store.PrepareOwnOrderSubmissionWithContract(
		ctx,
		scope,
		operation,
		args,
		source,
		s.orderContract(),
		func(p db.OrderPreparation) (string, error) {
			return orderSubmissionDescription(p, language)
		},
	)
}

func (s *Server) prepareMiniAppOrderWithContract(ctx context.Context, deployment db.EventScope,
	session db.MiniAppOrderSession, operation uuid.UUID, args db.OrderSubmission,
) (db.OrderPreparation, error) {
	// Resolve presentation only; the write transaction revalidates the session and
	// derives its original identity binding from that transaction's current scope.
	attempt, err := s.store.MiniAppSession(ctx, deployment.BotID, session.Bearer, session.Generation)
	if err != nil {
		return db.OrderPreparation{}, err
	}
	language, err := s.orderPresentationLanguage(ctx, miniAppOrdersScope(attempt))
	if err != nil {
		return db.OrderPreparation{}, err
	}
	return s.store.PrepareMiniAppOrderSubmissionWithContract(
		ctx,
		deployment,
		session,
		operation,
		args,
		s.orderContract(),
		func(p db.OrderPreparation) (string, error) {
			return orderSubmissionDescription(p, language)
		},
	)
}

func (s *Server) inspectOrderOperation(ctx context.Context, in *operationInspectionRequest,
	scope db.PaymentScope,
) (*operationInspectionResponse, error) {
	if in.Body.Method != string(db.OrderSubmit) {
		return nil, huma.NewError(http.StatusUnprocessableEntity, "operation adoption unsupported for this method")
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	owner := db.ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		Subject:      scope.Subject,
		UserID:       scope.UserID,
	}
	original, err := s.store.InspectOrderSubmissionOperation(ctx, owner, operation, s.orderContract())
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	out := &operationInspectionResponse{}
	out.Body.ProtocolVersion = 1
	out.Body.Status = agentPrepared
	out.Body.OperationID = in.Body.OperationID
	out.Body.ServiceID = agentServiceID
	out.Body.Module = in.Body.Module
	out.Body.Method = in.Body.Method
	out.Body.ContractRevision = original.Contract.Revision
	out.Body.Binding.Issuer = original.Contract.Issuer
	out.Body.Binding.Actor = original.Contract.Actor
	out.Body.Binding.Subject = original.Subject
	out.Body.Binding.OrgID = original.Organization
	out.Body.Arguments, err = json.Marshal(original.Preparation.Arguments)
	if err != nil {
		return nil, err
	}
	out.Body.Description = original.Preparation.Description
	out.Body.Preconditions.ProposalID = original.Preparation.ID
	out.Body.ExpiresAt = original.Preparation.ExpiresAt
	if original.Result != nil {
		out.Body.Status = agentSucceeded
		out.Body.Result, err = json.Marshal(original.Result)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
