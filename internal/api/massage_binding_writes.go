package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

const massageBindingWritePath = "/internal/agent/administration/set_massage_specialist_binding"

type massageBindingPreparationArguments struct {
	db.MassageBindingChange

	ExistingProposalID massageProposalID `json:"existing_proposal_id,omitzero"`
}

type massageBindingPendingArguments struct {
	EventID uuid.UUID `json:"event_id,omitempty"`
	After   uuid.UUID `json:"after,omitempty"`
}

type massageBindingStatusArguments struct {
	OperationID uuid.UUID `json:"operation_id,omitempty"`
	ProposalID  uuid.UUID `json:"proposal_id,omitempty"`
}

func (s *Server) registerMassageBindingWrites(api huma.API) {
	registerAgentPost(
		api,
		"agent-prepare-massage-binding",
		massageBindingWritePath+"/prepare",
		internalBodyLimit,
		s.prepareMassageBinding,
	)
	registerAgentPost(
		api,
		"agent-change-massage-binding",
		massageBindingWritePath,
		internalBodyLimit,
		s.changeMassageBinding,
	)
	registerAgentPost(
		api,
		"agent-massage-binding-status",
		massageBindingWritePath+"/status",
		internalBodyLimit,
		s.massageBindingEffectStatus,
	)
	registerAgentPost(
		api,
		"agent-pending-massage-bindings",
		"/internal/agent/administration/massage_specialist_pending",
		internalBodyLimit,
		s.pendingMassageBindings,
	)
	registerAgentPost(
		api,
		"agent-massage-binding-proposal",
		"/internal/agent/administration/massage_specialist_proposal",
		internalBodyLimit,
		s.massageBindingProposal,
	)
}

func (s *Server) prepareMassageBinding(
	ctx context.Context,
	in *agentReadRequest[massageBindingPreparationArguments],
) (*massagePreparationResponse[db.MassageBindingChange], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	a := in.Body.Arguments
	var prepared db.MassageBindingPreparation
	if a.ExistingProposalID.UUID != uuid.Nil {
		prepared, err = s.store.AdoptMassageBinding(
			ctx,
			scope,
			operation,
			a.ExistingProposalID.UUID,
			a.MassageBindingChange,
		)
	} else {
		prepared, err = s.store.PrepareMassageBinding(ctx, scope, operation, a.MassageBindingChange)
	}
	if err != nil {
		return nil, massageBookingError(err)
	}
	status, err := s.store.MassageBindingStatus(ctx, scope, nil, operation, uuid.Nil)
	if err != nil {
		return nil, massageBookingError(err)
	}
	if status.State != agentPending || !status.ExpiresAt.After(time.Now()) {
		return nil, huma.Error409Conflict(
			"proposal completed or expired; read massage_specialist_proposal to reconcile the original result",
		)
	}
	description, err := s.massageBindingDescription(ctx, scope, prepared.Arguments)
	if err != nil {
		return nil, massageBookingError(err)
	}
	out := &massagePreparationResponse[db.MassageBindingChange]{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = description
	return out, nil
}

func (s *Server) massageBindingDescription(
	ctx context.Context,
	scope db.ProfileScope,
	a db.MassageBindingChange,
) (string, error) {
	event, err := s.store.Event(ctx, s.eventScope(), a.EventID)
	if err != nil {
		return "", err
	}
	target, err := s.store.MassageBindingTargetLookup(ctx, scope, nil, db.MassageBindingTargetQuery{UserID: a.UserID})
	if err != nil {
		return "", err
	}
	language, err := s.orderPresentationLanguage(ctx, scope)
	if err != nil {
		return "", err
	}
	action := "Revoke"
	if a.Active {
		action = "Grant"
	}
	if language == "ru" {
		action = "Отозвать"
		if a.Active {
			action = "Назначить"
		}
		return fmt.Sprintf(
			"%s доступ специалиста: эвент %q, специалист %q, пользователь %q (Telegra"+
				"m %d). Изменяется только доступ к расписанию и собственным клиентам; зап"+
				"иси и платежи не меняются.",
			action,
			event.Document.Name,
			a.SpecialistID,
			target.DisplayName,
			target.TelegramUserID,
		), nil
	}
	return fmt.Sprintf(
		"%s specialist access: event %q, specialist %q, account %q (Telegram %d)."+
			" Changes only access to the timetable and own clients; bookings and paym"+
			"ents stay unchanged.",
		action,
		event.Document.Name,
		a.SpecialistID,
		target.DisplayName,
		target.TelegramUserID,
	), nil
}

func (s *Server) changeMassageBinding(
	ctx context.Context,
	in *massageWriteRequest[db.MassageBindingChange],
) (*paymentWriteResponse[db.MassageSpecialistBinding], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if err = requireExactOperation(
		b.OperationID,
		b.IdempotencyKey,
		b.Confirmation.Preconditions.ProposalID,
	); err != nil {
		return nil, err
	}
	result, err := s.store.ChangeMassageBinding(
		ctx,
		scope,
		b.OperationID,
		b.Confirmation.Preconditions.ProposalID,
		b.Arguments,
		func(checkCtx context.Context) error {
			_, checkErr := s.authorizeIdentity(checkCtx, in.Authorization, true)
			return checkErr
		},
	)
	return massageBindingResult(b.OperationID.String(), result, err)
}

func massageBindingResult(
	operation string,
	result db.MassageSpecialistBinding,
	err error,
) (*paymentWriteResponse[db.MassageSpecialistBinding], error) {
	out := &paymentWriteResponse[db.MassageSpecialistBinding]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := massageBookingError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "massage_binding_rejected",
			Message:        "specialist identity, versions, approval or current administrator rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) massageBindingEffectStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.MassageSpecialistBinding], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	result, err := s.store.MassageBindingOperation(ctx, scope, operation)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.MassageSpecialistBinding]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return massageBindingResult(in.Body.OperationID, result, err)
}

func (s *Server) pendingMassageBindings(
	ctx context.Context,
	in *agentReadRequest[massageBindingPendingArguments],
) (*agentReadResponse[db.MassageBindingPendingPage], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.PendingMassageBindings(ctx, scope, nil, a.EventID, a.After)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageBindingProposal(
	ctx context.Context,
	in *agentReadRequest[massageBindingStatusArguments],
) (*agentReadResponse[db.MassageBindingProposalStatus], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.MassageBindingStatus(ctx, scope, nil, a.OperationID, a.ProposalID)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
