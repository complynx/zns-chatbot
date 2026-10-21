package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

type miniAppKnowledgePrepareInput[T any] struct {
	OperationID string `json:"operation_id"`
	Arguments   T      `json:"arguments"`
}
type miniAppKnowledgeConfirmInput[T any] struct {
	OperationID string    `json:"operation_id"`
	Arguments   T         `json:"arguments"`
	ProposalID  uuid.UUID `json:"proposal_id"`
	Confirmed   bool      `json:"confirmed"`
}
type miniAppKnowledgeStatusInput struct {
	OperationID string `json:"operation_id"`
}

func registerMiniAppKnowledgeWrite[T any](
	s *Server,
	kind string,
	toChange func(T) db.KnowledgeChange,
	fromChange func(db.KnowledgeChange) T,
) {
	registerMiniAppKnowledgePost(
		s,
		kind+"/prepare",
		func(ctx context.Context, scope db.PaymentScope, session db.MiniAppKnowledgeSession, in miniAppKnowledgePrepareInput[T]) (any, error) {
			if err := knowledgeOperationID(in.OperationID); err != nil {
				return nil, err
			}
			change, err := miniAppKnowledgeWriteChange(in.Arguments, toChange)
			if err != nil {
				return nil, err
			}
			change.Kind = kind
			change = miniAppKnowledgePreparedChange(scope, in.OperationID, change)
			prep, err := s.store.PrepareMiniAppKnowledge(ctx, scope, in.OperationID, change, session)
			if err != nil {
				return nil, err
			}
			out := knowledgePreparationResponse[T]{}
			out.Body.Status, out.Body.OperationID = agentPrepared, in.OperationID
			out.Body.Arguments = fromChange(prep.Arguments)
			out.Body.Preconditions.ProposalID = prep.ID
			out.Body.Description, out.Body.ExpiresAt = prep.Description, prep.ExpiresAt
			return out.Body, nil
		},
	)
	registerMiniAppKnowledgePost(
		s,
		kind+"/confirm",
		func(ctx context.Context, scope db.PaymentScope, session db.MiniAppKnowledgeSession, in miniAppKnowledgeConfirmInput[T]) (any, error) {
			if err := knowledgeOperationID(in.OperationID); err != nil {
				return nil, err
			}
			if !in.Confirmed || in.ProposalID == uuid.Nil {
				return nil, knowledge.ErrInvalid
			}
			change, err := miniAppKnowledgeWriteChange(in.Arguments, toChange)
			if err != nil {
				return nil, err
			}
			change.Kind = kind
			result, err := s.store.ConfirmMiniAppKnowledge(ctx, scope, in.OperationID, change, in.ProposalID, session)
			if err != nil {
				return nil, err
			}
			return knowledgeResultProject(result), nil
		},
	)
	registerMiniAppKnowledgePost(
		s,
		kind+"/status",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in miniAppKnowledgeStatusInput) (any, error) {
			if err := knowledgeOperationID(in.OperationID); err != nil {
				return nil, err
			}
			action, err := s.store.KnowledgeAction(ctx, scope, kind, in.OperationID)
			if err != nil {
				return nil, err
			}
			return miniAppKnowledgeActionProject(action, fromChange), nil
		},
	)
}

// New targets use the same stable actor/kind/operation identity as agent tools.
func miniAppKnowledgePreparedChange(
	scope db.PaymentScope,
	operation string,
	change db.KnowledgeChange,
) db.KnowledgeChange {
	if (change.Kind == "suggest" || change.Kind == "edit") && change.ArticleID == uuid.Nil &&
		change.ExpectedRevision == 0 {
		change.ArticleID = uuid.NewSHA1(scope.UserID, []byte("knowledge:"+change.Kind+":"+operation))
	}
	return change
}

// Original preparations are recoverable across channels. They are historical
// terms, not permission to execute or proof that current revisions still match.
type miniAppKnowledgePreparationView[T any] struct {
	Arguments     T                       `json:"arguments"`
	Preconditions assignmentPreconditions `json:"preconditions"`
	Description   string                  `json:"description"`
	ExpiresAt     time.Time               `json:"expires_at"`
}
type miniAppKnowledgeActionView[T any] struct {
	OperationID string                              `json:"operation_id"`
	Status      string                              `json:"status"`
	ObservedAt  time.Time                           `json:"observed_at"`
	Preparation *miniAppKnowledgePreparationView[T] `json:"preparation,omitempty"`
	Result      *knowledgeResultView                `json:"result,omitempty"`
}

func miniAppKnowledgeActionProject[T any](
	action db.KnowledgeActionView,
	fromChange func(db.KnowledgeChange) T,
) miniAppKnowledgeActionView[T] {
	out := miniAppKnowledgeActionView[T]{
		OperationID: action.OperationID,
		Status:      action.State,
		ObservedAt:  action.ObservedAt,
	}
	if action.Preparation != nil && (action.State == agentPrepared || action.State == knowledgeActionExpired) {
		prep := action.Preparation
		out.Preparation = &miniAppKnowledgePreparationView[T]{
			Arguments: fromChange(prep.Arguments),
			Preconditions: assignmentPreconditions{
				ProposalID: prep.ID,
			},
			Description: prep.Description,
			ExpiresAt:   prep.ExpiresAt,
		}
	}
	if action.Result != nil {
		result := knowledgeResultProject(*action.Result)
		out.Result = &result
	}
	return out
}

// Pointer fields distinguish an explicit revocation/version zero from absent
// or null consent. Other native argument defaults retain their existing rules.
type miniAppKnowledgeModeratorArguments struct {
	ModeratorUserID          uuid.UUID `json:"moderator_user_id"`
	ModeratorEnabled         *bool     `json:"moderator_enabled"`
	ExpectedModeratorVersion *int64    `json:"expected_moderator_version"`
	Reason                   string    `json:"reason"`
}

func miniAppKnowledgeWriteChange[T any](args T, toChange func(T) db.KnowledgeChange) (db.KnowledgeChange, error) {
	if moderator, ok := any(args).(miniAppKnowledgeModeratorArguments); ok &&
		(moderator.ModeratorEnabled == nil || moderator.ExpectedModeratorVersion == nil) {
		return db.KnowledgeChange{}, knowledge.ErrInvalid
	}
	return toChange(args), nil
}

func miniAppKnowledgeModeratorChange(args miniAppKnowledgeModeratorArguments) db.KnowledgeChange {
	out := db.KnowledgeChange{
		ModeratorUserID:  args.ModeratorUserID,
		ModeratorEnabled: args.ModeratorEnabled,
		Reason:           args.Reason,
	}
	if args.ExpectedModeratorVersion != nil {
		out.ExpectedModeratorVersion = *args.ExpectedModeratorVersion
	}
	return out
}
func miniAppKnowledgeModeratorFromChange(change db.KnowledgeChange) miniAppKnowledgeModeratorArguments {
	args := knowledgeModeratorFromChange(change)
	return miniAppKnowledgeModeratorArguments{
		ModeratorUserID:          args.ModeratorUserID,
		ModeratorEnabled:         &args.ModeratorEnabled,
		ExpectedModeratorVersion: &args.ExpectedModeratorVersion,
		Reason:                   args.Reason,
	}
}
