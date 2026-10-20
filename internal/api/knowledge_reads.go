package api

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func knowledgeError(err error) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}
	switch {
	case errors.Is(err, db.ErrKnowledgeDenied):
		return huma.Error403Forbidden("current knowledge authority required")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("knowledge article, revision or suggestion not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("knowledge revision, lifecycle, proposal or operation changed")
	case errors.Is(err, knowledge.ErrInvalid):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error503ServiceUnavailable(
			"knowledge storage unavailable; reconcile the same operation after uncertainty",
		)
	}
}

// Metadata is bounded independently of the source body. Offsets always refer to
// the immutable revision returned here, including when revision 0 chose current.
func knowledgeProject(a db.KnowledgeArticle, compact bool) knowledgeArticleView {
	collection := knowledge.General
	if a.EventID != nil && !a.Historical {
		collection = knowledge.Event
	}
	out := knowledgeArticleView{
		ID:              a.ID,
		Revision:        a.Revision,
		CurrentRevision: a.CurrentRevision,
		OlderRevision:   a.CurrentRevision > a.Revision,
		Slug:            a.Content.Slug,
		Title:           a.Content.Title,
		Language:        a.Content.Language,
		Topics:          a.Content.Topics,
		Sources:         a.Content.Sources,
		Collection:      collection,
		EventID:         a.EventID,
		EventName:       a.EventName,
		EventStartsAt:   a.EventStartsAt,
		EventEndsAt:     a.EventEndsAt,
		EventTimezone:   a.EventTimezone,
		ArchivedAt:      a.ArchivedAt,
		Historical:      a.Historical,
		UpdatedAt:       a.UpdatedAt,
	}
	if compact {
		out.MetadataTruncated = len(out.Topics) > knowledgePreviewTopics || len(out.Sources) > knowledgePreviewSources
		out.Topics = out.Topics[:min(knowledgePreviewTopics, len(out.Topics))]
		out.Sources = out.Sources[:min(knowledgePreviewSources, len(out.Sources))]
	}
	out.Excerpt, _ = knowledge.Read(
		a.Content.Body,
		knowledge.ReadRequest{MaxBytes: knowledgePreviewBytes, LineCount: knowledgePreviewLines},
	)
	out.ExcerptKind = "lead"
	out.Outline, _ = knowledge.Outline(a.Content.Body, knowledge.OutlineRequest{Limit: knowledgePreviewHeadings})
	return out
}
func knowledgeSuggestionProject(a db.KnowledgeSuggestion, compact bool) knowledgeSuggestionView {
	view := knowledgeProject(db.KnowledgeArticle{Content: a.Content}, compact)
	return knowledgeSuggestionView{
		ID:                a.ID,
		ArticleID:         a.ArticleID,
		BaseRevision:      a.BaseRevision,
		EventID:           a.EventID,
		EventVersion:      a.EventVersion,
		ProposerID:        a.ProposerID,
		Reason:            a.Reason,
		State:             a.State,
		ReviewerID:        a.ReviewerID,
		DecisionReason:    a.DecisionReason,
		PublishedRevision: a.PublishedRevision,
		CreatedAt:         a.CreatedAt,
		DecidedAt:         a.DecidedAt,
		Content: knowledgeSuggestionContentView{
			Slug:              view.Slug,
			Title:             view.Title,
			Language:          view.Language,
			Topics:            view.Topics,
			Sources:           view.Sources,
			MetadataTruncated: view.MetadataTruncated,
			Excerpt:           view.Excerpt,
			Outline:           view.Outline,
		},
	}
}
func knowledgeResultProject(result db.KnowledgeResult) knowledgeResultView {
	out := knowledgeResultView{Kind: result.Kind, Moderator: result.Moderator}
	if result.Article != nil {
		a := knowledgeProject(*result.Article, true)
		out.Article = &a
	}
	if result.Suggestion != nil {
		a := knowledgeSuggestionProject(*result.Suggestion, true)
		out.Suggestion = &a
	}
	return out
}

