package db

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func kbRunGuard(ctx context.Context, q *sqlc.Queries, guard func(context.Context, *sqlc.Queries) error) error {
	if guard == nil {
		return nil
	}
	return guard(ctx, q)
}

func kbReplayPreparation(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	prior sqlc.ShuttleKnowledgePreparation,
	hash []byte,
	guard func(context.Context, *sqlc.Queries) error,
) (KnowledgePreparation, error) {
	if err := kbActor(ctx, q, scope); err != nil {
		return KnowledgePreparation{}, err
	}
	if err := kbRunGuard(ctx, q, guard); err != nil {
		return KnowledgePreparation{}, err
	}
	if prior.ActorID != scope.UserID || !bytes.Equal(prior.RequestHash, hash) {
		return KnowledgePreparation{}, ErrConflict
	}
	var doc kbPreparationDocument
	err := json.Unmarshal(prior.Document, &doc)
	return doc.Preparation, err
}

func kbReplayResult(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	prior sqlc.ShuttleKnowledgeOperation,
	hash []byte,
	proposal uuid.UUID,
	guard func(context.Context, *sqlc.Queries) error,
) (KnowledgeResult, error) {
	if err := kbActor(ctx, q, scope); err != nil {
		return KnowledgeResult{}, err
	}
	if err := kbRunGuard(ctx, q, guard); err != nil {
		return KnowledgeResult{}, err
	}
	if prior.ActorID != scope.UserID || prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return KnowledgeResult{}, ErrConflict
	}
	var result KnowledgeResult
	err := json.Unmarshal(prior.Result, &result)
	return result, err
}
