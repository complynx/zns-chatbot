package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func normalizeOrderEvidenceReview(args OrderEvidenceReview) (OrderEvidenceReview, error) {
	args.Reason = strings.TrimSpace(args.Reason)
	if args.EventID == uuid.Nil || args.EvidenceID == uuid.Nil || args.ExpectedVersion != 1 ||
		(args.Decision != OrderEvidenceConfirmed && args.Decision != OrderEvidenceRejected) || args.Reason == "" ||
		!utf8.ValidString(
			args.Reason,
		) || utf8.RuneCountInString(args.Reason) > 512 || strings.ContainsRune(args.Reason, 0) {
		return args, orders.ErrSelection
	}
	return args, nil
}

func (s *Store) orderEvidenceReviewTx(
	ctx context.Context,
	scope PaymentScope,
	operation, event, evidence uuid.UUID,
) (pgx.Tx, string, error) {
	if operation == uuid.Nil || !paymentScopeValid(scope, operation.String()) {
		return nil, "", orders.ErrSelection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(ctx, fmt.Sprintf("order-evidence-review:%s:%s", scope.UserID, operation))
	var role string
	if err == nil {
		_, role, err = lockOrderPaymentEvent(ctx, q, scope, event, evidence, true)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, "", err
	}
	return tx, role, nil
}

func orderEvidenceReviewTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args OrderEvidenceReview,
) (OrderPaymentEvidence, error) {
	row, err := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: args.EvidenceID, EventID: args.EventID})
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	evidence := orderEvidenceRecord(row)
	if evidence.Version != args.ExpectedVersion || evidence.State == OrderEvidenceConfirmed ||
		evidence.State == OrderEvidenceRejected {
		return OrderPaymentEvidence{}, ErrConflict
	}
	if args.Decision == OrderEvidenceConfirmed && evidence.State != OrderEvidencePending {
		return OrderPaymentEvidence{}, ErrConflict
	}
	if _, err = q.LockOrderEvidenceOwner(
		ctx,
		sqlc.LockOrderEvidenceOwnerParams{ID: evidence.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return OrderPaymentEvidence{}, err
	}
	return evidence, nil
}

func (s *Store) PrepareOrderEvidenceReview(
	ctx context.Context,
	scope PaymentScope,
	operation uuid.UUID,
	args OrderEvidenceReview,
) (OrderEvidenceReviewPreparation, error) {
	args, err := normalizeOrderEvidenceReview(args)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	hash, err := orderHash(args)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	tx, _, err := s.orderEvidenceReviewTx(ctx, scope, operation, args.EventID, args.EvidenceID)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = rejectStandaloneOrderReview(ctx, q, scope, args); err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	result, err := prepareOrderEvidenceReview(ctx, q, scope, operation, args, hash, "")
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	return result, tx.Commit(ctx)
}

// The caller owns the transaction, including any shared workspace and receipt.
func prepareOrderEvidenceReview(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	operation uuid.UUID,
	args OrderEvidenceReview,
	hash []byte,
	language string,
) (OrderEvidenceReviewPreparation, error) {
	prior, err := q.ReadOrderEvidencePreparation(
		ctx,
		sqlc.ReadOrderEvidencePreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if prior.EventID != args.EventID || !bytes.Equal(prior.RequestHash, hash) {
			return OrderEvidenceReviewPreparation{}, ErrConflict
		}
		var result OrderEvidenceReviewPreparation
		result, err = decodeOrderReviewPreparation(prior.Document, prior.Presentation)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderEvidenceReviewPreparation{}, err
	}
	evidence, err := orderEvidenceReviewTarget(ctx, q, scope, args)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	result := OrderEvidenceReviewPreparation{
		ID:        uuid.New(),
		Arguments: args,
		Evidence:  evidence,
		ExpiresAt: now.Time.UTC().Add(paymentPreparationTTL),
	}
	result.Description, err = orderReviewSummary(ctx, q, scope, result, language)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	compact := result
	compact.Description = ""
	raw, err := orderJSON(compact)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	err = q.InsertOrderEvidencePreparation(
		ctx,
		sqlc.InsertOrderEvidencePreparationParams{
			ID:           result.ID,
			UserID:       scope.UserID,
			OperationID:  operation,
			EventID:      args.EventID,
			RequestHash:  hash,
			Document:     raw,
			Presentation: result.Description,
			ExpiresAt:    pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
		},
	)
	if err != nil {
		return OrderEvidenceReviewPreparation{}, err
	}
	return result, nil
}

