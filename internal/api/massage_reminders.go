package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type massageReminderListResult struct {
	Notifications []db.MassageReminderSummary `json:"notifications"`
	Next          *uuid.UUID                  `json:"next,omitempty"`
}

func (s *Server) registerMassageReminderAPI(api huma.API) {
	registerAgentPost(
		api,
		"agent-admin-massage-reminders",
		"/internal/agent/administration/massage_reminders",
		internalBodyLimit,
		s.agentMassageReminders,
	)
	registerAgentPost(
		api,
		"agent-admin-massage-reminder",
		"/internal/agent/administration/massage_reminder",
		internalBodyLimit,
		s.agentMassageReminder,
	)
}

func (s *Server) agentMassageReminders(
	ctx context.Context,
	in *agentReadRequest[notificationListArguments],
) (*agentReadResponse[massageReminderListResult], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.ListAdminMassageReminders(ctx, scope.EventScope, args.State, args.After, args.Limit)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("massage reminder diagnostics unavailable")
	}
	result := massageReminderListResult{Notifications: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentMassageReminder(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.MassageReminder], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	n, err := s.store.MassageReminder(ctx, scope.EventScope, in.Body.Arguments.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("massage reminder not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("massage reminder diagnostics unavailable")
	}
	raw, err := json.Marshal(n)
	if err != nil || len(raw) > massagePageResultLimit {
		return nil, huma.Error503ServiceUnavailable("massage reminder diagnostics exceed response bound")
	}
	return agentReadResult(in.Body.OperationID, n), nil
}

func massageReminderMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return []map[string]any{
		administrativeRead(
			"massage_reminders",
			"List this deployment's owner massage reminder metadata with current global administrator "+
				"authority. Default/max page20; follow next as after. "+
				"Due and retry embargo are independent; "+
				"unknown delivery does not prove non-delivery. No raw outbound payload is returned.",
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
			"massage_reminder",
			"Inspect owner massage reminder state/version, due/retry times, attempt ID, Telegram "+
				"receipt and frozen booking terms. "+
				"Use resolve_notification with notification_kind=massage for exact audited "+
				"retry/dismiss; retry of unknown may duplicate and requires checking the "+
				"receipt/chat first. Cancelled bookings cannot be retried. "+
				"Delivery does not confirm payment; never repeat the booking to resend a reminder.",
			map[string]any{"id": uuidField},
			[]string{"id"},
			[]string{"id", schemaState, schemaVersion, paymentSchemaReceipt},
		),
	}
}
