package api

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func knowledgeOperationID(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return huma.Error400BadRequest("a nonzero UUID operation_id is required")
	}
	return nil
}

func knowledgeWriteResult(
	op string,
	result db.KnowledgeResult,
	err error,
) (*paymentWriteResponse[knowledgeResultView], error) {
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrKnowledgeDenied) ||
		errors.Is(err, knowledge.ErrInvalid) ||
		errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[knowledgeResultView]{}
		out.Body.OperationID, out.Body.Status = op, agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "knowledge_rejected",
			Message:        knowledgeError(err).Error(),
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	if err != nil {
		return nil, knowledgeError(err)
	}
	view := knowledgeResultProject(result)
	return paymentStatusResult(op, &view, nil)
}

// Each public verb has its own argument type. The server fixes Kind; normalized
// preparations return only that verb's fields, suitable for unchanged confirmation.
func registerKnowledgeWrite[T any](
	api huma.API,
	s *Server,
	kind string,
	toChange func(T) db.KnowledgeChange,
	fromChange func(db.KnowledgeChange) T,
) {
	registerAgentPost(
		api,
		"agent-knowledge-"+kind+"-prepare",
		knowledgePath+kind+"/prepare",
		knowledgeTransportLimit,
		knowledgePrepareHandler(s, kind, toChange, fromChange),
	)
	registerAgentPost(
		api,
		"agent-knowledge-"+kind,
		knowledgePath+kind,
		knowledgeTransportLimit,
		knowledgeConfirmHandler(s, kind, toChange),
	)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-knowledge-"+kind+"-"+suffix,
			knowledgePath+kind+"/"+suffix,
			internalBodyLimit,
			knowledgeStatusHandler(s, kind),
		)
	}
}
func knowledgeEditChange(args knowledgeEditArguments) db.KnowledgeChange {
	return db.KnowledgeChange{
		ArticleID:        args.ArticleID,
		ExpectedRevision: args.ExpectedRevision,
		EventID:          args.EventID,
		Content:          &args.Content,
		Reason:           args.Reason,
	}
}
func knowledgeEditFromChange(c db.KnowledgeChange) knowledgeEditArguments {
	out := knowledgeEditArguments{
		ArticleID:        c.ArticleID,
		ExpectedRevision: c.ExpectedRevision,
		EventID:          c.EventID,
		Reason:           c.Reason,
	}
	if c.Content != nil {
		out.Content = *c.Content
	}
	return out
}
func knowledgeReviewChange(args knowledgeReviewArguments) db.KnowledgeChange {
	return db.KnowledgeChange{
		SuggestionID:     args.SuggestionID,
		ExpectedRevision: args.ExpectedRevision,
		Decision:         args.Decision,
		Reason:           args.Reason,
	}
}
func knowledgeReviewFromChange(c db.KnowledgeChange) knowledgeReviewArguments {
	return knowledgeReviewArguments{
		SuggestionID:     c.SuggestionID,
		ExpectedRevision: c.ExpectedRevision,
		Decision:         c.Decision,
		Reason:           c.Reason,
	}
}
func knowledgeModeratorChange(args knowledgeModeratorArguments) db.KnowledgeChange {
	return db.KnowledgeChange{
		ModeratorUserID:          args.ModeratorUserID,
		ModeratorEnabled:         &args.ModeratorEnabled,
		ExpectedModeratorVersion: args.ExpectedModeratorVersion,
		Reason:                   args.Reason,
	}
}
func knowledgeModeratorFromChange(c db.KnowledgeChange) knowledgeModeratorArguments {
	out := knowledgeModeratorArguments{
		ModeratorUserID:          c.ModeratorUserID,
		ExpectedModeratorVersion: c.ExpectedModeratorVersion,
		Reason:                   c.Reason,
	}
	if c.ModeratorEnabled != nil {
		out.ModeratorEnabled = *c.ModeratorEnabled
	}
	return out
}
