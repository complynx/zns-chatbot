package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// AttachMemorySources crosses the service boundary; the bot's database role
// never reads core memory records or writes their provenance tables directly.
func (c APIClient) AttachMemorySources(ctx context.Context, owner, operationKey string, updateID int64) error {
	if _, err := c.userToken(ctx, owner); err != nil {
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

// ArchiveConversation preserves privacy filtering in Core without granting the
// bot database role access to private history or knowledge storage.
func (c APIClient) ArchiveConversation(
	ctx context.Context,
	owner, key, kind, text string,
	replyTo int64,
	media bool,
) error {
	return c.archiveConversation(ctx, owner, key, kind, text, replyTo, media, nil)
}

func (c APIClient) archiveConversation(
	ctx context.Context,
	owner, key, kind, text string,
	replyTo int64,
	media bool,
	generation *int64,
) error {
	if _, err := c.userToken(ctx, owner); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		ExpectedGeneration *int64 `json:"expected_generation,omitempty"`
		SourceKey          string `json:"source_key"`
		Kind               string `json:"kind"`
		Text               string `json:"text"`
		ReplyToUpdateID    int64  `json:"reply_to_update_id"`
		Media              bool   `json:"media"`
	}{generation, key, kind, text, replyTo, media})
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
		"/internal/history/archive",
		data,
		&result,
	)
}

// AssessMemoryProposal carries only the trusted classifier verdict. It cannot
// publish a fact or assign permissions, and owner is bound by the host principal.
func (c APIClient) AssessMemoryProposal(
	ctx context.Context,
	owner string,
	input knowledge.Assessment,
) (knowledge.Result, error) {
	if _, err := c.userToken(ctx, owner); err != nil {
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
	err = c.requestToken(
		ctx,
		c.Signer.MemoryProvenanceToken(owner),
		http.MethodPost,
		"/internal/memory/assess",
		data,
		&result,
	)
	return result, err
}

func (c APIClient) HistorySummaryBatch(ctx context.Context, owner string, before int64) ([]conversation.Event, error) {
	if _, err := c.userToken(ctx, owner); err != nil {
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
	return events, err
}

func (c APIClient) CommitHistorySummary(
	ctx context.Context,
	owner string,
	version int64,
	ids []int64,
	text string,
) error {
	if _, err := c.userToken(ctx, owner); err != nil {
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
