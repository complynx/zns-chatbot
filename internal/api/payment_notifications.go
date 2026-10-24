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

const schemaNotifications = "notifications"

type notificationListArguments struct {
	State string    `json:"state,omitempty"`
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

// Keep the state enum readable without changing its request validation.
func (notificationListArguments) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	s.Properties["state"].Enum = []any{
		agentPending, notificationStateRetryable, notificationStateInflight, notificationStateDelivered,
		notificationStateUnknown, notificationStateFailed, notificationStateBlocked, notificationStateDismissed,
	}
	return s
}

type notificationListResult struct {
	Notifications []db.NotificationSummary `json:"notifications"`
	Next          *uuid.UUID               `json:"next,omitempty"`
}

func (s *Server) registerNotificationAPI(api huma.API) {
	describeNotificationResolutionSchema(api.OpenAPI().Components.Schemas)
	s.registerPairNotificationAPI(api)
	s.registerOrderNotificationAPI(api)
	registerAgentPost(
		api,
		"agent-admin-knowledge-notification",
		"/internal/agent/administration/knowledge_notification",
		internalBodyLimit,
		s.knowledgeNoticeDiagnostics,
	)
	s.registerMassageReminderAPI(api)
	s.registerMassageSpecialistNotificationAPI(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-notifications",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/notifications",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentNotifications,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-notification",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/notification",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentNotification,
	)
}

func (s *Server) notificationScope() db.EventScope {
	return db.EventScope{BotID: s.options.BotID, Organization: s.options.OrganizationID}
}

func (s *Server) agentNotifications(
	ctx context.Context,
	in *agentReadRequest[notificationListArguments],
) (*agentReadResponse[notificationListResult], error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.ListAdminNotifications(ctx, s.notificationScope(), args.State, args.After, args.Limit)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("notification diagnostics unavailable")
	}
	result := notificationListResult{Notifications: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentNotification(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.PaymentNotification], error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	result, err := s.store.PaymentNotification(ctx, s.notificationScope(), in.Body.Arguments.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("notification not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("notification diagnostics unavailable")
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func notificationMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	methods := []map[string]any{
		notificationResolutionMethod(),
		administrativeRead(
			"notifications",
			"List this deployment's durable payment notification metadata, including blocked and "+
				"unknown delivery outcomes. UUID pages, max/default 20; follow next as after. "+
				"A Telegram timeout is not proof of non-delivery. "+
				"Do not resend unknown messages blindly.",
			map[string]any{
				schemaState: map[string]any{
					manifestType: schemaString,
					schemaEnum: []string{
						agentPending,
						notificationStateRetryable,
						notificationStateInflight,
						notificationStateDelivered,
						notificationStateUnknown,
						notificationStateFailed,
						notificationStateBlocked,
						notificationStateDismissed,
					},
				},
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
			"notification",
			"Inspect a durable payment notification's frozen business event, recipient, current "+
				"delivery state/version and Telegram receipt. "+
				"Current global administrator authority required. "+
				"No credentials or downloaded file bytes. Unknown send remains uncertain; "+
				"the ledger is independent of notification delivery.",
			map[string]any{"id": uuidField},
			[]string{"id"},
			[]string{"id", schemaState, schemaVersion, paymentSchemaReceipt},
		),
	}
	methods = append(methods, pairNotificationMethods()...)
	methods = append(methods, orderNotificationMethods()...)
	methods = append(methods, knowledgeNoticeDiagnosticsMethod())
	methods = append(methods, massageReminderMethods()...)
	return append(methods, massageSpecialistNotificationMethods()...)
}
