package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massageBookingPath = "/internal/agent/massage/"

type massageWriteRequest[A any] struct {
	AuthInput

	Body struct {
		ProtocolVersion int                   `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID             `json:"operation_id"`
		RunID           string                `json:"run_id,omitempty"`
		Arguments       A                     `json:"arguments"`
		IdempotencyKey  uuid.UUID             `json:"idempotency_key"`
		Confirmation    authorityConfirmation `json:"confirmation"`
	}
}

type massagePreparationResponse[A any] struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     A                       `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

// An omitted selector starts a preparation. A supplied selector must identify an existing proposal.
type massageProposalID struct {
	uuid.UUID
}

func (p *massageProposalID) UnmarshalJSON(raw []byte) error {
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return err
	}
	if id == uuid.Nil {
		return massage.ErrInvalid
	}
	p.UUID = id
	return nil
}

func (massageProposalID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid"}
}

type massageBookingPreparationArguments struct {
	db.MassageBookingArguments

	ExistingProposalID massageProposalID `json:"existing_proposal_id,omitzero"`
}

type massageCancellationPreparationArguments struct {
	db.MassageCancellationArguments

	ExistingProposalID massageProposalID `json:"existing_proposal_id,omitzero"`
}

type massageBookingDetailArguments struct {
	EventID   uuid.UUID `json:"event_id"`
	BookingID uuid.UUID `json:"booking_id"`
}

type massageBookingListArguments struct {
	EventID uuid.UUID `json:"event_id"`
	After   uuid.UUID `json:"after,omitempty"`
	Limit   int       `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type massageOperationArguments struct {
	OperationID uuid.UUID `json:"operation_id"`
}

func (s *Server) registerMassageBooking(api huma.API) {
	registerAgentPost(api, "agent-pending-massage-proposals", massageBookingPath+agentPending,
		internalBodyLimit, s.ownPendingMassageProposals)

	registerAgentPost(
		api,
		"agent-massage-availability",
		massageBookingPath+"availability",
		internalBodyLimit,
		s.ownMassageAvailability,
	)
	registerAgentPost(
		api,
		"agent-prepare-massage-booking",
		massageBookingPath+"book/prepare",
		internalBodyLimit,
		s.prepareMassageBooking,
	)
	registerAgentPost(api, "agent-book-massage", massageBookingPath+"book", internalBodyLimit, s.bookMassage)
	registerAgentPost(
		api,
		"agent-massage-booking-status",
		massageBookingPath+"book/status",
		internalBodyLimit,
		s.massageBookingStatus,
	)
	registerAgentPost(
		api,
		"agent-prepare-massage-cancellation",
		massageBookingPath+"cancel/prepare",
		internalBodyLimit,
		s.prepareMassageCancellation,
	)
	registerAgentPost(api, "agent-cancel-massage", massageBookingPath+"cancel", internalBodyLimit, s.cancelMassage)
	registerAgentPost(
		api,
		"agent-massage-cancellation-status",
		massageBookingPath+"cancel/status",
		internalBodyLimit,
		s.massageCancellationStatus,
	)
	registerAgentPost(
		api,
		"agent-own-massage-booking",
		massageBookingPath+"get",
		internalBodyLimit,
		s.ownMassageBooking,
	)
	registerAgentPost(
		api,
		"agent-own-massage-bookings",
		massageBookingPath+"list",
		internalBodyLimit,
		s.ownMassageBookings,
	)
	registerAgentPost(
		api,
		"agent-massage-operation",
		massageBookingPath+"operation",
		internalBodyLimit,
		s.massageOperation,
	)
}

func (s *Server) prepareMassageBooking(
	ctx context.Context,
	in *agentReadRequest[massageBookingPreparationArguments],
) (*massagePreparationResponse[db.MassageBookingArguments], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	var prepared db.MassageBookingPreparation
	if in.Body.Arguments.ExistingProposalID.UUID != uuid.Nil {
		prepared, err = s.store.AdoptOwnMassageBooking(
			ctx,
			scope,
			operation,
			in.Body.Arguments.ExistingProposalID.UUID,
			in.Body.Arguments.MassageBookingArguments,
		)
	} else {
		prepared, err = s.store.PrepareOwnMassageBooking(
			ctx,
			scope,
			operation,
			in.Body.Arguments.MassageBookingArguments,
		)
	}
	if err != nil {
		return nil, massageBookingError(err)
	}
	language, err := s.orderPresentationLanguage(ctx, scope)
	if err != nil {
		return nil, massageBookingError(err)
	}
	description, err := massageBookingDescription(prepared.Snapshot, language, false)
	if err != nil {
		return nil, massageBookingError(err)
	}
	out := &massagePreparationResponse[db.MassageBookingArguments]{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = description
	return out, nil
}

func (s *Server) prepareMassageCancellation(
	ctx context.Context,
	in *agentReadRequest[massageCancellationPreparationArguments],
) (*massagePreparationResponse[db.MassageCancellationArguments], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	var prepared db.MassageCancellationPreparation
	if in.Body.Arguments.ExistingProposalID.UUID != uuid.Nil {
		prepared, err = s.store.AdoptOwnMassageCancellation(
			ctx,
			scope,
			operation,
			in.Body.Arguments.ExistingProposalID.UUID,
			in.Body.Arguments.MassageCancellationArguments,
		)
	} else {
		prepared, err = s.store.PrepareOwnMassageCancellation(
			ctx,
			scope,
			operation,
			in.Body.Arguments.MassageCancellationArguments,
		)
	}
	if err != nil {
		return nil, massageBookingError(err)
	}
	language, err := s.orderPresentationLanguage(ctx, scope)
	if err != nil {
		return nil, massageBookingError(err)
	}
	description, err := massageBookingDescription(prepared.Booking.Snapshot, language, true)
	if err != nil {
		return nil, massageBookingError(err)
	}
	out := &massagePreparationResponse[db.MassageCancellationArguments]{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = description
	return out, nil
}

func (s *Server) bookMassage(
	ctx context.Context,
	in *massageWriteRequest[db.MassageBookingArguments],
) (*paymentWriteResponse[db.MassageBooking], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
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
	result, err := s.store.BookOwnMassage(
		ctx,
		scope,
		b.OperationID,
		b.Confirmation.Preconditions.ProposalID,
		b.Arguments,
		db.MassageSourceAgent,
	)
	return massageBookingResult(b.OperationID.String(), result, err)
}

func (s *Server) cancelMassage(
	ctx context.Context,
	in *massageWriteRequest[db.MassageCancellationArguments],
) (*paymentWriteResponse[db.MassageBooking], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
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
	result, err := s.store.CancelOwnMassage(
		ctx,
		scope,
		b.OperationID,
		b.Confirmation.Preconditions.ProposalID,
		b.Arguments,
	)
	return massageBookingResult(b.OperationID.String(), result, err)
}

func (s *Server) ownMassageBooking(
	ctx context.Context,
	in *agentReadRequest[massageBookingDetailArguments],
) (*agentReadResponse[db.MassageBooking], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMassageBooking(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.BookingID)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) ownMassageBookings(
	ctx context.Context,
	in *agentReadRequest[massageBookingListArguments],
) (*agentReadResponse[db.MassageBookingPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.OwnMassageBookings(ctx, scope, a.EventID, a.After, a.Limit)
	if err != nil {
		return nil, massageBookingError(err)
	}
	result, err = boundedMassagePage(result)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageBookingStatus(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[db.MassageBooking], error) {
	return s.massageEffectStatus(ctx, in, "book")
}

func (s *Server) massageCancellationStatus(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[db.MassageBooking], error) {
	return s.massageEffectStatus(ctx, in, "cancel")
}

func (s *Server) massageEffectStatus(
	ctx context.Context,
	in *profileOperationRequest,
	kind string,
) (*paymentWriteResponse[db.MassageBooking], error) {
	scope, operation, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.OwnMassageOperationReceipt(ctx, scope, operation)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && receipt.Kind != kind) {
		out := &paymentWriteResponse[db.MassageBooking]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return massageBookingResult(in.Body.OperationID, receipt.Result, err)
}

func (s *Server) massageOperation(
	ctx context.Context,
	in *agentReadRequest[massageOperationArguments],
) (*paymentWriteResponse[db.MassageOperationReceipt], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.OwnMassageOperationReceipt(ctx, scope, in.Body.Arguments.OperationID)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.MassageOperationReceipt]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return massageBookingResult(in.Body.OperationID, receipt, err)
}

func massageBookingError(err error) error {
	switch {
	case errors.Is(err, massage.ErrUnavailable):
		return huma.Error409Conflict("massage time, specialist, owner or table capacity unavailable")
	case errors.Is(err, massage.ErrBookingTopology):
		return huma.Error409Conflict("massage party windows overlap; event configuration requires repair")
	default:
		return massageScheduleError(err)
	}
}

func massageBookingResult[T any](operation string, result T, err error) (*paymentWriteResponse[T], error) {
	out := &paymentWriteResponse[T]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := massageBookingError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "massage_operation_rejected",
			Message:        "booking, approval, versions, capacity or current account rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
