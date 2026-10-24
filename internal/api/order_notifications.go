package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) registerOrderNotificationAPI(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:  "agent-admin-order-notifications",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/order_notifications",
		MaxBodyBytes: internalBodyLimit,
	}, s.agentOrderNotifications)
	huma.Register(api, huma.Operation{
		OperationID:  "agent-admin-order-notification",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/order_notification",
		MaxBodyBytes: internalBodyLimit,
	}, s.agentOrderNotification)
}

func (s *Server) agentOrderNotifications(
	ctx context.Context,
	in *agentReadRequest[notificationListArguments],
) (*agentReadResponse[notificationListResult], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.ListAdminOrderNotifications(ctx, scope.EventScope, args.State, args.After, args.Limit)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("order notification diagnostics unavailable")
	}
	result := notificationListResult{Notifications: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentOrderNotification(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.OrderNotification], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OrderNotification(ctx, scope.EventScope, in.Body.Arguments.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("order notification not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("order notification diagnostics unavailable")
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func orderNotificationMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return []map[string]any{
		administrativeRead(
			"order_notifications",
			"List this deployment's order payment evidence delivery metadata with cur" +
"rent global administrator authority. Default/max page 20; follow next as" +
" after. Unknown delivery is not proof of non-delivery.",
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
			"order_notification",
			"Inspect an order payment evidence notification's recipient, state/versio" +
"n, attempt ID, Telegram receipt and original evidence and sold order ter" +
"ms. Use resolve_notification with notification_kind=order for an exact, " +
"audited retry or dismissal; unknown retry may duplicate a message. Never" +
" repeat the order business action to resend a notification.",
			map[string]any{"id": uuidField},
			[]string{"id"},
			[]string{"id", schemaState, schemaVersion, paymentSchemaReceipt},
		),
	}
}