func (s *Server) knowledgeOverview(
	ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[db.KnowledgeOverview], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.KnowledgeOverview(ctx, scope)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) knowledgeList(
	ctx context.Context,
	in *agentReadRequest[db.KnowledgeListRequest],
) (*agentReadResponse[knowledgeListView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	normalized, err := (knowledge.SearchRequest{Collection: args.Collection, EventID: args.EventID, Language: args.Language, Topic: args.Topic, Limit: args.Limit, Queries: []string{"browse"}}).Normalize()
	if err != nil {
		return nil, knowledgeError(err)
	}
	args.Collection, args.Language, args.Topic, args.Limit = normalized.Collection, normalized.Language, normalized.Topic, normalized.Limit
	result, err := s.store.ListKnowledge(ctx, scope, args)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := knowledgeListView{
		Items:     make([]knowledgeArticleView, 0, len(result.Items)),
		Next:      result.Next,
		Filters:   args,
		Truncated: result.Next != nil,
	}
	for _, a := range result.Items {
		out.Items = append(out.Items, knowledgeProject(a, true))
	}
	return agentReadResult(in.Body.OperationID, out), nil
}

func (s *Server) knowledgeSearch(
	ctx context.Context,
	in *agentReadRequest[knowledge.SearchRequest],
) (*agentReadResponse[knowledgeSearchView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args, err := in.Body.Arguments.Normalize()
	if err != nil {
		return nil, knowledgeError(err)
	}
	result, err := s.store.SearchKnowledge(ctx, scope, args)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := knowledgeSearchView{
		Items:     make([]knowledgeArticleView, 0, len(result.Items)),
		Next:      result.Next,
		Filters:   args,
		Truncated: result.Next != "",
	}
	for _, a := range result.Items {
		view := knowledgeProject(a.Article, true)
		view.MatchedQueries = a.MatchedQueries
		knowledgeSearchExcerpt(&view, a.Article.Content.Body, a.MatchedQueries)
		out.Items = append(out.Items, view)
	}
	return agentReadResult(in.Body.OperationID, out), nil
}

// Full-text morphology can match without a literal source occurrence. Such hits
// keep an explicitly labelled lead rather than inventing a matching passage.
func knowledgeSearchExcerpt(view *knowledgeArticleView, body string, queries []string) {
	for _, query := range queries {
		terms := append([]string{query}, strings.Fields(query)...)
		for _, term := range terms {
			found, err := knowledge.Find(
				body,
				knowledge.FindRequest{Query: term, Limit: 1, ContextBytes: knowledgePreviewContextBytes},
			)
			if err != nil || len(found.Matches) == 0 {
				continue
			}
			start := found.Matches[0].StartByte
			view.Excerpt, _ = knowledge.Read(
				body,
				knowledge.ReadRequest{
					StartByte: &start,
					MaxBytes:  knowledgePreviewBytes,
					LineCount: knowledgePreviewLines,
				},
			)
			view.ExcerptKind = "literal_match"
			view.ExcerptAnchor = found.Matches[0].Anchor
			return
		}
	}
}

func (s *Server) knowledgeRead(
	ctx context.Context,
	in *agentReadRequest[knowledgeReadArguments],
) (*agentReadResponse[knowledgeArticleView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	a, err := s.store.ReadKnowledge(ctx, scope, args.ArticleID, args.Revision)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := knowledgeProject(a, false)
	out.Excerpt, err = knowledge.Read(a.Content.Body, args.ReadRequest)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out.ExcerptKind = "selected_passage"
	out.ExcerptAnchor = args.Anchor
	out.Outline, err = knowledge.Outline(a.Content.Body, knowledge.OutlineRequest{})
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, out), nil
}

func (s *Server) knowledgeOutline(
	ctx context.Context,
	in *agentReadRequest[knowledgeOutlineArguments],
) (*agentReadResponse[knowledgeArticleView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	a, err := s.store.ReadKnowledge(ctx, scope, args.ArticleID, args.Revision)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := knowledgeProject(a, false)
	out.Outline, err = knowledge.Outline(a.Content.Body, args.OutlineRequest)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, out), nil
}

func (s *Server) knowledgeFind(
	ctx context.Context,
	in *agentReadRequest[knowledgeFindArguments],
) (*agentReadResponse[knowledgeFindView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	a, err := s.store.ReadKnowledge(ctx, scope, args.ArticleID, args.Revision)
	if err != nil {
		return nil, knowledgeError(err)
	}
	matches, err := knowledge.Find(a.Content.Body, args.FindRequest)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(
		in.Body.OperationID,
		knowledgeFindView{Article: knowledgeProject(a, false), Matches: matches},
	), nil
}

func (s *Server) knowledgeRevisions(
	ctx context.Context,
	in *agentReadRequest[knowledgeRevisionArguments],
) (*agentReadResponse[db.KnowledgeRevisionPage], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	result, err := s.store.KnowledgeRevisions(ctx, scope, args.ArticleID, args.Before, args.Limit)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) knowledgeSuggestion(
	ctx context.Context,
	in *agentReadRequest[knowledgeSuggestionArguments],
) (*agentReadResponse[knowledgeSuggestionView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	result, err := s.store.ReadKnowledgeSuggestion(ctx, scope, args.SuggestionID)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := knowledgeSuggestionProject(result, false)
	out.Content.Excerpt, err = knowledge.Read(result.Content.Body, args.ReadRequest)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out.Content.Outline, err = knowledge.Outline(result.Content.Body, knowledge.OutlineRequest{})
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, out), nil
}

func (s *Server) knowledgeSuggestions(
	ctx context.Context,
	in *agentReadRequest[knowledgeSuggestionListArguments],
	pending bool,
) (*agentReadResponse[knowledgeSuggestionListView], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = knowledge.DefaultLimit
	}
	if args.Limit < 1 || args.Limit > knowledge.MaxLimit {
		return nil, huma.Error400BadRequest("limit must be 1..20")
	}
	filters := db.KnowledgeSuggestionRequest{
		Pending: pending,
		EventID: args.EventID,
		After:   args.After,
		Limit:   args.Limit,
	}
	result, err := s.store.KnowledgeSuggestions(ctx, scope, filters)
	if err != nil {
		return nil, knowledgeError(err)
	}
	out := knowledgeSuggestionListView{
		Items:     make([]knowledgeSuggestionView, 0, len(result.Items)),
		Next:      result.Next,
		Filters:   filters,
		Truncated: result.Next != nil,
	}
	for _, a := range result.Items {
		out.Items = append(out.Items, knowledgeSuggestionProject(a, true))
	}
	return agentReadResult(in.Body.OperationID, out), nil
}

func (s *Server) knowledgeCapabilities(
	ctx context.Context,
	in *agentReadRequest[knowledgeCapabilitiesArguments],
) (*agentReadResponse[db.KnowledgeCapabilities], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.KnowledgeCapabilities(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) knowledgeModerators(
	ctx context.Context,
	in *agentReadRequest[knowledgeModeratorListArguments],
) (*agentReadResponse[db.KnowledgeModeratorPage], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = 10
	}
	result, err := s.store.KnowledgeModerators(ctx, scope, args.After, args.Limit)
	if err != nil {
		return nil, knowledgeError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
