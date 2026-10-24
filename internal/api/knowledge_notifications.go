package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type knowledgeNotificationArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int       `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

func (s *Server) knowledgeNotifications(
	ctx context.Context,
	in *agentReadRequest[knowledgeNotificationArguments],
) (*agentReadResponse[db.KnowledgeNotificationPage], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ListKnowledgeNotifications(ctx, scope, in.Body.Arguments.After, in.Body.Arguments.Limit)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := &agentReadResponse[db.KnowledgeNotificationPage]{}
	out.Body.Status = agentSucceeded
	out.Body.OperationID = in.Body.OperationID
	out.Body.Result = result
	return out, nil
}
