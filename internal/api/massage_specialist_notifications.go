package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type massageSpecialistNotificationListResult struct {
	Notifications []db.MassageSpecialistNotificationSummary `json:"notifications"`
	Next          *uuid.UUID                                `json:"next,omitempty"`
}

func (s *Server) registerMassageSpecialistNotificationAPI(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-massage-specialist-notifications",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/massage_specialist_notifications",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentMassageSpecialistNotifications,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-massage-specialist-notification",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/massage_specialist_notification",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentMassageSpecialistNotification,
	)
}

func (s *Server) agentMassageSpecialistNotifications(
	ctx context.Context,
	in *agentReadRequest[notificationListArguments],
) (*agentReadResponse[massageSpecialistNotificationListResult], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.ListAdminMassageSpecialistNotifications(
		ctx,
		scope.EventScope,
		args.State,
		args.After,
		args.Limit,
	)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("massage specialist notification diagnostics unavailable")
	}
	result := massageSpecialistNotificationListResult{Notifications: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentMassageSpecialistNotification(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.MassageSpecialistNotificationDetail], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	n, err := s.store.AdminMassageSpecialistNotification(ctx, scope.EventScope, in.Body.Arguments.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, huma.Error404NotFound("massage specialist notification not found")
	case err != nil:
		return nil, huma.Error503ServiceUnavailable("massage specialist notification diagnostics unavailable")
	}
	raw, err := json.Marshal(n)
	if err != nil || len(raw) > massagePageResultLimit {
		return nil, huma.Error503ServiceUnavailable("massage specialist notification diagnostics exceed response bound")
	}
	return agentReadResult(in.Body.OperationID, n), nil
}

func massageSpecialistNotificationMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return []map[string]any{
		administrativeRead(
			"massage_specialist_notifications",
			"List this deployment's specialist booking/cancellation notification meta"+
				"data with current global administrator authority. Default/max page20; fo"+
				"llow next as after. Captured grant/preference generations are immutable."+
				" Unknown does not prove non-delivery. No outbound payload, client name o"+
				"r contact is returned.",
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
			"massage_specialist_notification",
			"Inspect specialist notification metadata, state/version, captured grant/"+
				"preference generations, attempt ID and Telegram receipt. No raw outbound"+
				" payload or client contact/profile. Use resolve_notification with notifi"+
				"cation_kind=massage_specialist for exact audited retry/dismiss. Unknown "+
				"retry may duplicate; inspect receipt/chat first and retry only as a last"+
				" resort. Retry never rebases captured generations and rechecks current m"+
				"odule, recipient, binding, preference and booking state. Cancelled notic"+
				"es require a cancelled booking. Never repeat the business action to rese"+
				"nd a notice.",
			map[string]any{"id": uuidField},
			[]string{"id"},
			[]string{"id", schemaState, schemaVersion, paymentSchemaReceipt},
		),
	}
}
