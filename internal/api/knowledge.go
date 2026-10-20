package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerKnowledge(api huma.API) {
	s.registerKnowledgeActions(api)
	registerAgentPost(api, "agent-knowledge-discover", knowledgePath+"discover", internalBodyLimit, s.discoverKnowledge)
	registerAgentPost(api, "agent-knowledge-overview", knowledgePath+"overview", internalBodyLimit, s.knowledgeOverview)
	registerAgentPost(api, "agent-knowledge-list", knowledgePath+"list", internalBodyLimit, s.knowledgeList)
	registerAgentPost(api, "agent-knowledge-search", knowledgePath+"search", internalBodyLimit, s.knowledgeSearch)
	registerAgentPost(api, "agent-knowledge-read", knowledgePath+"read", internalBodyLimit, s.knowledgeRead)
	registerAgentPost(api, "agent-knowledge-outline", knowledgePath+"outline", internalBodyLimit, s.knowledgeOutline)
	registerAgentPost(api, "agent-knowledge-find", knowledgePath+"find", internalBodyLimit, s.knowledgeFind)
	registerAgentPost(
		api,
		"agent-knowledge-revisions",
		knowledgePath+"revisions",
		internalBodyLimit,
		s.knowledgeRevisions,
	)
	registerAgentPost(
		api,
		"agent-knowledge-suggestion",
		knowledgePath+"suggestion",
		internalBodyLimit,
		s.knowledgeSuggestion,
	)
	registerAgentPost(
		api,
		"agent-knowledge-capabilities",
		knowledgePath+"capabilities",
		internalBodyLimit,
		s.knowledgeCapabilities,
	)
	registerAgentPost(
		api,
		"agent-knowledge-moderators",
		knowledgePath+"moderators",
		internalBodyLimit,
		s.knowledgeModerators,
	)
	for _, pending := range []bool{false, true} {
		name := "my_suggestions"
		if pending {
			name = "pending_suggestions"
		}
		registerAgentPost(
			api,
			"agent-knowledge-"+name,
			knowledgePath+name,
			internalBodyLimit,
			func(ctx context.Context, in *agentReadRequest[knowledgeSuggestionListArguments]) (*agentReadResponse[knowledgeSuggestionListView], error) {
				return s.knowledgeSuggestions(ctx, in, pending)
			},
		)
	}
	registerKnowledgeWrite(api, s, "suggest", knowledgeEditChange, knowledgeEditFromChange)
	registerKnowledgeWrite(api, s, "edit", knowledgeEditChange, knowledgeEditFromChange)
	registerKnowledgeWrite(api, s, "review", knowledgeReviewChange, knowledgeReviewFromChange)
	registerKnowledgeWrite(api, s, "set_moderator", knowledgeModeratorChange, knowledgeModeratorFromChange)
}
func (s *Server) discoverKnowledge(ctx context.Context, in *AuthInput) (*agentDiscoveryResponse, error) {
	scope, err := s.paymentScope(ctx, *in)
	if err != nil {
		return nil, err
	}
	capabilities, err := s.store.KnowledgeCapabilities(ctx, scope, nil)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return discoveryResult(knowledgeMethods(capabilities))
}
