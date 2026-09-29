package appclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// AttachMemorySources crosses the service boundary; the bot's database role
// never reads core memory records or writes their provenance tables directly.
func (c Host) AttachMemorySources(ctx context.Context, owner, operationKey string, updateID int64) error {
	const maxOperationKey = 200
	if updateID <= 0 || operationKey == "" || len(operationKey) > maxOperationKey {
		return &core.ProblemError{Status: http.StatusBadRequest, Code: invalidJSONCode}
	}
	if c.LocalKnowledge != nil {
		_, err := hostKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (bool, error) {
			err := s.AttachMemorySources(
				ctx,
				actor,
				operationKey,
				[]string{"tg-user-" + strconv.FormatInt(updateID, 10)},
			)
			return err == nil, err
		})
		return err
	}
	if err := c.validateUser(ctx, owner); err != nil {
		return err
	}
	input := struct {
		OperationKey string `json:"operation_key"`
		UpdateID     int64  `json:"update_id"`
	}{operationKey, updateID}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.MemoryProvenanceToken(owner),
		http.MethodPost,
		"/internal/memory/sources",
		data,
		&result,
	)
}

func (c Host) ArchiveOriginal(ctx context.Context, owner, key, kind, text string) error {
	if c.LocalHistory != nil {
		return hostHistoryWrite(ctx, c, owner, func(s conversation.Service, actor string) error {
			return s.ArchiveOriginal(ctx, actor, key, kind, text)
		})
	}
	return c.archiveRequest(
		ctx,
		owner,
		"original",
		conversation.OriginalArchive{SourceKey: key, Kind: kind, Text: text},
	)
}

func (c Host) ArchiveOutcome(ctx context.Context, owner, key, text string) error {
	if c.LocalHistory != nil {
		return hostHistoryWrite(ctx, c, owner, func(s conversation.Service, actor string) error {
			return s.ArchiveOutcome(ctx, actor, key, text)
		})
	}
	return c.archiveRequest(ctx, owner, "outcome", conversation.OutcomeArchive{SourceKey: key, Text: text})
}

func (c Host) ArchiveDerived(
	ctx context.Context,
	owner, key, text string,
	replyTo int64,
	media bool,
	generation int64,
	authorities []readsource.Authority,
) error {
	input := conversation.DerivedArchive{SourceKey: key, Text: text, ReplyToUpdateID: replyTo, Media: media,
		ExpectedGeneration: &generation, ReadAuthorities: authorities}
	if c.LocalHistory != nil {
		return hostHistoryWrite(ctx, c, owner, func(s conversation.Service, actor string) error {
			return s.ArchiveDerived(ctx, actor, input)
		})
	}
	return c.archiveRequest(ctx, owner, "derived", input)
}

func (c Host) archiveRequest(ctx context.Context, owner, route string, input interface{ Validate() error }) error {
	if err := c.validateUser(ctx, owner); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.MemoryProvenanceToken(owner),
		http.MethodPost,
		"/internal/history/archive/"+route,
		data,
		&result,
	)
}

// AssessMemoryProposal carries only the trusted classifier verdict. It cannot
// publish a fact or assign permissions, and owner is bound by the host principal.
func (c Host) AssessMemoryProposal(
	ctx context.Context,
	owner string,
	input knowledge.Assessment,
) (knowledge.Result, error) {
	if c.LocalKnowledge != nil {
		return hostKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Result, error) {
			return s.Assess(ctx, actor, input)
		})
	}

	if err := c.validateUser(ctx, owner); err != nil {
		return knowledge.Result{}, err
	}
	data, err := json.Marshal(struct {
		Key        string `json:"key"`
		ProposalID int64  `json:"proposal_id"`
		Version    int64  `json:"version"`
		Worthwhile bool   `json:"worthwhile"`
		Reason     string `json:"reason"`
	}{input.Key, input.ProposalID, input.Version, input.Worthwhile, input.Reason})
	if err != nil {
		return knowledge.Result{}, err
	}
	var result knowledge.Result
	err = requestAuthorizedLimit(
		ctx,
		c.Base, c.HTTP,
		c.Signer.MemoryProvenanceToken(owner),
		"",
		http.MethodPost,
		"/internal/memory/assess",
		data,
		&knowledgeEnvelope{&result}, knowledgeResponseBytes,
	)
	result.RestoreReadAuthorities()
	return result, err
}

func (c Host) HistorySummaryBatch(ctx context.Context, owner string, before int64) ([]conversation.Event, error) {
	if c.LocalHistory != nil {
		return hostHistory(ctx, c, owner, func(s conversation.Service, actor string) ([]conversation.Event, error) {
			return s.SummaryBatch(ctx, actor, before)
		})
	}
	if err := c.validateUser(ctx, owner); err != nil {
		return nil, err
	}
	var events []conversation.Event
	err := c.requestToken(
		ctx,
		c.Signer.MemoryProvenanceToken(owner),
		http.MethodGet,
		"/internal/history/summary-batch?before="+strconv.FormatInt(before, 10),
		nil,
		&events,
	)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (c Host) CommitHistorySummary(
	ctx context.Context,
	owner string,
	version int64,
	ids []int64,
	text string,
) error {
	if c.LocalHistory != nil {
		return hostHistoryWrite(ctx, c, owner, func(s conversation.Service, actor string) error {
			return s.CommitSummary(ctx, actor, version, ids, text)
		})
	}
	if err := c.validateUser(ctx, owner); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Version int64   `json:"version"`
		IDs     []int64 `json:"ids"`
		Text    string  `json:"text"`
	}{version, ids, text})
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.MemoryProvenanceToken(owner),
		http.MethodPost,
		"/internal/history/summary",
		data,
		&result,
	)
}
