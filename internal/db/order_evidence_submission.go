package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderSubmitted = "submitted"

// SubmitOwnOrderEvidence atomically retains the claim, applicable holds, notices
// and original receipt. Only exact, timely, fully available requirements admit.
// Unresolved policies preserve the file/target as needs_decision without holds.
func (s *Store) SubmitOwnOrderEvidence(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
	args OrderEvidenceSubmission,
) (OrderPaymentEvidence, error) {
	hash, err := orderEvidenceSubmissionHash(operation, args)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	tx, err := s.orderTx(ctx, scope, args.EventID, operation)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := submitOrderEvidence(ctx, s.q.WithTx(tx), scope, operation, args, hash)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func orderEvidenceSubmissionHash(operation uuid.UUID, args OrderEvidenceSubmission) ([]byte, error) {
	if operation == uuid.Nil || args.EventID == uuid.Nil || args.OrderID == uuid.Nil || args.FileID == uuid.Nil ||
		args.ExpectedOrderVersion < 1 ||
		args.AmountMinor < 1 ||
		args.AmountMinor > orders.MaxPrice {
		return nil, orders.ErrSelection
	}
	return orderHash(args)
}

func submitOrderEvidence(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	operation uuid.UUID,
	args OrderEvidenceSubmission,
	hash []byte,
) (OrderPaymentEvidence, bool, error) {
	file, err := q.GetOwnedFile(
		ctx,
		sqlc.GetOwnedFileParams{
			ID:             args.FileID,
			ID_2:           scope.UserID,
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	prior, found, err := readOrderEvidenceReceipt(
		ctx,
		q,
		scope.UserID,
		args.EventID,
		operation,
		paymentSubmission,
		hash,
	)
	if err != nil || found {
		return prior, found, err
	}
	row, err := q.ReadSubmittedOrder(
		ctx,
		sqlc.ReadSubmittedOrderParams{ID: args.OrderID, EventID: args.EventID, UserID: scope.UserID},
	)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	order, err := submittedOrderRecord(row)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	if order.Version != args.ExpectedOrderVersion || order.State != orderSubmitted {
		return OrderPaymentEvidence{}, false, ErrConflict
	}
	active, err := q.HasActiveOrderEvidence(ctx, args.OrderID)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	if active {
		return OrderPaymentEvidence{}, false, ErrConflict
	}
	paymentScope := PaymentScope{
		BotID: scope.BotID, Organization: scope.Organization,
		UserID:  scope.UserID,
		Subject: scope.Subject,
	}
	if err = paymentSourceAvailable(ctx, q, paymentScope, file.UniqueID); err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	state, reason, err := orderEvidenceAdmission(ctx, q, args, order, file.SentAt.Time, now.Time)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	return storeOrderEvidence(ctx, q, scope, operation, args, hash, order, file, now, state, reason)
}

func storeOrderEvidence(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	operation uuid.UUID,
	args OrderEvidenceSubmission,
	hash []byte,
	order SubmittedOrder,
	file sqlc.ShuttleFile,
	now pgtype.Timestamptz,
	state, reason string,
) (OrderPaymentEvidence, bool, error) {
	scale := order.Snapshot.Quote.CurrencyScale
	if scale < 0 || scale > 6 {
		return OrderPaymentEvidence{}, false, orders.ErrSelection
	}
	saved, err := q.InsertOrderEvidence(ctx, sqlc.InsertOrderEvidenceParams{
		ID:                  uuid.New(),
		OrderID:             args.OrderID,
		EventID:             args.EventID,
		UserID:              scope.UserID,
		FileID:              args.FileID,
		SourceUniqueID:      file.UniqueID,
		ReportedAmountMinor: args.AmountMinor,
		QuotedAmountMinor:   order.Snapshot.Quote.TotalMinor,
		Currency:            order.Snapshot.Quote.Currency,
		CurrencyScale:       int32(scale),
		SentAt:              file.SentAt,
		SubmittedAt:         now,
		DueAt:               pgtype.Timestamptz{Time: order.Snapshot.SalesClosesAt, Valid: true},
		AdmissionState:      state,
		Reason:              reason,
	})
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	result := orderEvidenceRecord(saved)
	if state == OrderEvidencePending {
		for _, demand := range order.Snapshot.Capacity.Demands {
			if err = q.InsertOrderAllocation(
				ctx,
				sqlc.InsertOrderAllocationParams{
					EvidenceID: result.ID,
					EventID:    result.EventID,
					ResourceID: demand.ResourceID,
					Units:      demand.Units,
				},
			); err != nil {
				return OrderPaymentEvidence{}, false, err
			}
		}
	}
	if err = createOrderEvidenceNotices(ctx, q, scope, result); err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	err = saveOrderEvidenceReceipt(ctx, q, scope.UserID, operation, paymentSubmission, hash, result)
	return result, false, err
}

func orderEvidenceAdmission(
	ctx context.Context,
	q *sqlc.Queries,
	args OrderEvidenceSubmission,
	order SubmittedOrder,
	sent, now time.Time,
) (string, string, error) {
	if args.AmountMinor != order.Snapshot.Quote.TotalMinor {
		return OrderEvidenceNeedsDecision, "amount_requires_decision", nil
	}
	if !sent.Before(order.Snapshot.SalesClosesAt) || !now.Before(order.Snapshot.SalesClosesAt) {
		return OrderEvidenceNeedsDecision, "deadline_requires_decision", nil
	}
	if order.Snapshot.Capacity == nil {
		return OrderEvidenceNeedsDecision, "unknown_requirements", nil
	}
	shortage, err := orderCapacityShortage(ctx, q, args.EventID, order.Snapshot.Capacity)
	if err != nil {
		return "", "", err
	}
	if shortage {
		if len(order.Snapshot.Capacity.Demands) == 1 {
			return OrderEvidenceExhausted, "refund_attention_required", nil
		}
		return OrderEvidenceNeedsDecision, "mixed_capacity_shortage", nil
	}
	return OrderEvidencePending, "", nil
}

func (s *Store) OwnOrderEvidence(
	ctx context.Context,
	scope ProfileScope,
	event, evidence uuid.UUID,
) (OrderPaymentEvidence, error) {
	tx, err := s.orderTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: evidence, EventID: event})
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	if row.UserID != scope.UserID {
		return OrderPaymentEvidence{}, pgx.ErrNoRows
	}
	return loadOrderEvidenceDecision(ctx, s.q.WithTx(tx), row)
}
