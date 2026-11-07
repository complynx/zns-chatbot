package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

// MiniAppPaymentEvidenceSession carries verified identity and a local IAM fence.
// AuthorityUnchanged must remain true through lock waits and the final commit.
type MiniAppPaymentEvidenceSession struct {
	UserID             uuid.UUID
	Bearer, Generation [sha256.Size]byte
	AuthorityUnchanged func() bool
}

type MiniAppPaymentEvidenceSubmission struct {
	PassID      uuid.UUID `json:"pass_id"`
	FileID      uuid.UUID `json:"file_id"`
	AmountMinor int64     `json:"amount_minor"`
	Purpose     string    `json:"purpose"`
}

type MiniAppPaymentEvidenceReceipt struct {
	Timezone    string            `json:"timezone"`
	OperationID string            `json:"operation_id"`
	Kind        string            `json:"kind"`
	Result      payments.Evidence `json:"result"`
}

func paymentEvidenceFence(scope PaymentScope, session MiniAppPaymentEvidenceSession) error {
	if session.UserID != scope.UserID || session.AuthorityUnchanged == nil || !session.AuthorityUnchanged() {
		return ErrMiniAppDenied
	}
	return nil
}

// miniAppPaymentEvidenceTx owns one lease. Event writes use the same operation,
// event, actor SHARE and session order as payment review. It never upgrades users.
func (s *Store) miniAppPaymentEvidenceTx(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentEvidenceSession, event uuid.UUID, operation string,
) (pgx.Tx, error) {
	if !paymentScopeValid(scope, "read") {
		return nil, payments.ErrInvalid
	}
	if err := paymentEvidenceFence(scope, session); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (pgx.Tx, error) { rollbackLegacyArchive(ctx, tx); return nil, err }
	q := s.q.WithTx(tx)
	if operation != "" {
		if err = q.LockEventOperation(ctx, paymentOperationLock(scope, operation)); err != nil {
			return fail(err)
		}
	}
	if event != uuid.Nil {
		if _, _, err = lockPaymentEvent(ctx, q, scope, event, false); err != nil {
			return fail(err)
		}
	} else {
		owner, ownerErr := q.LockAuthorityActor(
			ctx,
			sqlc.LockAuthorityActorParams{
				BotID:          scope.BotID,
				ZitadelOrg:     scope.Organization,
				ZitadelSubject: scope.Subject,
			},
		)
		if ownerErr != nil {
			return fail(ownerErr)
		}
		if owner.ID != scope.UserID {
			return fail(ErrMiniAppDenied)
		}
	}
	if err = paymentEvidenceFence(scope, session); err != nil {
		return fail(err)
	}
	native := MiniAppPaymentSession{
		Bearer: session.Bearer, Generation: session.Generation, AuthorityUnchanged: session.AuthorityUnchanged,
	}
	if err = (paymentReviewOrigin{session: &native}).authorize(ctx, tx, scope); err != nil {
		return fail(err)
	}
	if err = paymentEvidenceFence(scope, session); err != nil {
		return fail(err)
	}
	return tx, nil
}

func nativePaymentSubmission(args MiniAppPaymentEvidenceSubmission, event uuid.UUID) payments.Submission {
	return payments.Submission{
		EventID:     event,
		PassID:      args.PassID,
		FileID:      args.FileID,
		AmountMinor: args.AmountMinor,
		Purpose:     args.Purpose,
	}
}

