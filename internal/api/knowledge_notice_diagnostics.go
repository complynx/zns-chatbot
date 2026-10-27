package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

// The administrator detail excludes proposed content and the outbound payload.
type knowledgeNoticeDiagnostics struct {
	db.KnowledgeNotification

	EventID    *uuid.UUID      `json:"event_id,omitempty"`
	AttemptID  uuid.UUID       `json:"attempt_id"`
	Receipt    json.RawMessage `json:"receipt"`
	ObservedAt time.Time       `json:"observed_at"`
}

func (s *Server) knowledgeNoticeDiagnostics(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[knowledgeNoticeDiagnostics], error) {
	scope, err := s.notificationAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	n, err := s.store.KnowledgeNotification(ctx, scope.EventScope, in.Body.Arguments.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("knowledge notification not found")
	}
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("knowledge notification diagnostics unavailable")
	}
	return agentReadResult(
		in.Body.OperationID,
		knowledgeNoticeDiagnostics{
			KnowledgeNotification: n,
			EventID:               n.EventID,
			AttemptID:             n.AttemptID,
			Receipt:               n.Receipt,
			ObservedAt:            time.Now().UTC(),
		},
	), nil
}

func knowledgeNoticeDiagnosticsMethod() map[string]any {
	return administrativeRead(
		"knowledge_notification",
		"Inspect a knowledge notice's delivery locators, original event origin, attempt ID and "+
			"Telegram receipt with current global administrator authority. "+
			"No proposed text or raw outbound payload is returned. "+
			"Use resolve_notification with notification_kind=knowledge for exact audited "+
			"retry/dismiss. Pass event_id only for event-origin suggestions; "+
			"omit it for general suggestions. Unknown may already be delivered: check receipt/chat "+
			"before an explicit last-resort retry. Never repeat suggestion or moderation to resend a "+
			"notice.",
		map[string]any{
			"id": map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		},
		[]string{"id"},
		[]string{"id", schemaState, schemaVersion, paymentSchemaReceipt},
	)
}
