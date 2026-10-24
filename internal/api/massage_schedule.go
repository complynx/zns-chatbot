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
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massageScheduleWritePath = "/internal/agent/administration/set_massage_schedule"
const massageScheduleBodyLimit = 64 << 10

type massageScheduleRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                      `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID                `json:"operation_id"`
		RunID           string                   `json:"run_id,omitempty"`
		Arguments       db.MassageScheduleChange `json:"arguments"`
		IdempotencyKey  uuid.UUID                `json:"idempotency_key"`
		Confirmation    authorityConfirmation    `json:"confirmation"`
	}
}

type massageSchedulePreparationResponse struct {
	Body struct {
		Status        string                   `json:"status"`
		OperationID   string                   `json:"operation_id"`
		Arguments     db.MassageScheduleChange `json:"arguments"`
		Preconditions assignmentPreconditions  `json:"preconditions"`
		Description   string                   `json:"description"`
		ExpiresAt     time.Time                `json:"expires_at"`
	}
}

func (s *Server) registerMassageSchedule(api huma.API) {
	registerAgentPost(api, "agent-administrative-massage-bookings", "/internal/agent/administration/massage_bookings",
		internalBodyLimit, s.administrativeMassageBookings)
	s.registerMassageBooking(api)
	s.registerMassageStaff(api)
	s.registerMassageBindings(api)
	registerAgentPost(api, "agent-own-massage-candidates", "/internal/agent/massage/candidate_starts",
		massageScheduleBodyLimit, s.ownMassageCandidates)
	registerAgentPost(api, "agent-own-massage-schedule", "/internal/agent/massage/schedule",
		internalBodyLimit, s.ownMassageSchedule)
	registerAgentPost(api, "agent-admin-massage-schedule", "/internal/agent/administration/massage_schedule",
		internalBodyLimit, s.adminMassageSchedule)
	registerAgentPost(api, "agent-prepare-massage-schedule", massageScheduleWritePath+"/prepare",
		massageScheduleBodyLimit, s.prepareMassageSchedule)
	registerAgentPost(api, "agent-set-massage-schedule", massageScheduleWritePath,
		massageScheduleBodyLimit, s.setMassageSchedule)
	registerAgentPost(api, "agent-massage-schedule-status", massageScheduleWritePath+"/status",
		internalBodyLimit, s.massageScheduleStatus)
}

func (s *Server) ownMassageCandidates(
	ctx context.Context,
	in *agentReadRequest[db.MassageCandidateArguments],
) (*agentReadResponse[db.MassageCandidateView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMassageCandidates(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, massageScheduleError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageAdminScope(ctx context.Context, auth AuthInput) (db.ProfileScope, error) {
	p, err := s.authorize(ctx, auth.Authorization, true)
	if err != nil {
		return db.ProfileScope{}, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return db.ProfileScope{}, err
	}
	return db.ProfileScope{
		BotID:        s.options.BotID,
		Organization: s.options.OrganizationID,
		Subject:      p.Subject,
		UserID:       u.ID,
	}, nil
}

func (s *Server) ownMassageSchedule(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.MassageScheduleView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMassageSchedule(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, massageScheduleError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) adminMassageSchedule(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.MassageSchedule], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MassageScheduleConfiguration(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, massageScheduleError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) prepareMassageSchedule(
	ctx context.Context,
	in *agentReadRequest[db.MassageScheduleChange],
) (*massageSchedulePreparationResponse, error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	prepared, err := s.store.PrepareMassageSchedule(ctx, scope, op, in.Body.Arguments)
	if err != nil {
		return nil, massageScheduleError(err)
	}
	out := &massageSchedulePreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = massageScheduleDescription(prepared.Arguments)
	return out, nil
}

func massageScheduleDescription(args db.MassageScheduleChange) string {
	return fmt.Sprintf(
		"Replace massage schedule for event %s at event version %d, schedule vers"+
			"ion %d. Currency %s, %d services, %d work windows. Schedule only; no res"+
			"ervations or money change.",
		args.EventID,
		args.ExpectedEventVersion,
		args.ExpectedVersion,
		args.Document.Currency,
		len(args.Document.Services),
		len(args.Document.Windows),
	)
}

func (s *Server) setMassageSchedule(
	ctx context.Context,
	in *massageScheduleRequest,
) (*paymentWriteResponse[db.MassageSchedule], error) {
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
	result, err := s.store.ChangeMassageSchedule(
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
	return massageScheduleResult(b.OperationID.String(), result, err)
}

func (s *Server) massageScheduleStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.MassageSchedule], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	result, err := s.store.MassageScheduleOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.MassageSchedule]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return massageScheduleResult(in.Body.OperationID, result, err)
}

func massageScheduleError(err error) error {
	switch {
	case errors.Is(err, massage.ErrInvalid):
		return huma.Error400BadRequest("invalid massage schedule")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("massage schedule or entitlement not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("schedule, event version or approval changed")
	default:
		if _, ok := errors.AsType[huma.StatusError](err); ok {
			return err
		}
		return huma.Error503ServiceUnavailable("massage schedule storage unavailable")
	}
}

func massageScheduleResult(
	operation string,
	result db.MassageSchedule,
	err error,
) (*paymentWriteResponse[db.MassageSchedule], error) {
	out := &paymentWriteResponse[db.MassageSchedule]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := massageScheduleError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "massage_schedule_rejected",
			Message:        "schedule, versions, approval or account rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
