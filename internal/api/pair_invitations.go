package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const pairInvitationPath = "/internal/agent/pairs/"

type pairInvitationListArguments struct {
	Direction string    `json:"direction,omitempty" enum:"incoming,outgoing"`
	State     string    `json:"state,omitempty"     enum:"pending,accepted,declined,revoked,expired,all"`
	EventID   uuid.UUID `json:"event_id,omitempty"`
	After     uuid.UUID `json:"after,omitempty"`
	Limit     int32     `json:"limit,omitempty"                                                          minimum:"1" maximum:"20"`
}

type pairInvitationListResult struct {
	Invitations []db.PairInvitationView `json:"invitations"`
	Next        *uuid.UUID              `json:"next,omitempty"`
}

func (s *Server) registerPairInvitations(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-pair-invitations",
			Method:       http.MethodPost,
			Path:         pairInvitationPath + "invitations",
			MaxBodyBytes: internalBodyLimit,
		},
		s.pairInvitations,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-pair-invitation-context",
			Method:       http.MethodPost,
			Path:         pairInvitationPath + "invitation_context",
			MaxBodyBytes: internalBodyLimit,
		},
		s.pairInvitationContext,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-pair-invite",
			Method:       http.MethodPost,
			Path:         pairInvitationPath + methodInvite,
			MaxBodyBytes: internalBodyLimit,
		},
		s.invitePair,
	)
	for _, kind := range []string{methodAccept, methodDecline, methodRevoke} {
		huma.Register(
			api,
			huma.Operation{
				OperationID:  "agent-pair-" + kind,
				Method:       http.MethodPost,
				Path:         pairInvitationPath + kind,
				MaxBodyBytes: internalBodyLimit,
			},
			func(ctx context.Context, in *paymentWriteRequest[db.PairInvitationDecision]) (*paymentWriteResponse[db.PairResult], error) {
				return s.decidePairInvitation(ctx, in, kind)
			},
		)
	}
	for _, kind := range []string{methodInvite, methodAccept, methodDecline, methodRevoke} {
		for _, suffix := range []string{routeStatus, routeReconcile} {
			huma.Register(
				api,
				huma.Operation{
					OperationID:  "agent-pair-" + kind + "-" + suffix,
					Method:       http.MethodPost,
					Path:         pairInvitationPath + kind + "/" + suffix,
					MaxBodyBytes: internalBodyLimit,
				},
				func(ctx context.Context, in *agentRegistrationStatusRequest) (*paymentWriteResponse[db.PairResult], error) {
					return s.pairInvitationStatus(ctx, in, kind)
				},
			)
		}
	}
}

func invitationError(err error) error {
	if errors.Is(err, db.ErrPairDenied) {
		return huma.Error403Forbidden("invitation action not permitted")
	}
	return registrationError(err)
}

func (s *Server) pairInvitations(
	ctx context.Context,
	in *agentReadRequest[pairInvitationListArguments],
) (*agentReadResponse[pairInvitationListResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	args = normalizedPairInvitationList(args)
	rows, err := s.store.PairInvitations(
		ctx,
		scope,
		db.PairInvitationList{
			Direction: args.Direction,
			State:     args.State,
			EventID:   args.EventID,
			After:     args.After,
			Limit:     args.Limit,
		},
	)
	if err != nil {
		return nil, invitationError(err)
	}
	result := pairInvitationListResult{Invitations: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].Invitation.ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func normalizedPairInvitationList(args pairInvitationListArguments) pairInvitationListArguments {
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	if args.Direction == "" {
		args.Direction = "incoming"
	}
	switch args.State {
	case "":
		args.State = invitationPending
	case "all":
		args.State = ""
	}
	return args
}

func (s *Server) pairInvitationContext(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.PairInvitationContext], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PairInvitationContext(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, invitationError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func validParticipantWrite[T any](body paymentWriteBody[T]) bool {
	return body.OperationID == body.IdempotencyKey &&
		(body.Confirmation == nil || body.Confirmation.Mode == confirmationNone)
}

func (s *Server) invitePair(
	ctx context.Context,
	in *paymentWriteRequest[db.PairInvitationRequest],
) (*paymentWriteResponse[db.PairResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if !validParticipantWrite(in.Body) {
		return nil, huma.Error400BadRequest("matching operation key and participant action required")
	}
	result, err := s.store.CreatePairInvitation(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	return invitationWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) decidePairInvitation(
	ctx context.Context,
	in *paymentWriteRequest[db.PairInvitationDecision],
	kind string,
) (*paymentWriteResponse[db.PairResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if !validParticipantWrite(in.Body) {
		return nil, huma.Error400BadRequest("matching operation key and participant action required")
	}
	result, err := s.store.DecidePairInvitation(ctx, scope, in.Body.OperationID, kind, in.Body.Arguments)
	return invitationWriteResult(in.Body.OperationID, result, err)
}

func invitationWriteResult(
	operation string,
	result db.PairResult,
	err error,
) (*paymentWriteResponse[db.PairResult], error) {
	out := &paymentWriteResponse[db.PairResult]{}
	out.Body.OperationID = operation
	if err != nil {
		if errors.Is(err, registrationdetails.ErrRequired) || errors.Is(err, db.ErrConflict) ||
			errors.Is(err, pgx.ErrNoRows) ||
			errors.Is(err, registrations.ErrInvalid) ||
			errors.Is(err, registrations.ErrUnavailable) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "pair_invitation_rejected",
				Message:        "Invitation unavailable or changed; read your current invitations before retrying.",
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, invitationError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) pairInvitationStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
	kind string,
) (*paymentWriteResponse[db.PairResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PairInvitationOperation(ctx, scope, in.Body.OperationID, kind)
	out := &paymentWriteResponse[db.PairResult]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, invitationError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
