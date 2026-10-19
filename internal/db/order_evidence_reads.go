package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// OrderEvidenceOperationKind identifies the original business receipt phase.
type OrderEvidenceOperationKind string

const (
	OrderEvidenceSubmissionReceipt OrderEvidenceOperationKind = "submit"
	OrderEvidenceReviewReceipt     OrderEvidenceOperationKind = "review"
)

type OrderEvidenceOperation struct {
	OperationID uuid.UUID                  `json:"operation_id"`
	Kind        OrderEvidenceOperationKind `json:"kind"`
	Result      OrderPaymentEvidence       `json:"result"`
}

func orderEvidenceProfile(scope PaymentScope) ProfileScope {
	return ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		UserID:       scope.UserID,
		Subject:      scope.Subject,
	}
}

// orderEvidenceAuthorityTx uses the orders module, independently of registrations.
func (s *Store) orderEvidenceAuthorityTx(ctx context.Context, scope PaymentScope, event uuid.UUID) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	_, _, err = lockPaymentModuleEvent(ctx, s.q.WithTx(tx), scope, event, "orders", true)
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}

func currentAuthorityOrderEvidence(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	event, id uuid.UUID,
) (OrderPaymentEvidence, error) {
	row, err := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: id, EventID: event})
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	// A revoked payer cannot read their proof, but must not prevent its resolution.
	if _, err = q.LockOrderEvidenceOwner(
		ctx,
		sqlc.LockOrderEvidenceOwnerParams{ID: row.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return OrderPaymentEvidence{}, err
	}
	return loadOrderEvidenceDecision(ctx, q, row)
}

// OrderEvidenceForReview checks current event authority, including for own proofs.
func (s *Store) OrderEvidenceForReview(
	ctx context.Context,
	scope PaymentScope,
	event, id uuid.UUID,
) (OrderPaymentEvidence, error) {
	tx, err := s.orderEvidenceAuthorityTx(ctx, scope, event)
	if err != nil {
		return OrderPaymentEvidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return currentAuthorityOrderEvidence(ctx, s.q.WithTx(tx), scope, event, id)
}

// OrderEvidenceOperationByID returns only this actor's original receipt. The
// initial owner lock is released before normal event -> actor authorization.
// Missing receipts do not prove non-execution; current state is read separately.
func (s *Store) OrderEvidenceOperationByID(
	ctx context.Context,
	scope PaymentScope,
	operation uuid.UUID,
	kind OrderEvidenceOperationKind,
) (OrderEvidenceOperation, error) {
	if operation == uuid.Nil || (kind != OrderEvidenceSubmissionReceipt && kind != OrderEvidenceReviewReceipt) {
		return OrderEvidenceOperation{}, orders.ErrSelection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderEvidenceOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, orderEvidenceProfile(scope), false); err != nil {
		return OrderEvidenceOperation{}, err
	}
	key := sqlc.ReadOrderEvidenceOperationParams{UserID: scope.UserID, OperationID: operation, Kind: string(kind)}
	first, err := s.q.WithTx(tx).ReadOrderEvidenceOperation(ctx, key)
	if err != nil {
		return OrderEvidenceOperation{}, err
	}
	rollbackLegacyArchive(ctx, tx)
	return s.authorizedOrderEvidenceOperation(ctx, scope, first.EventID, key, kind)
}

func (s *Store) authorizedOrderEvidenceOperation(
	ctx context.Context,
	scope PaymentScope,
	event uuid.UUID,
	key sqlc.ReadOrderEvidenceOperationParams,
	kind OrderEvidenceOperationKind,
) (OrderEvidenceOperation, error) {
	var tx pgx.Tx
	var err error
	if kind == OrderEvidenceSubmissionReceipt {
		tx, err = s.orderTx(ctx, orderEvidenceProfile(scope), event, uuid.Nil)
	} else {
		tx, err = s.orderEvidenceAuthorityTx(ctx, scope, event)
	}
	if err != nil {
		return OrderEvidenceOperation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).ReadOrderEvidenceOperation(ctx, key)
	if err != nil {
		return OrderEvidenceOperation{}, err
	}
	if row.EventID != event {
		return OrderEvidenceOperation{}, ErrConflict
	}
	result := OrderEvidenceOperation{OperationID: key.OperationID, Kind: kind}
	if err = json.Unmarshal(row.Result, &result.Result); err != nil {
		return OrderEvidenceOperation{}, err
	}
	return result, nil
}

// OrderEvidenceFile resolves proof IDs, never grants access by foreign file ID.
// Callers must repeat authorization after external Telegram IO before disclosure.
func (s *Store) OrderEvidenceFile(
	ctx context.Context,
	scope PaymentScope,
	evidence uuid.UUID,
	audit bool,
) (File, error) {
	event, err := s.q.OrderEvidenceEvent(
		ctx,
		sqlc.OrderEvidenceEventParams{ID: evidence, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return File{}, err
	}
	// Read the association under the event lock before choosing owner or authority.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return File{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if _, err = q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return File{}, err
	}
	row, err := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: evidence, EventID: event})
	if err != nil {
		return File{}, err
	}
	switch {
	case audit:
		_, _, err = lockPaymentModuleEvent(ctx, q, scope, event, "orders", true)
	case row.UserID != scope.UserID:
		return File{}, pgx.ErrNoRows
	default:
		err = authorizeOrdersSelection(ctx, tx, orderEvidenceProfile(scope), event)
	}
	if err != nil {
		return File{}, err
	}
	file, err := q.GetOrderEvidenceFile(
		ctx,
		sqlc.GetOrderEvidenceFileParams{
			ID:         evidence,
			EventID:    event,
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
		},
	)
	if err != nil {
		return File{}, err
	}
	return storedFile(file), nil
}
