package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// Older preparations embed consent in document. New preparations store the full
// immutable text separately so JSON escaping cannot overflow transport receipts.
func decodeOrderReviewPreparation(raw []byte, presentation string) (OrderEvidenceReviewPreparation, error) {
	var result OrderEvidenceReviewPreparation
	err := json.Unmarshal(raw, &result)
	if err == nil && presentation != "" {
		result.Description = presentation
	}
	return result, err
}

func compactOrderReviewDocument(document any) ([]byte, error) {
	switch value := document.(type) {
	case OrderReviewWorkspace:
		value.Preparation.Description = ""
		document = value
	case OrderReviewCommitPreparation:
		value.Description = ""
		document = value
	}
	return orderJSON(document)
}

// Restore only consent, leaving the historical receipt's state and revision intact.
func hydrateOrderReviewWorkspace(
	ctx context.Context,
	q *sqlc.Queries,
	actor uuid.UUID,
	snapshot *OrderReviewWorkspace,
) error {
	if snapshot.Preparation.Description != "" {
		return nil
	}
	canonical, err := readOrderReviewWorkspace(ctx, q, actor, snapshot.ID)
	if err != nil {
		return err
	}
	if snapshot.Preparation.ID != canonical.Preparation.ID ||
		snapshot.Preparation.Arguments != canonical.Preparation.Arguments {
		return ErrConflict
	}
	snapshot.Preparation.Description = canonical.Preparation.Description
	return nil
}

func hydrateOrderReviewCommit(w OrderReviewWorkspace, snapshot *OrderReviewCommitPreparation) error {
	if snapshot.Description != "" {
		return nil
	}
	if err := exactOrderReviewWorkspace(w, snapshot.Arguments); err != nil {
		return err
	}
	snapshot.Description = w.Preparation.Description
	return nil
}