// ReviewOrderEvidence rechecks current authority and exact consent before changing
// only this attempt's holds. Confirmed holds remain occupied; rejection releases.
func (s *Store) ReviewOrderEvidence(
	ctx context.Context,
	scope PaymentScope,
	operation, proposal uuid.UUID,
	args OrderEvidenceReview,
) (OrderPaymentEvidence, error) {
	args, err := normalizeOrderEvidenceReview(args)
	if err != nil || proposal == uuid.Nil {
		return OrderPaymentEvidence{}, orders.ErrSelection
	}
	hash, err := orderHash(args)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	requestHash, err := orderHash(struct {
		Arguments  OrderEvidenceReview
		ProposalID uuid.UUID
	}{args, proposal})
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	tx, role, err := s.orderEvidenceReviewTx(ctx, scope, operation, args.EventID, args.EvidenceID)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = rejectStandaloneOrderReview(ctx, s.q.WithTx(tx), scope, args); err != nil {
		return OrderPaymentEvidence{}, err
	}
	result, replayed, err := reviewOrderEvidence(
		ctx,
		s.q.WithTx(tx),
		scope,
		operation,
		proposal,
		args,
		hash,
		requestHash,
		role,
	)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func reviewOrderEvidence(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	operation, proposal uuid.UUID,
	args OrderEvidenceReview,
	hash, requestHash []byte,
	role string,
) (OrderPaymentEvidence, bool, error) {
	prior, found, err := readOrderEvidenceReceipt(
		ctx,
		q,
		scope.UserID,
		args.EventID,
		operation,
		paymentReview,
		requestHash,
	)
	if err != nil || found {
		return prior, found, err
	}
	prepared, err := q.ReadOrderEvidencePreparation(
		ctx,
		sqlc.ReadOrderEvidencePreparationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	if prepared.ID != proposal || prepared.EventID != args.EventID || !bytes.Equal(prepared.RequestHash, hash) ||
		!prepared.ExpiresAt.Time.After(now.Time) {
		return OrderPaymentEvidence{}, false, ErrConflict
	}
	evidence, err := orderEvidenceReviewTarget(ctx, q, scope, args)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	allocationState := "released"
	amount := int64(0)
	if args.Decision == OrderEvidenceConfirmed {
		allocationState = "occupied"
		amount = evidence.QuotedAmountMinor
	}
	err = q.InsertOrderEvidenceReview(
		ctx,
		sqlc.InsertOrderEvidenceReviewParams{
			EvidenceID:    evidence.ID,
			ActorID:       scope.UserID,
			ActorSubject:  scope.Subject,
			AuthorityRole: role,
			Decision:      args.Decision,
			AmountMinor:   amount,
			Reason:        args.Reason,
			ReviewedAt:    now,
		},
	)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	if err = q.ChangeOrderAllocations(
		ctx,
		sqlc.ChangeOrderAllocationsParams{EvidenceID: evidence.ID, State: allocationState},
	); err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	row, err := q.ReviewOrderEvidence(ctx, sqlc.ReviewOrderEvidenceParams{ID: evidence.ID, State: args.Decision})
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	result, err := loadOrderEvidenceDecision(ctx, q, row)
	if err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	profile := ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		UserID:       scope.UserID,
		Subject:      scope.Subject,
	}
	if err = createOrderEvidenceNotices(ctx, q, profile, result); err != nil {
		return OrderPaymentEvidence{}, false, err
	}
	err = saveOrderEvidenceReceipt(ctx, q, scope.UserID, operation, paymentReview, requestHash, result)
	return result, false, err
}

func loadOrderEvidenceDecision(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ShuttleOrderEvidence,
) (OrderPaymentEvidence, error) {
	result := orderEvidenceRecord(row)
	if result.State != OrderEvidenceConfirmed && result.State != OrderEvidenceRejected {
		return result, nil
	}
	review, err := q.ReadOrderEvidenceReview(ctx, result.ID)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	result.Review = &OrderPaymentDecision{
		ActorID:       review.ActorID,
		AuthorityRole: review.AuthorityRole,
		Decision:      review.Decision,
		AmountMinor:   review.AmountMinor,
		Reason:        review.Reason,
		ReviewedAt:    review.ReviewedAt.Time.UTC(),
	}
	return result, nil
}

// Once this actor uses shared revisions for a proof, standalone entry points
// cannot consume canonical or older independent consent. Shared commits use the
// private transaction-local helper and complete their workspace/receipt atomically.
func rejectStandaloneOrderReview(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args OrderEvidenceReview,
) error {
	present, err := q.HasOrderReviewWorkspaceHistory(
		ctx,
		sqlc.HasOrderReviewWorkspaceHistoryParams{
			ActorID:    scope.UserID,
			EventID:    args.EventID,
			EvidenceID: args.EvidenceID,
		},
	)
	if err != nil {
		return err
	}
	if present {
		return ErrConflict
	}
	return nil
}
