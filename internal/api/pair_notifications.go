package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type pairNoticeListResult struct {
	Notifications []db.PairInvitationNotification `json:"notifications"`
	Next          *uuid.UUID                      `json:"next,omitempty"`
}

func (s *Server) registerPairNotificationAPI(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:  "agent-admin-pair-notifications",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/pair_notifications",
		MaxBodyBytes: internalBodyLimit,
	}, s.agentPairNotifications)
	huma.Register(api, huma.Operation{
		OperationID:  "agent-admin-pair-notification",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/pair_notification",
		MaxBodyBytes: internalBodyLimit,
	}, s.agentPairNotification)
}

func (s *Server) agentPairNotifications(
	ctx context.Context,
	in *agentReadRequest[notificationListArguments],
) (*agentReadResponse[pairNoticeListResult], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.ListAdminPairNotifications(ctx, scope.EventScope, args.State, args.After, args.Limit)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("pair notification diagnostics unavailable")
	}
	result := pairNoticeListResult{Notifications: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentPairNotification(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.PairInvitationDelivery], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PairInvitationNotification(ctx, scope.EventScope, in.Body.Arguments.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("pair notification not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("pair notification diagnostics unavailable")
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func pairNotificationMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return []map[string]any{
		administrativeRead(
			"pair_notifications",
			"List this deployment's pair invitation delivery metadata and receipts with current global administrator authority. Default/max page 20; follow next as after. Unknown delivery is not proof of non-delivery.",
			map[string]any{
				schemaState: map[string]any{manifestType: schemaString},
				schemaAfter: uuidField,
				schemaLimit: map[string]any{
					manifestType:  schemaInteger,
					schemaMinimum: 1,
					schemaMaximum: agentTaskPageLimit,
				},
			},
			nil,
			[]string{schemaNotifications},
		),
		administrativeRead(
			"pair_notification",
			"Inspect a pair invitation notification's recipient, state/version, attempt ID, Telegram receipt and current event/invitation context. Use resolve_notification with notification_kind=pair for an exact, audited retry or dismissal; unknown retry may duplicate a message. Never repeat the invitation business action to resend a notification.",
			map[string]any{"id": uuidField},
			[]string{"id"},
			[]string{"id", schemaState, schemaVersion, paymentSchemaReceipt},
		),
	}
}
