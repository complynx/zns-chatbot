package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const avatarResolutionPath = "/internal/agent/administration/resolve_avatar_delivery"
const schemaAvatarDeliveryID = "delivery_id"
const schemaAvatarResolutionAction = "action"

const avatarDeliveryPath = "/internal/agent/administration/avatar_delivery"

type avatarResolutionPreparationResponse struct {
	Body struct {
		Status        string                      `json:"status"`
		OperationID   string                      `json:"operation_id"`
		Arguments     db.AvatarDeliveryResolution `json:"arguments"`
		Preconditions assignmentPreconditions     `json:"preconditions"`
		Description   string                      `json:"description"`
		ExpiresAt     time.Time                   `json:"expires_at"`
	}
}
type avatarResolutionPreparationArguments struct {
	db.AvatarDeliveryResolution

	ResumeProposalID *uuid.UUID `json:"resume_proposal_id,omitempty"`
}

type avatarDeliveryArguments struct {
	DeliveryID uuid.UUID `json:"delivery_id"`
}

func (s *Server) registerAvatarDeliveryResolutions(api huma.API) {
	s.registerAvatarDeliveryProposals(api)
	s.registerMiniAppAvatarAdmin()
	registerAgentPost(
		api,
		"agent-prepare-avatar-delivery-resolution",
		avatarResolutionPath+"/prepare",
		internalBodyLimit,
		s.prepareAvatarDeliveryResolution,
	)
	registerAgentPost(
		api,
		"agent-resolve-avatar-delivery",
		avatarResolutionPath,
		internalBodyLimit,
		s.resolveAvatarDelivery,
	)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-avatar-delivery-resolution-"+suffix,
			avatarResolutionPath+"/"+suffix,
			internalBodyLimit,
			s.avatarDeliveryResolutionStatus,
		)
	}
	registerAgentPost(
		api,
		"agent-inspect-avatar-delivery",
		avatarDeliveryPath,
		internalBodyLimit,
		s.inspectAvatarDelivery,
	)
}
func (s *Server) avatarDeliveryAdminScope(ctx context.Context, auth AuthInput) (db.AvatarDeliveryAdminScope, error) {
	admin, err := s.adminScope(ctx, auth)
	profile := db.ProfileScope{
		BotID: admin.BotID, Organization: admin.Organization, UserID: admin.UserID, Subject: admin.Subject,
	}
	return db.AvatarDeliveryAdminScope{ProfileScope: profile, GlobalAdmin: admin.GlobalAdmin}, err
}
func avatarResolutionError(err error) error {
	switch {
	case errors.Is(err, payments.ErrDenied):
		return huma.Error403Forbidden("active global administrator required")
	case errors.Is(err, db.ErrInvalidAvatar):
		return huma.Error400BadRequest("invalid avatar delivery resolution")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("avatar delivery or proposal changed or expired")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("avatar delivery or proposal not found")
	default:
		return huma.Error503ServiceUnavailable("avatar delivery resolution unavailable")
	}
}
func avatarResolutionOperation(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	return id, nil
}

func (s *Server) prepareAvatarDeliveryResolution(
	ctx context.Context,
	in *agentReadRequest[avatarResolutionPreparationArguments],
) (*avatarResolutionPreparationResponse, error) {
	scope, err := s.avatarDeliveryAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := avatarResolutionOperation(in.Body.OperationID)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	var prep db.AvatarDeliveryPreparation
	if args.ResumeProposalID != nil {
		prep, err = s.store.AdoptAvatarDeliveryResolution(
			ctx,
			scope,
			op,
			*args.ResumeProposalID,
			args.AvatarDeliveryResolution,
		)
	} else {
		prep, err = s.store.PrepareAvatarDeliveryResolution(ctx, scope, op, args.AvatarDeliveryResolution)
	}
	if err != nil {
		return nil, avatarResolutionError(err)
	}
	out := &avatarResolutionPreparationResponse{}
	out.Body.Status = agentPrepared
	out.Body.OperationID = in.Body.OperationID
	out.Body.Arguments = prep.Arguments
	out.Body.Preconditions.ProposalID = prep.ID
	out.Body.Description = prep.Description
	out.Body.ExpiresAt = prep.ExpiresAt
	return out, nil
}

