package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const miniAppOrderEvidencePageSize = 20

// MiniAppOrderEvidenceFile exposes accepted attachment metadata without Telegram
// locators or bytes. Availability is checked when the source is actually read.
type MiniAppOrderEvidenceFile struct {
	ID         uuid.UUID `json:"id"`
	Filename   string    `json:"filename"`
	MIMEType   string    `json:"mime_type"`
	Kind       string    `json:"kind"`
	SizeBytes  int64     `json:"size_bytes"`
	SentAt     time.Time `json:"sent_at"`
	AcceptedAt time.Time `json:"accepted_at"`
}

type MiniAppOrderEvidenceFiles struct {
	Files []MiniAppOrderEvidenceFile `json:"files"`
	Next  *uuid.UUID                 `json:"next,omitempty"`
}

// SubmitMiniAppOrderEvidence uses the same admission and immutable receipt as
// agent calls. The native session is locked inside the event/owner transaction.
func (s *Store) SubmitMiniAppOrderEvidence(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	operation uuid.UUID,
	args OrderEvidenceSubmission,
) (OrderPaymentEvidence, error) {
	hash, err := orderEvidenceSubmissionHash(operation, args)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, args.EventID, operation)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, _, err := submitOrderEvidence(ctx, s.q.WithTx(tx), scope, operation, args, hash)
	if err != nil {
		return result, err
	}
	// Explicit native replay records native acceptance now, without relabeling the original receipt.
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_miniapp_order_evidence_actions(user_id,operation_id)
 VALUES($1,$2) ON CONFLICT(user_id,operation_id) DO NOTHING`, scope.UserID, operation)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) MiniAppOwnOrderEvidence(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	event, evidence uuid.UUID,
) (OrderPaymentEvidence, error) {
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	row, err := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: evidence, EventID: event})
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	if row.UserID != scope.UserID {
		return OrderPaymentEvidence{}, pgx.ErrNoRows
	}
	return loadOrderEvidenceDecision(ctx, q, row)
}

// MiniAppOrderEvidenceSubmissionReceipt recovers only this owner's original
// submit result, after current entitlement and native session checks.
func (s *Store) MiniAppOrderEvidenceSubmissionReceipt(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	event, operation uuid.UUID,
) (OrderEvidenceOperation, error) {
	if operation == uuid.Nil {
		return OrderEvidenceOperation{}, orders.ErrSelection
	}
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, event, operation)
	if err != nil {
		return OrderEvidenceOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		ReadOrderEvidenceOperation(ctx, sqlc.ReadOrderEvidenceOperationParams{UserID: scope.UserID, OperationID: operation, Kind: paymentSubmission})
	if err != nil {
		return OrderEvidenceOperation{}, err
	}
	if row.EventID != event {
		return OrderEvidenceOperation{}, pgx.ErrNoRows
	}
	result := OrderEvidenceOperation{OperationID: operation, Kind: OrderEvidenceSubmissionReceipt}
	if err = json.Unmarshal(row.Result, &result.Result); err != nil {
		return OrderEvidenceOperation{}, err
	}
	return result, nil
}

// MiniAppOrderEvidenceFileList returns a stable bounded owner cursor. Listing
// never selects a payment target, changes file classification or holds capacity.
func (s *Store) MiniAppOrderEvidenceFileList(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	event, after uuid.UUID,
	limit int32,
) (MiniAppOrderEvidenceFiles, error) {
	if limit < 1 || limit > miniAppOrderEvidencePageSize {
		return MiniAppOrderEvidenceFiles{}, orders.ErrSelection
	}
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return MiniAppOrderEvidenceFiles{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := s.q.WithTx(tx).
		ListMiniAppOrderEvidenceFiles(ctx, sqlc.ListMiniAppOrderEvidenceFilesParams{UserID: scope.UserID, AfterID: after, PageLimit: limit + 1})
	if err != nil {
		return MiniAppOrderEvidenceFiles{}, err
	}
	result := MiniAppOrderEvidenceFiles{Files: []MiniAppOrderEvidenceFile{}}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		result.Files = append(
			result.Files,
			MiniAppOrderEvidenceFile{
				ID:         row.ID,
				Filename:   row.Filename,
				MIMEType:   row.MimeType,
				Kind:       row.Kind,
				SizeBytes:  row.SizeBytes,
				SentAt:     row.SentAt.Time.UTC(),
				AcceptedAt: row.AcceptedAt.Time.UTC(),
			},
		)
	}
	if more {
		result.Next = &result.Files[len(result.Files)-1].ID
	}
	return result, nil
}

// MiniAppOwnOrderEvidenceList discovers agent- or native-submitted claims by
// the order already visible in the form; no cross-interface receipt ID is needed.
func (s *Store) MiniAppOwnOrderEvidenceList(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	event, order, after uuid.UUID,
	limit int32,
) (OrderEvidencePage, error) {
	if limit < 1 || limit > miniAppOrderEvidencePageSize {
		return OrderEvidencePage{}, orders.ErrSelection
	}
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return OrderEvidencePage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOrderEvidencePage(
		ctx,
		s.q.WithTx(tx),
		sqlc.ListOrderEvidenceParams{
			EventID:        event,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			OwnerID:        scope.UserID,
			OrderID:        order,
			AfterID:        after,
			PageLimit:      limit + 1,
		},
	)
}

// MiniAppOrderEvidenceSource authorizes an accepted owner attachment before
// selection. Repeat after external IO; this transaction must not cross network IO.
func (s *Store) MiniAppOrderEvidenceSource(
	ctx context.Context,
	deployment EventScope,
	session MiniAppOrderSession,
	event, file uuid.UUID,
) (File, error) {
	tx, scope, err := s.miniAppOrderTx(ctx, deployment, session, event, uuid.Nil)
	if err != nil {
		return File{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		GetOwnedFile(ctx, sqlc.GetOwnedFileParams{ID: file, ID_2: scope.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject})
	if err != nil {
		return File{}, err
	}
	return storedFile(row), nil
}
