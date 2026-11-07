package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/complynx/zns-chatbot/internal/db"
)

const operationInspectPath = "/internal/agent/operations/inspect"

type operationInspectionRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int    `json:"protocol_version" enum:"1"`
		OperationID     string `json:"operation_id" format:"uuid"`
		Module          string `json:"module"`
		Method          string `json:"method"`
	}
}

type operationInspectionResponse struct {
	Body struct {
		ProtocolVersion  int    `json:"protocol_version"`
		Status           string `json:"status"`
		OperationID      string `json:"operation_id"`
		ServiceID        string `json:"service_id"`
		Module           string `json:"module"`
		Method           string `json:"method"`
		ContractRevision string `json:"contract_revision"`
		Binding          struct {
			Issuer  string `json:"issuer"`
			Subject string `json:"subject"`
			Actor   string `json:"actor"`
			OrgID   string `json:"org_id"`
		} `json:"binding"`
		Arguments     json.RawMessage         `json:"arguments"`
		Description   string                  `json:"description"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		ExpiresAt     time.Time               `json:"expires_at"`
		Result        json.RawMessage         `json:"result,omitempty"`
	}
}

func (s *Server) knowledgeContract() db.KnowledgeContract {
	return db.KnowledgeContract{Issuer: s.options.Issuer, Actor: s.options.ActorID, Revision: agentContractRevision}
}

func (s *Server) inspectOperation(
	ctx context.Context,
	in *operationInspectionRequest,
) (*operationInspectionResponse, error) {
	principal, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	if principal.Actor != s.options.ActorID {
		return nil, huma.Error403Forbidden("delegated caller required")
	}
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.Module == "zinusya_orders" || body.Module == mealModuleID {
		return s.inspectOrderOperation(ctx, in, scope)
	}
	if body.Module != "zinusya_knowledge" {
		return nil, huma.NewError(http.StatusUnprocessableEntity, "operation adoption unsupported for this module")
	}
	if body.Method != knowledgeSuggestKind && body.Method != knowledgeEditKind && body.Method != paymentSchemaReview &&
		body.Method != "set_moderator" {
		return nil, huma.NewError(http.StatusUnprocessableEntity, "operation adoption unsupported for this method")
	}
	if err = knowledgeOperationID(body.OperationID); err != nil {
		return nil, err
	}
	original, err := s.store.InspectKnowledgeOperation(ctx, scope, body.Method, body.OperationID, s.knowledgeContract())
	if err != nil {
		return nil, knowledgeError(err)
	}
	// Reuse the same projection as original-operation reads, including exact arguments.
	action := db.KnowledgeActionView{
		Kind:        body.Method,
		OperationID: body.OperationID,
		State:       agentPrepared,
		Preparation: &original.Preparation,
	}
	view := knowledgeActionProject(action)
	out := &operationInspectionResponse{}
	out.Body.ProtocolVersion = 1
	out.Body.Status = agentPrepared
	out.Body.OperationID = body.OperationID
	out.Body.ServiceID = agentServiceID
	out.Body.Module = body.Module
	out.Body.Method = body.Method
	out.Body.ContractRevision = original.Contract.Revision
	out.Body.Binding.Issuer = original.Contract.Issuer
	out.Body.Binding.Actor = original.Contract.Actor
	out.Body.Binding.Subject = original.Subject
	out.Body.Binding.OrgID = original.Organization
	out.Body.Arguments, err = json.Marshal(view.Preparation.Arguments)
	if err != nil {
		return nil, err
	}
	out.Body.Description = original.Preparation.Description
	out.Body.Preconditions.ProposalID = original.Preparation.ID
	out.Body.ExpiresAt = original.Preparation.ExpiresAt
	if original.Result != nil {
		result := knowledgeResultProject(*original.Result)
		out.Body.Result, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
		out.Body.Status = agentSucceeded
	}
	return out, nil
}
