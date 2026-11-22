package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	identityReceiptTimeout = 5 * time.Second
	identityDeliveryMethod = "send_identity_export"
	identityDeliveryPath   = "/internal/agent/registrations/" + identityDeliveryMethod
)

type identityDeliveryArgs struct {
	EventID uuid.UUID `json:"event_id"`
}
type identityDeliveryResult struct {
	DeliveryStatus string `json:"delivery_status"`
}
type identityDeliveryPreparation struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     identityDeliveryArgs    `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

func (s *Server) registrationTools() []any {
	methods := registrationMethods()
	if s.options.Telegram == nil || s.options.RegistrationDetails == nil {
		return methods
	}
	method := paymentMethod(
		identityDeliveryMethod,
		"On request, send the selected event's protected identity ZIP to the organizer's private Telegram chat. "+
			"Resolve the event first. Shuttle chooses the recipient from the authenticated account. "+
			"Only delivery_status is returned. Never fetch, inspect, relay or request the archive or a Telegram file ID. "+
			"After delivery_unknown check the chat; never repeat an uncertain send. For too_large use the Mini App export.",
		map[string]any{
			schemaEventID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		},
		[]string{schemaEventID},
		nil,
	)
	method[manifestPath] = identityDeliveryPath
	method[manifestEffect] = effectWrite
	method[manifestDisclosure] = true
	method[manifestTimeout] = passIdentityExportTimeout.Milliseconds()
	method[manifestIdempotency] = true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = identityDeliveryPath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: identityDeliveryPath + "/prepare",
	}
	method[manifestOutputSchema] = map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaProperties: map[string]any{
			"delivery_status": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{db.IdentitySent, db.IdentityFailed, db.IdentityUnknown, db.IdentityTooLarge},
			},
		},
		schemaRequired: []string{"delivery_status"},
	}
	return append(methods, method)
}
func (s *Server) registerIdentityDelivery(api huma.API) {
	if s.options.Telegram == nil || s.options.RegistrationDetails == nil {
		return
	}
	registerAgentPost(
		api,
		"agent-identity-delivery-prepare",
		identityDeliveryPath+"/prepare",
		internalBodyLimit,
		s.prepareIdentityDelivery,
	)
	registerAgentPost(api, "agent-identity-delivery", identityDeliveryPath, internalBodyLimit, s.sendIdentityDelivery)
	registerAgentPost(
		api,
		"agent-identity-delivery-status",
		identityDeliveryPath+"/status",
		internalBodyLimit,
		s.identityDeliveryStatus,
	)
}

func (s *Server) prepareIdentityDelivery(
	ctx context.Context,
	in *agentReadRequest[identityDeliveryArgs],
) (*identityDeliveryPreparation, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	d, err := s.store.PrepareIdentityDelivery(ctx, scope, in.Body.Arguments.EventID, in.Body.OperationID)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	out := &identityDeliveryPreparation{}
	out.Body.Status = agentPrepared
	out.Body.OperationID = in.Body.OperationID
	out.Body.Arguments = in.Body.Arguments
	out.Body.Preconditions.ProposalID = d.ProposalID
	out.Body.ExpiresAt = d.ExpiresAt
	out.Body.Description = fmt.Sprintf(
		"Send the protected participant identity ZIP for %q (%s) to your private Telegram chat.",
		d.EventName,
		d.EventID,
	)
	return out, nil
}

func (s *Server) sendIdentityDelivery(
	ctx context.Context,
	in *paymentWriteRequest[identityDeliveryArgs],
) (*paymentWriteResponse[identityDeliveryResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if b.IdempotencyKey != b.OperationID || b.Confirmation == nil || b.Confirmation.Mode != assignmentConfirmationMode ||
		b.Confirmation.Preconditions == nil || b.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation and exact confirmation required")
	}
	d, err := s.store.IdentityDeliveryStatus(ctx, scope, b.OperationID)
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	if d.ProposalID != b.Confirmation.Preconditions.ProposalID || d.EventID != b.Arguments.EventID {
		return nil, pairAssignmentError(db.ErrConflict)
	}
	if d.State != db.IdentityPrepared {
		return paymentStatusResult(b.OperationID, &identityDeliveryResult{d.State}, nil)
	}
	if !time.Now().Before(d.ExpiresAt) {
		return nil, pairAssignmentError(db.ErrConflict)
	}
	token := strings.TrimPrefix(in.Authorization, "Bearer ")
	err = s.buildPassIdentity(
		ctx,
		scope,
		nil,
		d.EventID,
		func(policy db.PassIdentityPolicy, archive io.Reader, size int64) error {
			claimed, send, claimErr := s.store.ReserveIdentityDelivery(ctx, scope, b.OperationID, d.ProposalID, policy,
				s.identityDeliveryAuthority(token, scope), size > telegram.MaxArchiveBytes)
			if claimErr != nil {
				return claimErr
			}
			d = claimed
			if send {
				d.State, claimErr = s.deliverIdentityArchive(ctx, scope.UserID, d, archive, size)
			}
			return claimErr
		},
	)
	if errors.Is(err, identity.ErrDenied) {
		return nil, huma.Error403Forbidden("current organizer authorization required")
	}
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	return paymentStatusResult(b.OperationID, &identityDeliveryResult{d.State}, nil)
}

func (s *Server) identityDeliveryAuthority(token string, scope db.PaymentScope) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		principal, err := s.iam.VerifyFresh(ctx, token)
		if err != nil {
			return false, err
		}
		if principal.Subject != scope.Subject || principal.OrganizationID != scope.Organization {
			return false, db.ErrPairDenied
		}
		_, admin := principal.Roles["zinusya.admin"][scope.Organization]
		return admin, nil
	}
}

func (s *Server) deliverIdentityArchive(
	ctx context.Context,
	user uuid.UUID,
	d db.IdentityDelivery,
	archive io.Reader,
	size int64,
) (string, error) {
	err := s.options.Telegram.SendArchive(ctx, d.Recipient, "identity-"+d.EventID.String()+".zip", archive, size)
	state := db.IdentitySent
	if err != nil {
		var tgErr *telegram.Error
		if !errors.As(err, &tgErr) || tgErr.Outcome == telegram.Unknown {
			return db.IdentityUnknown, nil
		}
		state = db.IdentityFailed
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), identityReceiptTimeout)
	defer cancel()
	return state, s.store.FinishIdentityDelivery(finish, user, d.ProposalID, state)
}

func (s *Server) identityDeliveryStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[identityDeliveryResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	d, err := s.store.IdentityDeliveryStatus(ctx, scope, in.Body.OperationID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && d.State == db.IdentityPrepared {
		return paymentStatusResult[identityDeliveryResult](in.Body.OperationID, nil, nil)
	}
	if err != nil {
		return nil, pairAssignmentError(err)
	}
	return paymentStatusResult(in.Body.OperationID, &identityDeliveryResult{d.State}, nil)
}
