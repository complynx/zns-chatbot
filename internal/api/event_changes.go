package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

const (
	eventChangePath        = "/internal/agent/administration/change_event"
	eventChangeBodyLimit   = 128 << 10
	eventChangeResultLimit = 256 << 10
)

type eventChangePreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.EventChange          `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

func (s *Server) registerEventChanges(api huma.API) {
	registerAgentPost(
		api,
		"agent-prepare-event-change",
		eventChangePath+"/prepare",
		eventChangeBodyLimit,
		s.prepareEventChange,
	)
	registerAgentPost(api, "agent-confirm-event-change", eventChangePath, eventChangeBodyLimit, s.confirmEventChange)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-event-change-"+suffix,
			eventChangePath+"/"+suffix,
			internalBodyLimit,
			s.eventChangeStatus,
		)
	}
}

func eventChangeError(err error) error {
	switch {
	case errors.Is(err, db.ErrAdminDenied):
		return huma.Error403Forbidden("active global administrator required")
	case errors.Is(err, db.ErrAdminInvalid), errors.Is(err, events.ErrInvalid):
		return huma.Error400BadRequest("invalid event change")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("event or proposal not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("event or exact proposal changed or expired")
	default:
		return huma.Error503ServiceUnavailable("event change uncertain or unavailable; reconcile the same operation")
	}
}

func (s *Server) prepareEventChange(
	ctx context.Context, in *agentReadRequest[db.EventChange],
) (*eventChangePreparationResponse, error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PrepareEventChange(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, eventChangeError(err)
	}
	out := &eventChangePreparationResponse{}
	out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
	out.Body.Arguments, out.Body.Description, out.Body.ExpiresAt = prep.Arguments, prep.Description, prep.ExpiresAt
	out.Body.Preconditions.ProposalID = prep.ID
	return out, nil
}

func (s *Server) confirmEventChange(
	ctx context.Context, in *paymentWriteRequest[db.EventChange],
) (*paymentWriteResponse[events.Event], error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
		body.Confirmation.Mode != assignmentConfirmationMode || body.Confirmation.Preconditions == nil ||
		body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
	}
	result, err := s.store.ConfirmEventChange(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	out := &paymentWriteResponse[events.Event]{}
	out.Body.OperationID = body.OperationID
	if errors.Is(err, db.ErrConflict) || errors.Is(err, events.ErrInvalid) || errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "event_change_rejected",
			Message:        "Event or exact proposal changed, is missing or expired",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, eventChangeError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) eventChangeStatus(
	ctx context.Context, in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[events.Event], error) {
	scope, err := s.adminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.EventChangeReceipt(ctx, scope, in.Body.OperationID)
	out := &paymentWriteResponse[events.Event]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, eventChangeError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, result
	return out, nil
}
