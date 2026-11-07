package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func readOrderEvidenceReceipt(
	ctx context.Context,
	q *sqlc.Queries,
	actor, event, operation uuid.UUID,
	kind string,
	hash []byte,
) (OrderPaymentEvidence, bool, error) {
	row, err := q.ReadOrderEvidenceOperation(
		ctx,
		sqlc.ReadOrderEvidenceOperationParams{UserID: actor, OperationID: operation, Kind: kind},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderPaymentEvidence{}, false, nil
	}
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	if row.EventID != event || !bytes.Equal(row.RequestHash, hash) {
		return OrderPaymentEvidence{}, false, ErrConflict
	}
	var result OrderPaymentEvidence
	err = json.Unmarshal(row.Result, &result)
	return result, true, err
}

func saveOrderEvidenceReceipt(
	ctx context.Context,
	q *sqlc.Queries,
	actor, operation uuid.UUID,
	kind string,
	hash []byte,
	result OrderPaymentEvidence,
) error {
	raw, err := orderJSON(result)
	if err != nil {
		return err
	}
	return q.InsertOrderEvidenceOperation(
		ctx,
		sqlc.InsertOrderEvidenceOperationParams{
			UserID:      actor,
			OperationID: operation,
			Kind:        kind,
			EventID:     result.EventID,
			RequestHash: hash,
			Result:      raw,
		},
	)
}

func createOrderEvidenceNotices(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	evidence OrderPaymentEvidence,
) error {
	recipients, err := q.ListPaymentNotificationRecipients(
		ctx,
		sqlc.ListPaymentNotificationRecipientsParams{
			EventID:    evidence.EventID,
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
		},
	)
	if err != nil {
		return err
	}
	raw, err := orderJSON(evidence)
	if err != nil {
		return err
	}
	seen := make(map[uuid.UUID]bool, len(recipients)+1)
	for _, id := range append(recipients, evidence.UserID) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err = q.InsertOrderEvidenceNotice(
			ctx,
			sqlc.InsertOrderEvidenceNoticeParams{
				ID:          uuid.New(),
				EvidenceID:  evidence.ID,
				RecipientID: id,
				Kind:        evidence.State,
				Document:    raw,
			},
		); err != nil {
			return err
		}
	}
	return nil
}
