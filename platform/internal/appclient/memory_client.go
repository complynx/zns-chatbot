package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const memoryReferenceQuery = "ref"

func (c Client) MemorySummary(
	ctx context.Context,
	owner string,
	query knowledge.MemoryQuery,
) (knowledge.MemoryOverview, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(
			ctx,
			c,
			owner,
			func(s knowledge.Service, actor string) (knowledge.MemoryOverview, error) {
				return s.MemorySummary(ctx, actor, query)
			},
		)
	}

	var result knowledge.MemoryOverview
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/summary?"+memoryQueryValues(query).Encode(),
		nil,
		&result,
	)
	return result, err
}

func (c Client) MemorySearch(
	ctx context.Context,
	owner string,
	query knowledge.MemoryQuery,
) (knowledge.MemoryPage, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.MemoryPage, error) {
			return s.SearchMemory(ctx, actor, query)
		})
	}
	var result knowledge.MemoryPage
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/search?"+memoryQueryValues(query).Encode(),
		nil,
		&result,
	)
	return result, err
}

func (c Client) MemoryEntry(
	ctx context.Context,
	owner string,
	reference, cursor string,
) (knowledge.MemoryEntry, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.MemoryEntry, error) {
			return s.ReadMemoryPage(ctx, actor, reference, cursor)
		})
	}
	var result knowledge.MemoryEntry
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/read?"+url.Values{memoryReferenceQuery: {reference}, memoryCursorQuery: {cursor}}.Encode(),
		nil,
		&result,
	)
	return result, err
}

func (c Client) MemoryRevision(
	ctx context.Context,
	owner string,
	reference, cursor string,
) (knowledge.MemoryEntry, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.MemoryEntry, error) {
			return s.ReadMemoryRevisionPage(ctx, actor, reference, cursor)
		})
	}
	var result knowledge.MemoryEntry
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/revision?"+url.Values{memoryReferenceQuery: {reference}, memoryCursorQuery: {cursor}}.Encode(),
		nil,
		&result,
	)
	return result, err
}

func (c Client) MemoryHistory(
	ctx context.Context,
	owner string,
	reference, cursor string,
) (knowledge.MemoryPage, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.MemoryPage, error) {
			return s.MemoryHistory(ctx, actor, reference, cursor)
		})
	}
	var result knowledge.MemoryPage
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/history?"+url.Values{memoryReferenceQuery: {reference}, memoryCursorQuery: {cursor}}.Encode(),
		nil,
		&result,
	)
	return result, err
}
func (c Client) MemorySources(ctx context.Context, owner string, reference string) (conversation.Page, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (conversation.Page, error) {
			return s.MemorySources(ctx, actor, reference)
		})
	}
	var result conversation.Page
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/sources?"+url.Values{memoryReferenceQuery: {reference}}.Encode(),
		nil,
		&result,
	)
	return result, err
}
func (c Client) MemoryDocument(ctx context.Context, owner string, topic, key string) (knowledge.Document, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Document, error) {
			return s.DocumentState(ctx, actor, topic, key)
		})
	}
	var result knowledge.Document
	err := c.knowledgeCall(
		ctx,
		owner,
		http.MethodGet,
		"/v1/memory/document?"+url.Values{"topic": {topic}, "key": {key}}.Encode(),
		nil,
		&result,
	)
	return result, err
}
