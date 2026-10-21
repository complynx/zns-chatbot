package db

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderReviewSave = "save"
const orderReviewCancel = "cancel"
const orderReviewCommit = "commit"

// Transport operation -> event -> current actor -> native session. The session
// helper is shared with pass reviews; it contains no registrations-module check.
func (s *Store) orderReviewTx(
	ctx context.Context,
	scope PaymentScope,
	operation, event, evidence uuid.UUID,
	origin paymentReviewOrigin,
) (pgx.Tx, string, error) {
	if operation == uuid.Nil || !paymentScopeValid(scope, operation.String()) {
		return nil, "", orders.ErrSelection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(ctx, fmt.Sprintf("order-review-call:%s:%s", scope.UserID, operation))
	var role string
	if err == nil {
		_, role, err = lockOrderPaymentEvent(ctx, q, scope, event, evidence, true)
	}
	if err == nil {
		err = origin.authorize(ctx, tx, scope)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, "", err
	}
	return tx, role, nil
}

func readOrderReviewWorkspace(ctx context.Context, q *sqlc.Queries, actor, id uuid.UUID) (OrderReviewWorkspace, error) {
	row, err := q.ReadOrderReviewWorkspace(ctx, sqlc.ReadOrderReviewWorkspaceParams{ID: id, ActorID: actor})
	if err != nil {
		return OrderReviewWorkspace{}, err
	}
	w := OrderReviewWorkspace{
		ID:         row.ID,
		EventID:    row.EventID,
		EvidenceID: row.EvidenceID,
		Revision:   row.Revision,
		State:      row.State,
		Current:    row.Current,
	}
	w.Preparation, err = decodeOrderReviewPreparation(row.Document, row.Presentation)
	return w, err
}

func matchOrderReviewCall(call sqlc.ShuttleOrderReviewCall, kind, source string, hash []byte) error {
	if call.Kind != kind || call.Source != source || !bytes.Equal(call.RequestHash, hash) {
		return ErrConflict
	}
	return nil
}

func saveOrderReviewCall(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	op uuid.UUID,
	kind string,
	origin paymentReviewOrigin,
	hash []byte,
	w OrderReviewWorkspace,
	document any,
	complete bool,
	preparation *OrderReviewCommitPreparation,
) error {
	raw, err := compactOrderReviewDocument(document)
	if err != nil {
		return err
	}
	// Reserve envelope and canonical outcome space within the 64 KiB tool result.
	if len(raw) > 56<<10 {
		return orders.ErrSelection
	}
	row := sqlc.InsertOrderReviewCallParams{
		ActorID:     scope.UserID,
		OperationID: op,
		Kind:        kind,
		Source:      origin.source(),
		RequestHash: hash,
		WorkspaceID: w.ID,
		Document:    raw,
	}
	if complete {
		now, nowErr := q.RegistrationNow(ctx)
		if nowErr != nil {
			return nowErr
		}
		row.Result = raw
		row.CompletedAt = now
	}
	if preparation != nil {
		row.ProposalID = pgtype.UUID{Bytes: preparation.ID, Valid: true}
		row.ExpiresAt = pgtype.Timestamptz{Time: preparation.ExpiresAt, Valid: true}
	}
	return q.InsertOrderReviewCall(ctx, row)
}

func exactOrderReviewWorkspace(w OrderReviewWorkspace, args OrderReviewCommitChange) error {
	if args.Revision < 1 || w.ID != args.WorkspaceID || w.Revision != args.Revision ||
		w.Preparation.Arguments != args.Review {
		return ErrConflict
	}
	return nil
}

func pendingOrderReviewWorkspace(ctx context.Context, q *sqlc.Queries, w OrderReviewWorkspace, revision int64) error {
	if revision != w.Revision || !w.Current || w.State != paymentWorkspacePrepared || w.Preparation.Description == "" {
		return ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if !now.Time.Before(w.Preparation.ExpiresAt) {
		return ErrConflict
	}
	return nil
}

// orderReviewWorkspaceTx resolves only this actor's private workspace before
// authorization, and rereads under normal event/current-identity locks.
func (s *Store) orderReviewWorkspaceTx(
	ctx context.Context,
	scope PaymentScope,
	op, id uuid.UUID,
	origin paymentReviewOrigin,
) (pgx.Tx, OrderReviewWorkspace, string, error) {
	initial, err := readOrderReviewWorkspace(ctx, s.q, scope.UserID, id)
	if err != nil {
		return nil, OrderReviewWorkspace{}, "", err
	}
	tx, role, err := s.orderReviewTx(ctx, scope, op, initial.EventID, initial.EvidenceID, origin)
	if err != nil {
		return nil, OrderReviewWorkspace{}, "", err
	}
	q := s.q.WithTx(tx)
	w, err := readOrderReviewWorkspace(ctx, q, scope.UserID, id)
	if err == nil && w.EventID != initial.EventID {
		err = ErrConflict
	}
	if err == nil {
		_, err = currentAuthorityOrderEvidence(ctx, q, scope, w.EventID, w.EvidenceID)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, OrderReviewWorkspace{}, "", err
	}
	return tx, w, role, nil
}
