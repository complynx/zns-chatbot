package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const schemaNotificationID = "notification_id"

const notificationResolutionPath = "/internal/agent/administration/resolve_notification"

type notificationPreparationResponse struct {
	Body struct {
		Status        string                    `json:"status"`
		OperationID   string                    `json:"operation_id"`
		Arguments     db.NotificationResolution `json:"arguments"`
		Preconditions assignmentPreconditions   `json:"preconditions"`
		Description   string                    `json:"description"`
		ExpiresAt     time.Time                 `json:"expires_at"`
	}
}

func (s *Server) registerNotificationResolutions(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-prepare-notification-resolution",
			Method:       http.MethodPost,
			Path:         notificationResolutionPath + "/prepare",
			MaxBodyBytes: internalBodyLimit,
		},
		s.prepareNotificationResolution,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-resolve-notification",
			Method:       http.MethodPost,
			Path:         notificationResolutionPath,
			MaxBodyBytes: internalBodyLimit,
		},
		s.resolveNotification,
	)
	for _, suffix := range []string{"status", "reconcile"} {
		huma.Register(
			api,
			huma.Operation{
				OperationID:  "agent-notification-resolution-" + suffix,
				Method:       http.MethodPost,
				Path:         notificationResolutionPath + "/" + suffix,
				MaxBodyBytes: internalBodyLimit,
			},
			s.notificationResolutionStatus,
		)
	}
}

func (s *Server) notificationAdminScope(ctx context.Context, auth AuthInput) (db.PaymentScope, error) {
	scope, err := s.paymentScope(ctx, auth)
	if err != nil {
		return scope, err
	}
	if !scope.GlobalAdmin {
		return scope, huma.Error403Forbidden("global administrator required")
	}
	return scope, nil
}

func notificationResolutionError(err error) error {
	switch {
	case errors.Is(err, payments.ErrDenied):
		return huma.Error403Forbidden("active global administrator required")
	case errors.Is(err, payments.ErrInvalid):
		return huma.Error400BadRequest("invalid notification resolution")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("notification or proposal changed or expired")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("notification or proposal not found")
	default:
		return huma.Error503ServiceUnavailable("notification resolution unavailable")
	}
}

func (s *Server) prepareNotificationResolution(
	ctx context.Context,
	in *agentReadRequest[db.NotificationResolution],
) (*notificationPreparationResponse, error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PrepareNotificationResolution(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, notificationResolutionError(err)
	}
	out := &notificationPreparationResponse{}
	out.Body.Status, out.Body.OperationID = agentPrepared, in.Body.OperationID
	out.Body.Arguments, out.Body.Description, out.Body.ExpiresAt = prep.Arguments, prep.Description, prep.ExpiresAt
	out.Body.Preconditions.ProposalID = prep.ID
	return out, nil
}

func (s *Server) resolveNotification(
	ctx context.Context,
	in *paymentWriteRequest[db.NotificationResolution],
) (*paymentWriteResponse[db.NotificationResolutionResult], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
		body.Confirmation.Mode != assignmentConfirmationMode || body.Confirmation.Preconditions == nil {
		return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
	}
	result, err := s.store.ResolveNotification(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	out := &paymentWriteResponse[db.NotificationResolutionResult]{}
	out.Body.OperationID = body.OperationID
	if errors.Is(err, db.ErrConflict) || errors.Is(err, payments.ErrInvalid) || errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "notification_resolution_rejected",
			Message:        "Notification or exact proposal changed, is missing, or expired",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, notificationResolutionError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func (s *Server) notificationResolutionStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.NotificationResolutionResult], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.NotificationResolutionReceipt(ctx, scope, in.Body.OperationID)
	out := &paymentWriteResponse[db.NotificationResolutionResult]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) || err == nil && receipt == nil {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, notificationResolutionError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, receipt
	return out, nil
}

func notificationResolutionMethod() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	method := administrativeRead(
		"resolve_notification",
		"Retry or dismiss an uncertain, failed or blocked notification with curre" +
"nt global administrator authority. Inspect its receipt and chat first: a" +
"n unknown send may already be delivered, so retry can duplicate the mess" +
"age and is a last resort. Dismiss closes delivery only and does not prov" +
"e receipt. Exact proposal includes recipient, event, state/version and r" +
"eason. Never replay the payment or booking to resend a message. Same ope" +
"ration ID returns the original resolution receipt; reconcile before any " +
"new operation. Retry rechecks recipient permissions and increments the d" +
"elivery generation.",
		map[string]any{
			"notification_kind": map[string]any{
				manifestType:        schemaString,
				schemaEnum:          []string{"payment", "pair", "order", "massage", "massage_specialist", "knowledge"},
				manifestDescription: "Defaults to payment for compatibility. Use pair for invitation notificat" +
"ions, order for order payment evidence notices, massage for owner bookin" +
"g reminders, massage_specialist for specialist booking/cancellation noti" +
"ces, or knowledge for knowledge suggestion notices. For knowledge only, " +
"omit event_id for general/history suggestions without event origin; even" +
"t-origin suggestions require their original event_id.",
			},
			schemaEventID: map[string]any{
				manifestType:        schemaString,
				schemaFormat:        schemaUUID,
				manifestDescription: "Required except for knowledge notices whose suggestion has no event origin",
			},
			schemaNotificationID: uuidField,
			"expected_version":   map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			"action":             map[string]any{manifestType: schemaString, schemaEnum: []string{"retry", "dismiss"}},
			"reason": map[string]any{
				manifestType:    schemaString,
				"minLength":     1,
				schemaMaxLength: payments.MaxReasonLength,
			},
		},
		[]string{
			"notification_id",
			"expected_version",
			"action",
			"reason",
		},
		[]string{schemaNotificationID, schemaState, schemaVersion},
	)
	method[manifestEffect] = effectWrite
	method[manifestIdempotency] = true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestStatusPath] = notificationResolutionPath + "/status"
	method[manifestConfirmation] = map[string]any{
		confirmationMode:    assignmentConfirmationMode,
		manifestPreparePath: notificationResolutionPath + "/prepare",
	}
	return method
}