// frozenNativePaymentSubmission preserves receipt terms before reading current pass terms.
func frozenNativePaymentSubmission(ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	operation string, event uuid.UUID, args MiniAppPaymentEvidenceSubmission,
) (payments.Submission, error) {
	canonical := nativePaymentSubmission(args, event)
	prior, err := q.GetPaymentOperation(ctx, paymentOperationKey(scope, operation))
	switch {
	case err == nil:
		if prior.Kind != paymentSubmission {
			return payments.Submission{}, ErrConflict
		}
		var receipt payments.Evidence
		if err = json.Unmarshal(prior.Result, &receipt); err != nil {
			return payments.Submission{}, err
		}
		if receipt.UserID != scope.UserID || receipt.EventID != event || receipt.PassID != args.PassID ||
			receipt.FileID != args.FileID ||
			receipt.AmountMinor != args.AmountMinor ||
			receipt.Purpose != args.Purpose {
			return payments.Submission{}, ErrConflict
		}
		canonical.Currency, canonical.CurrencyScale = receipt.Currency, receipt.CurrencyScale
	case errors.Is(err, pgx.ErrNoRows):
		entry, readErr := paymentPass(ctx, q, scope, event, args.PassID)
		if readErr != nil {
			return payments.Submission{}, readErr
		}
		if entry.UserID != scope.UserID || entry.Pass == nil {
			return payments.Submission{}, pgx.ErrNoRows
		}
		canonical.Currency, canonical.CurrencyScale = entry.Pass.Currency, entry.Pass.CurrencyScale
	default:
		return payments.Submission{}, err
	}
	return canonical, nil
}

// SubmitMiniAppPaymentEvidence records a reported transfer, never confirmed money.
// Replays recover frozen original terms before reading a changed payment balance.
func (s *Store) SubmitMiniAppPaymentEvidence(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentEvidenceSession, operation string, event uuid.UUID, args MiniAppPaymentEvidenceSubmission,
) (payments.Evidence, error) {
	canonical := nativePaymentSubmission(args, event)
	if !paymentScopeValid(scope, operation) || !canonical.Valid() {
		return payments.Evidence{}, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, operation)
	if err != nil {
		return payments.Evidence{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	canonical, err = frozenNativePaymentSubmission(ctx, q, scope, operation, event, args)
	if err != nil {
		return payments.Evidence{}, err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return payments.Evidence{}, err
	}
	hash := sha256.Sum256(raw)
	result, err := submitPaymentEvidence(
		ctx,
		q,
		scope,
		operation,
		canonical,
		hash[:],
		func() error { return paymentEvidenceFence(scope, session) },
	)
	if err != nil {
		return payments.Evidence{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_native_payment_evidence_actions(bot_id,organization_id,actor,operation_id,user_id)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(bot_id,organization_id,actor,operation_id) DO NOTHING`,
		scope.BotID,
		scope.Organization,
		scope.Subject,
		operation,
		scope.UserID,
	)
	if err != nil {
		return payments.Evidence{}, err
	}
	if err = paymentEvidenceFence(scope, session); err != nil {
		return payments.Evidence{}, err
	}
	return result, tx.Commit(ctx)
}

// MiniAppPaymentSubmissionReceipt is read-only recovery; missing is unknown.
func (s *Store) MiniAppPaymentSubmissionReceipt(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentEvidenceSession, event uuid.UUID, operation string,
) (MiniAppPaymentEvidenceReceipt, error) {
	if !paymentScopeValid(scope, operation) || event == uuid.Nil {
		return MiniAppPaymentEvidenceReceipt{}, payments.ErrInvalid
	}
	tx, err := s.miniAppPaymentEvidenceTx(ctx, scope, session, event, operation)
	if err != nil {
		return MiniAppPaymentEvidenceReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).GetPaymentOperation(ctx, paymentOperationKey(scope, operation))
	if err != nil {
		return MiniAppPaymentEvidenceReceipt{}, err
	}
	result := MiniAppPaymentEvidenceReceipt{OperationID: operation, Kind: row.Kind}
	if row.Kind != paymentSubmission {
		return result, pgx.ErrNoRows
	}
	if err = json.Unmarshal(row.Result, &result.Result); err != nil {
		return result, err
	}
	if result.Result.UserID != scope.UserID || result.Result.EventID != event {
		return MiniAppPaymentEvidenceReceipt{}, pgx.ErrNoRows
	}
	eventRow, err := s.q.WithTx(tx).
		GetEvent(ctx, sqlc.GetEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization})
	if err != nil {
		return MiniAppPaymentEvidenceReceipt{}, err
	}
	result.Timezone = eventRow.Timezone
	if err = paymentEvidenceFence(scope, session); err != nil {
		return MiniAppPaymentEvidenceReceipt{}, err
	}
	return result, nil
}
