package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

// Only exact article and authorized suggestion reads include complete Markdown.
// Lists return bounded projections, never a page of complete article bodies.
func (s *Server) registerMiniAppKnowledgeReads() {
	registerMiniAppKnowledgePost(
		s,
		"overview",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, _ struct{}) (any, error) {
			return s.store.KnowledgeOverview(ctx, scope)
		},
	)
	registerMiniAppKnowledgePost(
		s,
		"capabilities",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledgeCapabilitiesArguments) (any, error) {
			return s.store.KnowledgeCapabilities(ctx, scope, in.EventID)
		},
	)
	registerMiniAppKnowledgePost(
		s,
		"moderators",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledgeModeratorListArguments) (any, error) {
			return s.store.KnowledgeModerators(ctx, scope, in.After, in.Limit)
		},
	)
	registerMiniAppKnowledgePost(
		s,
		"list",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in db.KnowledgeListRequest) (any, error) {
			result, err := s.store.ListKnowledge(ctx, scope, in)
			if err != nil {
				return nil, err
			}
			out := knowledgeListView{
				Items:     make([]knowledgeArticleView, 0, len(result.Items)),
				Next:      result.Next,
				Filters:   in,
				Truncated: result.Next != nil,
			}
			for _, article := range result.Items {
				out.Items = append(out.Items, knowledgeProject(article, true))
			}
			return out, nil
		},
	)
	registerMiniAppKnowledgePost(
		s,
		"search",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledge.SearchRequest) (any, error) {
			normalized, err := in.Normalize()
			if err != nil {
				return nil, err
			}
			result, err := s.store.SearchKnowledge(ctx, scope, normalized)
			if err != nil {
				return nil, err
			}
			out := knowledgeSearchView{
				Items:     make([]knowledgeArticleView, 0, len(result.Items)),
				Next:      result.Next,
				Filters:   normalized,
				Truncated: result.Next != "",
			}
			for _, hit := range result.Items {
				view := knowledgeProject(hit.Article, true)
				view.MatchedQueries = hit.MatchedQueries
				knowledgeSearchExcerpt(&view, hit.Article.Content.Body, hit.MatchedQueries)
				out.Items = append(out.Items, view)
			}
			return out, nil
		},
	)
	registerMiniAppKnowledgePost(
		s,
		"read",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledgeArticleArguments) (any, error) {
			article, err := s.store.ReadKnowledge(ctx, scope, in.ArticleID, in.Revision)
			if err != nil {
				return nil, err
			}
			return struct {
				Article knowledgeArticleView `json:"article"`
				Body    string               `json:"body"`
			}{Article: knowledgeProject(article, false), Body: article.Content.Body}, nil
		},
	)
	registerMiniAppKnowledgePost(
		s,
		"revisions",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledgeRevisionArguments) (any, error) {
			return s.store.KnowledgeRevisions(ctx, scope, in.ArticleID, in.Before, in.Limit)
		},
	)
	s.registerMiniAppKnowledgeSuggestions()
}

func (s *Server) registerMiniAppKnowledgeSuggestions() {
	registerMiniAppKnowledgePost(
		s,
		"suggestion",
		func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledgeSuggestionDetailInput) (any, error) {
			suggestion, err := s.store.ReadKnowledgeSuggestion(ctx, scope, in.SuggestionID)
			if err != nil {
				return nil, err
			}
			return struct {
				Suggestion knowledgeSuggestionView `json:"suggestion"`
				Body       string                  `json:"body"`
			}{Suggestion: knowledgeSuggestionProject(suggestion, false), Body: suggestion.Content.Body}, nil
		},
	)
	for _, pending := range []bool{false, true} {
		path := "my_suggestions"
		if pending {
			path = "pending_suggestions"
		}
		registerMiniAppKnowledgePost(
			s,
			path,
			func(ctx context.Context, scope db.PaymentScope, _ db.MiniAppKnowledgeSession, in knowledgeSuggestionListArguments) (any, error) {
				filters := db.KnowledgeSuggestionRequest{
					Pending: pending,
					After:   in.After,
					Limit:   in.Limit,
					EventID: in.EventID,
				}
				result, err := s.store.KnowledgeSuggestions(ctx, scope, filters)
				if err != nil {
					return nil, err
				}
				out := knowledgeSuggestionListView{
					Items:     make([]knowledgeSuggestionView, 0, len(result.Items)),
					Next:      result.Next,
					Filters:   filters,
					Truncated: result.Next != nil,
				}
				for _, suggestion := range result.Items {
					out.Items = append(out.Items, knowledgeSuggestionProject(suggestion, true))
				}
				return out, nil
			},
		)
	}
}

type knowledgeSuggestionDetailInput struct {
	SuggestionID uuid.UUID `json:"suggestion_id"`
}
