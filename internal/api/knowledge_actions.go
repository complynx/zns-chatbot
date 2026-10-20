package api

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const (
	knowledgeEditKind    = "edit"
	knowledgeSuggestKind = "suggest"
)

type knowledgeOperationArguments struct {
	Kind        string `json:"kind"         enum:"suggest,edit,review,set_moderator"`
	OperationID string `json:"operation_id"                                          format:"uuid"`
}

// The preparation contains exactly the original verb's normalized arguments.
// Its schema describes the same three shapes that its JSON marshaler emits.
type knowledgeActionArguments struct{ value any }

func (a knowledgeActionArguments) MarshalJSON() ([]byte, error) { return json.Marshal(a.value) }
func (knowledgeActionArguments) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{AnyOf: []*huma.Schema{
		huma.SchemaFromType(r, reflect.TypeFor[knowledgeEditArguments]()),
		huma.SchemaFromType(r, reflect.TypeFor[knowledgeReviewArguments]()),
		huma.SchemaFromType(r, reflect.TypeFor[knowledgeModeratorArguments]()),
	}}
}

type knowledgeActionPreparation struct {
	ID          uuid.UUID                `json:"proposal_id"`
	Arguments   knowledgeActionArguments `json:"arguments"`
	Description string                   `json:"description"`
	ExpiresAt   time.Time                `json:"expires_at"`
}

type knowledgeActionView struct {
	Kind        string                      `json:"kind"                  enum:"suggest,edit,review,set_moderator"`
	OperationID string                      `json:"operation_id"`
	State       string                      `json:"state"                 enum:"completed,prepared,expired,unknown"`
	Preparation *knowledgeActionPreparation `json:"preparation,omitempty"`
	Result      *knowledgeResultView        `json:"result,omitempty"`
	ObservedAt  time.Time                   `json:"observed_at"`
}

type knowledgeRecentActionsView struct {
	Items []db.KnowledgeRecentAction `json:"items" maxItems:"5"`
}

func knowledgeActionProject(action db.KnowledgeActionView) knowledgeActionView {
	out := knowledgeActionView{Kind: action.Kind, OperationID: action.OperationID,
		State: action.State, ObservedAt: action.ObservedAt}
	if action.Preparation != nil {
		prep := action.Preparation
		args := knowledgeActionArguments{}
		switch action.Kind {
		case knowledgeSuggestKind, knowledgeEditKind:
			args.value = knowledgeEditFromChange(prep.Arguments)
		case paymentSchemaReview:
			args.value = knowledgeReviewFromChange(prep.Arguments)
		case "set_moderator":
			args.value = knowledgeModeratorFromChange(prep.Arguments)
		}
		out.Preparation = &knowledgeActionPreparation{ID: prep.ID, Arguments: args,
			Description: prep.Description, ExpiresAt: prep.ExpiresAt}
	}
	if action.Result != nil {
		result := knowledgeResultProject(*action.Result)
		out.Result = &result
	}
	return out
}

func (s *Server) registerKnowledgeActions(api huma.API) {
	registerAgentPost(api, "agent-knowledge-operation", knowledgePath+"operation",
		internalBodyLimit, s.knowledgeAction)
	registerAgentPost(api, "agent-knowledge-recent-actions", knowledgePath+"recent_actions",
		internalBodyLimit, s.knowledgeRecentActions)
}

func (s *Server) knowledgeAction(ctx context.Context,
	in *agentReadRequest[knowledgeOperationArguments],
) (*agentReadResponse[knowledgeActionView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if err = knowledgeOperationID(args.OperationID); err != nil {
		return nil, err
	}
	action, err := s.store.KnowledgeAction(ctx, scope, args.Kind, args.OperationID)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, knowledgeActionProject(action)), nil
}

func (s *Server) knowledgeRecentActions(ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[knowledgeRecentActionsView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	actions, err := s.store.RecentKnowledgeActions(ctx, scope, time.Time{})
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, knowledgeRecentActionsView{Items: actions}), nil
}

func (s *Server) registerMiniAppKnowledgeActions() {
	registerMiniAppKnowledgePost(s, "recent_actions",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession,
			_ struct{},
		) (any, error) {
			actions, err := s.store.RecentKnowledgeActions(ctx, scope, time.Time{})
			return knowledgeRecentActionsView{Items: actions}, err
		})
}

func knowledgeActionMethods() []map[string]any {
	return []map[string]any{
		knowledgeMethod[knowledgeOperationArguments, knowledgeActionView](
			"operation",
			"Inspect your own original action using kind and operation_id. completed returns a bounded original receipt, not current article state. prepared/expired include exact original normalized verb arguments and proposal_id; this past consent is not current permission or a new request. To continue a prepared action, use its original operation_id and unchanged arguments and obtain fresh exact consent; confirmation still rechecks expiry, rights and versions. expired needs fresh preparation and consent. unknown is nonterminal, never proof a replacement operation is safe. Never mutate merely because history exists.",
		),
		knowledgeMethod[struct{}, knowledgeRecentActionsView](
			"recent_actions",
			"Read at most five recent actor-owned action locators, without content bodies. This is a bounded continuation hint, not an exhaustive directory or paginated history. Use operation with a known kind/operation_id for exact original terms and result. Observed states may have different timestamps. Historical actions are not current requests, consent or permission; continue only the user's explicit request.",
		),
	}
}