func (s *Server) resolveAvatarDelivery(
	ctx context.Context,
	in *paymentWriteRequest[avatarResolutionPreparationArguments],
) (*paymentWriteResponse[db.AvatarDeliveryResolutionResult], error) {
	scope, err := s.avatarDeliveryAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	op, err := avatarDraftOperationID(body.OperationID, body.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if body.Confirmation == nil || body.Confirmation.Mode != assignmentConfirmationMode ||
		body.Confirmation.Preconditions == nil ||
		body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("exact proposal confirmation required")
	}
	if body.Arguments.ResumeProposalID != nil &&
		*body.Arguments.ResumeProposalID != body.Confirmation.Preconditions.ProposalID {
		return nil, huma.Error400BadRequest("resume proposal must match exact confirmation")
	}
	result, err := s.store.ResolveAvatarDelivery(
		ctx,
		scope,
		op,
		body.Arguments.AvatarDeliveryResolution,
		body.Confirmation.Preconditions.ProposalID,
	)
	out := &paymentWriteResponse[db.AvatarDeliveryResolutionResult]{}
	out.Body.OperationID = body.OperationID
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrInvalidAvatar) || errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "avatar_delivery_resolution_rejected",
			Message:        "Avatar delivery or exact proposal changed, is missing, or expired",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, avatarResolutionError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}

func (s *Server) avatarDeliveryResolutionStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.AvatarDeliveryResolutionResult], error) {
	scope, err := s.avatarDeliveryAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := avatarResolutionOperation(in.Body.OperationID)
	if err != nil {
		return nil, err
	}
	result, err := s.store.AvatarDeliveryResolutionReceipt(ctx, scope, op)
	out := &paymentWriteResponse[db.AvatarDeliveryResolutionResult]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, avatarResolutionError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}

func (s *Server) inspectAvatarDelivery(
	ctx context.Context,
	in *agentReadRequest[avatarDeliveryArguments],
) (*agentReadResponse[db.AvatarDelivery], error) {
	scope, err := s.avatarDeliveryAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if in.Body.Arguments.DeliveryID == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero delivery ID required")
	}
	n, err := s.store.AvatarDelivery(
		ctx,
		db.EventScope{BotID: scope.BotID, Organization: scope.Organization},
		in.Body.Arguments.DeliveryID,
	)
	if err != nil {
		return nil, avatarResolutionError(err)
	}
	return agentReadResult(in.Body.OperationID, n), nil
}
func avatarDeliveryAdministrativeMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	method := administrativeRead(
		"resolve_avatar_delivery",
		"Retry or dismiss a saved-draft avatar document with current global administrator "+
			"authority. Inspect avatar_delivery and the Telegram chat first: unknown may already be "+
			"delivered; retry can duplicate the document and is a last resort. "+
			"Never generate another avatar to retry delivery. "+
			"Exact confirmation binds delivery/version/action/reason. Dismiss does not prove delivery. "+
			"Retry rechecks the captured recipient. Reconcile the same operation ID before any new "+
			"action. To continue your existing unexpired proposal from another interface, use its "+
			"exact original arguments plus optional resume_proposal_id; "+
			"this binds the new operation to that proposal without extending expiry or supplying "+
			"consent. A fresh exact approval is still required. "+
			"Completed/expired proposals must be reconciled, never adopted as fresh consent.",
		map[string]any{
			schemaAvatarDeliveryID: uuidField,
			schemaResumeProposalID: uuidField,
			schemaExpectedVersion:  map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			schemaAvatarResolutionAction: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"retry", "dismiss"},
			},
			schemaReason: map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: payments.MaxReasonLength,
			},
		},
		[]string{schemaAvatarDeliveryID, schemaExpectedVersion, schemaAvatarResolutionAction, schemaReason},
		[]string{schemaAvatarDeliveryID, "recipient_id", schemaState, schemaVersion},
	)
	method[manifestEffect] = effectWrite
	method[manifestIdempotency] = true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = avatarResolutionPath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: avatarResolutionPath + "/prepare",
	}
	return append([]map[string]any{
		method,
		administrativeRead(
			"avatar_delivery",
			"Inspect one saved-draft avatar delivery in this bot and organization, including the "+
				"captured recipient, version, attempt and bounded Telegram receipt. "+
				"No image bytes or credentials are returned. "+
				"An unknown outcome is not proof of non-delivery. "+
				"Use resolve_avatar_delivery only after explicit confirmation.",
			map[string]any{schemaAvatarDeliveryID: uuidField},
			[]string{schemaAvatarDeliveryID},
			[]string{"id", "artifact_id", schemaUserID, schemaState, schemaVersion, "receipt"},
		),
	}, avatarDeliveryProposalMethods()...)
}
