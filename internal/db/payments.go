package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const (
	paymentSubmission          = "submit"
	paymentReview              = "review"
	paymentPreparationTTL      = 5 * time.Minute
	globalPaymentAdministrator = "global_administrator"
	paymentWorkspacePrepared   = "prepared"
)

// PaymentScope is constructed from verified identity, never agent arguments.
type PaymentScope struct {
	EventScope

	UserID      uuid.UUID
	Subject     string
	GlobalAdmin bool
}
type PaymentPreparation struct {
	ID          uuid.UUID             `json:"proposal_id"`
	Arguments   payments.ReviewChange `json:"arguments"`
	Description string                `json:"description"`
	ExpiresAt   time.Time             `json:"expires_at"`
}

func paymentScopeValid(scope PaymentScope, operation string) bool {
	return scope.BotID > 0 && scope.Organization != "" && scope.UserID != uuid.Nil && scope.Subject != "" &&
		operation != "" &&
		len(operation) <= 128
}

// All payment effects and authority changes serialize on the event. Sales closing
// does not prevent a human from reviewing an already submitted timely proof.
func lockPaymentEvent(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	eventID uuid.UUID,
	requireAuthority bool,
) (sqlc.ShuttleEvent, string, error) {
	return lockPaymentModuleEvent(ctx, q, scope, eventID, "registrations", requireAuthority)
}

// lockPaymentModuleEvent shares current actor/authority checks without requiring
// an unrelated module. Only domain handlers choose the module, never requests.
func lockPaymentModuleEvent(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	eventID uuid.UUID,
	module string,
	requireAuthority bool,
) (sqlc.ShuttleEvent, string, error) {
	event, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return event, "", err
	}
	actor, err := q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return event, "", err
	}
	if actor.ID != scope.UserID {
		return event, "", payments.ErrDenied
	}
	if !slices.Contains(event.Modules, module) {
		return event, "", payments.ErrUnavailable
	}
	if !requireAuthority {
		return event, "", nil
	}
	if scope.GlobalAdmin {
		return event, globalPaymentAdministrator, nil
	}
	role, err := q.ReadEventAuthority(
		ctx,
		sqlc.ReadEventAuthorityParams{
			EventID:        eventID,
			UserID:         actor.ID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return event, "", payments.ErrDenied
	}
	if err != nil {
		return event, "", err
	}
	if role.Role != AuthorityAmbassador && role.Role != AuthorityAdministrator {
		return event, "", payments.ErrDenied
	}
	return event, role.Role, nil
}

func paymentPass(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	eventID, passID uuid.UUID,
) (registrations.Registration, error) {
	row, err := q.ReadPaymentPass(ctx, sqlc.ReadPaymentPassParams{ID: passID, EventID: eventID})
	if err != nil {
		return registrations.Registration{}, err
	}
	if _, err = q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: row.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return registrations.Registration{}, err
	}
	return registrationRecord(ctx, q, row)
}
func paymentBalance(ctx context.Context, q *sqlc.Queries, pass registrations.Pass) (payments.Balance, error) {
	version, err := q.ReadPaymentVersion(ctx, pass.ID)
	if err != nil {
		return payments.Balance{}, err
	}
	total, err := q.PaymentTotal(ctx, pass.ID)
	if err != nil {
		return payments.Balance{}, err
	}
	return payments.Summarize(pass, version, total)
}
func paymentOperationLock(scope PaymentScope, operation string) string {
	return fmt.Sprintf("payment:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, operation)
}
func paymentOperationKey(scope PaymentScope, operation string) sqlc.GetPaymentOperationParams {
	return sqlc.GetPaymentOperationParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		OperationID:    operation,
	}
}

func paymentReceipt(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	operation, kind string,
	hash []byte,
	result any,
) (bool, error) {
	row, err := q.GetPaymentOperation(ctx, paymentOperationKey(scope, operation))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if row.Kind != kind || !bytes.Equal(row.RequestHash, hash) {
		return false, ErrConflict
	}
	return true, json.Unmarshal(row.Result, result)
}

func savePaymentReceipt(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	operation, kind string,
	hash []byte,
	result any,
) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return q.InsertPaymentOperation(
		ctx,
		sqlc.InsertPaymentOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
			Kind:           kind,
			RequestHash:    hash,
			Result:         raw,
		},
	)
}
func paymentStorageError(err error) error {
	if pg, ok := errors.AsType[*pgconn.PgError](err); ok && pg.Code == "23505" {
		return ErrConflict
	}
	return err
}

// reviewedEvidenceRecord exposes the stored human decision separately from the
// owner's reported amount, under the same evidence access checks.
func reviewedEvidenceRecord(row sqlc.ShuttlePaymentEvidence, state string, review []byte) (payments.Evidence, error) {
	result := evidenceRecord(row, state)
	if len(review) > 0 {
		if err := json.Unmarshal(review, &result.Review); err != nil {
			return payments.Evidence{}, err
		}
	}
	return result, nil
}

func evidenceRecord(row sqlc.ShuttlePaymentEvidence, state string) payments.Evidence {
	return payments.Evidence{
		ID:            row.ID,
		EventID:       row.EventID,
		PassID:        row.PassID,
		UserID:        row.UserID,
		FileID:        row.FileID,
		AmountMinor:   row.AmountMinor,
		Currency:      row.Currency,
		CurrencyScale: row.CurrencyScale,
		Purpose:       row.Purpose,
		SentAt:        row.SentAt.Time.UTC(),
		SubmittedAt:   row.SubmittedAt.Time.UTC(),
		DueAt:         row.DueAt.Time.UTC(),
		Timely:        row.SentAt.Time.Before(row.DueAt.Time),
		State:         state,
	}
}

// SubmitPaymentEvidence shares an accepted owner file with this event's payment
// authorities. It creates no confirmed money, changes no pass state and never
// trusts a caller-provided timestamp. Pending or confirmed file provenance cannot credit another expectation.
// Rejected evidence can be submitted again with corrected claims and a new operation.
func (s *Store) SubmitPaymentEvidence(
	ctx context.Context,
	scope PaymentScope,
	operation string,
	args payments.Submission,
) (payments.Evidence, error) {
	if !paymentScopeValid(scope, operation) || !args.Valid() {
		return payments.Evidence{}, payments.ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return payments.Evidence{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return payments.Evidence{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(ctx, paymentOperationLock(scope, operation)); err != nil {
		return payments.Evidence{}, err
	}
	if _, _, err = lockPaymentEvent(ctx, q, scope, args.EventID, false); err != nil {
		return payments.Evidence{}, err
	}
	var prior payments.Evidence
	found, err := paymentReceipt(ctx, q, scope, operation, paymentSubmission, hash[:], &prior)
	if found || err != nil {
		return prior, err
	}
	entry, err := paymentPass(ctx, q, scope, args.EventID, args.PassID)
	if err != nil {
		return payments.Evidence{}, err
	}
	if entry.UserID != scope.UserID {
		return payments.Evidence{}, pgx.ErrNoRows
	}
	pass := entry.Pass
	if pass == nil || pass.Currency != args.Currency || pass.CurrencyScale != args.CurrencyScale {
		return payments.Evidence{}, payments.ErrInvalid
	}
	due, err := payments.Deadline(*pass, args.Purpose)
	if err != nil {
		return payments.Evidence{}, err
	}
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
		return payments.Evidence{}, err
	}

	if err = paymentSourceAvailable(ctx, q, scope, file.UniqueID); err != nil {
		return payments.Evidence{}, err
	}
	row, err := q.InsertPaymentEvidence(
		ctx,
		sqlc.InsertPaymentEvidenceParams{
			ID:             uuid.New(),
			EventID:        args.EventID,
			PassID:         args.PassID,
			UserID:         scope.UserID,
			FileID:         args.FileID,
			SourceUniqueID: file.UniqueID,
			AmountMinor:    args.AmountMinor,
			Currency:       args.Currency,
			CurrencyScale:  args.CurrencyScale,
			Purpose:        args.Purpose,
			SentAt:         file.SentAt,
			DueAt:          pgtype.Timestamptz{Time: due, Valid: true},
		},
	)
	if err != nil {
		return payments.Evidence{}, paymentStorageError(err)
	}
	result := evidenceRecord(row, payments.Pending)
	if err = createPaymentNotifications(ctx, q, scope, result, "submitted", nil); err != nil {
		return payments.Evidence{}, err
	}
	if err = savePaymentReceipt(ctx, q, scope, operation, paymentSubmission, hash[:], result); err != nil {
		return payments.Evidence{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return payments.Evidence{}, err
	}
	return result, nil
}

type paymentReviewTarget struct {
	Evidence payments.Evidence
	Pass     registrations.Pass
	Balance  payments.Balance
	Role     string
	Timezone string
}

func loadPaymentReview(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args payments.ReviewChange,
) (paymentReviewTarget, error) {
	event, role, err := lockPaymentEvent(ctx, q, scope, args.EventID, true)
	if err != nil {
		return paymentReviewTarget{}, err
	}
	row, err := q.ReadPaymentEvidence(ctx, sqlc.ReadPaymentEvidenceParams{ID: args.EvidenceID, EventID: args.EventID})
	if err != nil {
		return paymentReviewTarget{}, err
	}
	evidence := evidenceRecord(row.ShuttlePaymentEvidence, row.State)
	if evidence.State != payments.Pending {
		return paymentReviewTarget{}, ErrConflict
	}
	entry, err := paymentPass(ctx, q, scope, args.EventID, evidence.PassID)
	if err != nil {
		return paymentReviewTarget{}, err
	}
	if entry.Pass == nil || entry.UserID != evidence.UserID {
		return paymentReviewTarget{}, payments.ErrInvalid
	}
	balance, err := paymentBalance(ctx, q, *entry.Pass)
	if err != nil {
		return paymentReviewTarget{}, err
	}
	if balance.Version != args.ExpectedVersion {
		return paymentReviewTarget{}, ErrConflict
	}
	initialMissing := evidence.Purpose == payments.TopUp && !balance.DepositSatisfied
	if args.Decision == payments.Confirmed &&
		(args.AmountMinor > balance.RemainingMinor || (!evidence.Timely || initialMissing) && args.Reason == "") {
		return paymentReviewTarget{}, payments.ErrInvalid
	}
	return paymentReviewTarget{
		Evidence: evidence,
		Pass:     *entry.Pass,
		Balance:  balance,
		Role:     role,
		Timezone: event.Timezone,
	}, nil
}
func paymentPreparationKey(scope PaymentScope, op string) sqlc.GetPaymentPreparationParams {
	return sqlc.GetPaymentPreparationParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		OperationID:    op,
	}
}

// PreparePaymentReview discloses the evidence, frozen currency, deadline and
// outstanding balance. The proposal expires independently of event closing.
func (s *Store) PreparePaymentReview(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args payments.ReviewChange,
) (PaymentPreparation, error) {
	normalized, err := args.Normalize()
	if err != nil || !paymentScopeValid(scope, op) {
		return PaymentPreparation{}, payments.ErrInvalid
	}
	args = normalized
	raw, err := json.Marshal(args)
	if err != nil {
		return PaymentPreparation{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentPreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, replayed, err := preparePaymentReview(ctx, s.q.WithTx(tx), scope, op, args, hash)
	if err != nil || replayed {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PaymentPreparation{}, err
	}
	return result, nil
}

// preparePaymentReview uses the caller's transaction and normalized arguments.
// The boolean reports an existing preparation, with its original expiry.
func preparePaymentReview(
	ctx context.Context, q *sqlc.Queries, scope PaymentScope, op string,
	args payments.ReviewChange, hash [sha256.Size]byte,
) (PaymentPreparation, bool, error) {
	if err := q.LockEventOperation(ctx, paymentOperationLock(scope, op)); err != nil {
		return PaymentPreparation{}, false, err
	}
	if _, _, err := lockPaymentEvent(ctx, q, scope, args.EventID, true); err != nil {
		return PaymentPreparation{}, false, err
	}
	prior, err := q.GetPaymentPreparation(ctx, paymentPreparationKey(scope, op))
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash[:]) {
			return PaymentPreparation{}, false, ErrConflict
		}
		var result PaymentPreparation
		err = json.Unmarshal(prior.Document, &result)
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentPreparation{}, false, err
	}
	target, err := loadPaymentReview(ctx, q, scope, args)
	if err != nil {
		return PaymentPreparation{}, false, err
	}
	zone, err := time.LoadLocation(target.Timezone)
	if err != nil {
		return PaymentPreparation{}, false, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return PaymentPreparation{}, false, err
	}
	presentation, err := q.ReadPaymentReviewPresentation(ctx, sqlc.ReadPaymentReviewPresentationParams{
		ActorID: scope.UserID, EvidenceID: args.EvidenceID, EventID: args.EventID,
		BotID: scope.BotID, Organization: scope.Organization, Subject: scope.Subject,
	})
	if err != nil {
		return PaymentPreparation{}, false, err
	}
	result := PaymentPreparation{
		ID: uuid.New(), Arguments: args, ExpiresAt: now.Time.UTC().Add(paymentPreparationTTL),
		Description: paymentReviewDescription(args, target, presentation, zone),
	}
	doc, err := json.Marshal(result)
	if err != nil {
		return PaymentPreparation{}, false, err
	}
	err = q.InsertPaymentPreparation(
		ctx,
		sqlc.InsertPaymentPreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    op,
			ProposalID:     result.ID,
			RequestHash:    hash[:],
			ExpiresAt:      pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
			Document:       doc,
		},
	)
	if err != nil {
		return PaymentPreparation{}, false, err
	}
	return result, false, nil
}

func paymentProposalExpiry(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	op string,
	proposal uuid.UUID,
	hash []byte,
) (time.Time, error) {
	row, err := q.GetPaymentPreparation(ctx, paymentPreparationKey(scope, op))
	if err != nil {
		return time.Time{}, err
	}
	if row.ProposalID != proposal || !bytes.Equal(row.RequestHash, hash) {
		return time.Time{}, ErrConflict
	}
	return row.ExpiresAt.Time, nil
}

// ReviewPaymentEvidence rechecks current authority and all money invariants under
// the event lock, then commits decision, ledger revision and original receipt.
func (s *Store) ReviewPaymentEvidence(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args payments.ReviewChange,
	proposal uuid.UUID,
) (payments.ReviewResult, error) {
	normalized, err := args.Normalize()
	if err != nil || !paymentScopeValid(scope, op) || proposal == uuid.Nil {
		return payments.ReviewResult{}, payments.ErrInvalid
	}
	args = normalized
	raw, err := json.Marshal(args)
	if err != nil {
		return payments.ReviewResult{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return payments.ReviewResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, replayed, err := reviewPaymentEvidence(ctx, s.q.WithTx(tx), scope, op, args, proposal, hash)
	if err != nil || replayed {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return payments.ReviewResult{}, err
	}
	return result, nil
}

// reviewPaymentEvidence applies a normalized, validated decision in the caller's
// transaction. The caller must commit all related state together. It acquires the
// canonical operation lock before event, actor and payment-target locks.
func reviewPaymentEvidence(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	op string,
	args payments.ReviewChange,
	proposal uuid.UUID,
	hash [sha256.Size]byte,
) (payments.ReviewResult, bool, error) {
	if err := q.LockEventOperation(ctx, paymentOperationLock(scope, op)); err != nil {
		return payments.ReviewResult{}, false, err
	}
	if _, _, err := lockPaymentEvent(ctx, q, scope, args.EventID, true); err != nil {
		return payments.ReviewResult{}, false, err
	}
	expires, err := paymentProposalExpiry(ctx, q, scope, op, proposal, hash[:])
	if err != nil {
		return payments.ReviewResult{}, false, err
	}
	var prior payments.ReviewResult
	found, err := paymentReceipt(ctx, q, scope, op, paymentReview, hash[:], &prior)
	if found || err != nil {
		return prior, found, err
	}
	gate, err := q.ReadPaymentReviewWorkspaceGate(ctx, sqlc.ReadPaymentReviewWorkspaceGateParams{
		OperationID: op, UserID: scope.UserID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return payments.ReviewResult{}, false, err
	}
	if err == nil && (gate.State != paymentWorkspacePrepared || !gate.IsCurrent) {
		return payments.ReviewResult{}, false, ErrConflict
	}
	target, err := loadPaymentReview(ctx, q, scope, args)
	if err != nil {
		return payments.ReviewResult{}, false, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return payments.ReviewResult{}, false, err
	}
	if !now.Time.Before(expires) {
		return payments.ReviewResult{}, false, ErrConflict
	}
	row, err := q.InsertPaymentReview(
		ctx,
		sqlc.InsertPaymentReviewParams{
			ID:            uuid.New(),
			EvidenceID:    args.EvidenceID,
			ActorID:       scope.UserID,
			ActorSubject:  scope.Subject,
			AuthorityRole: target.Role,
			Decision:      args.Decision,
			AmountMinor:   args.AmountMinor,
			Reason:        args.Reason,
		},
	)
	if err != nil {
		return payments.ReviewResult{}, false, paymentStorageError(err)
	}
	if err = q.AdvancePaymentVersion(ctx, target.Pass.ID); err != nil {
		return payments.ReviewResult{}, false, err
	}
	balance, err := paymentBalance(ctx, q, target.Pass)
	if err != nil {
		return payments.ReviewResult{}, false, err
	}
	result := paymentReviewResult(target, balance, row)
	if err = createPaymentNotifications(ctx, q, scope, result.Evidence, args.Decision, &result.Balance); err != nil {
		return payments.ReviewResult{}, false, err
	}
	if err = savePaymentReceipt(ctx, q, scope, op, paymentReview, hash[:], result); err != nil {
		return payments.ReviewResult{}, false, err
	}
	if err = q.CompletePaymentReviewWorkspace(ctx, sqlc.CompletePaymentReviewWorkspaceParams{
		OperationID: op, UserID: scope.UserID,
	}); err != nil {
		return payments.ReviewResult{}, false, err
	}
	return result, false, nil
}

// PaymentBalance returns the owner's balance or an event-authorized audit read.
func (s *Store) PaymentBalance(
	ctx context.Context,
	scope PaymentScope,
	eventID, passID uuid.UUID,
) (payments.Balance, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return payments.Balance{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, _, err = lockPaymentEvent(ctx, q, scope, eventID, false); err != nil {
		return payments.Balance{}, err
	}
	entry, err := paymentPass(ctx, q, scope, eventID, passID)
	if err != nil {
		return payments.Balance{}, err
	}
	if entry.UserID != scope.UserID {
		if _, _, err = lockPaymentEvent(ctx, q, scope, eventID, true); err != nil {
			return payments.Balance{}, err
		}
	}
	if entry.Pass == nil {
		return payments.Balance{}, pgx.ErrNoRows
	}
	return paymentBalance(ctx, q, *entry.Pass)
}

func scopedPaymentEvidence(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	eventID, evidenceID uuid.UUID,
) (payments.Evidence, error) {
	if _, _, err := lockPaymentEvent(ctx, q, scope, eventID, false); err != nil {
		return payments.Evidence{}, err
	}
	row, err := q.ReadPaymentEvidence(ctx, sqlc.ReadPaymentEvidenceParams{ID: evidenceID, EventID: eventID})
	if err != nil {
		return payments.Evidence{}, err
	}
	if row.ShuttlePaymentEvidence.UserID != scope.UserID {
		if _, _, err = lockPaymentEvent(ctx, q, scope, eventID, true); err != nil {
			return payments.Evidence{}, err
		}
	}
	// No general cross-owner file access is granted by this association.
	return reviewedEvidenceRecord(row.ShuttlePaymentEvidence, row.State, row.Review)
}

func (s *Store) PaymentEvidence(
	ctx context.Context,
	scope PaymentScope,
	eventID, evidenceID uuid.UUID,
) (payments.Evidence, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return payments.Evidence{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return scopedPaymentEvidence(ctx, s.q.WithTx(tx), scope, eventID, evidenceID)
}

// PaymentEvidenceFile is separate from the owner-only general file API. Callers
// must recheck this association and current authority after a remote content read.
func (s *Store) PaymentEvidenceFile(
	ctx context.Context,
	scope PaymentScope,
	eventID, evidenceID uuid.UUID,
) (File, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, err = scopedPaymentEvidence(ctx, q, scope, eventID, evidenceID); err != nil {
		return File{}, err
	}
	row, err := q.GetPaymentEvidenceFile(ctx, sqlc.GetPaymentEvidenceFileParams{ID: evidenceID, EventID: eventID})
	if err != nil {
		return File{}, err
	}
	return storedFile(row), nil
}

// HasPaymentAuthority is discovery only. Every call rechecks its event role.
func (s *Store) HasPaymentAuthority(ctx context.Context, scope PaymentScope) (bool, error) {
	actor, err := s.q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return false, err
	}
	if actor.ID != scope.UserID {
		return false, payments.ErrDenied
	}
	if scope.GlobalAdmin {
		return true, nil
	}
	return s.q.HasEventPaymentAuthority(
		ctx,
		sqlc.HasEventPaymentAuthorityParams{
			UserID:         scope.UserID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
}

type PaymentReceipt struct {
	Kind     string                 `json:"kind"`
	Evidence *payments.Evidence     `json:"evidence,omitempty"`
	Review   *payments.ReviewResult `json:"review,omitempty"`
}

// PaymentOperation reads an actor's historical receipt after checking current
// access to its associated evidence. Missing receipts do not prove non-execution.
func (s *Store) PaymentOperation(ctx context.Context, scope PaymentScope, operation string) (PaymentReceipt, error) {
	if !paymentScopeValid(scope, operation) {
		return PaymentReceipt{}, payments.ErrInvalid
	}
	row, err := s.q.GetPaymentOperation(ctx, paymentOperationKey(scope, operation))
	if err != nil {
		return PaymentReceipt{}, err
	}
	result := PaymentReceipt{Kind: row.Kind}
	var evidence payments.Evidence
	switch row.Kind {
	case paymentSubmission:
		result.Evidence = &payments.Evidence{}
		if err = json.Unmarshal(row.Result, result.Evidence); err != nil {
			return PaymentReceipt{}, err
		}
		evidence = *result.Evidence
	case paymentReview:
		result.Review = &payments.ReviewResult{}
		if err = json.Unmarshal(row.Result, result.Review); err != nil {
			return PaymentReceipt{}, err
		}
		evidence = result.Review.Evidence
	default:
		return PaymentReceipt{}, payments.ErrInvalid
	}
	if _, err = s.PaymentEvidence(ctx, scope, evidence.EventID, evidence.ID); err != nil {
		return PaymentReceipt{}, err
	}
	return result, nil
}

// PaymentEvidenceList defaults to the caller's evidence. Auditor mode requires
// current authority for that event; it never includes another event's records.
func (s *Store) PaymentEvidenceList(
	ctx context.Context,
	scope PaymentScope,
	eventID, after uuid.UUID,
	limit int32,
	audit bool,
) ([]payments.Evidence, error) {
	if limit < 1 || limit > 20 {
		return nil, payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if _, _, err = lockPaymentEvent(ctx, q, scope, eventID, audit); err != nil {
		return nil, err
	}
	owner := scope.UserID
	if audit {
		owner = uuid.Nil
	}
	rows, err := q.ListPaymentEvidence(
		ctx,
		sqlc.ListPaymentEvidenceParams{EventID: eventID, OwnerID: owner, AfterID: after, PageLimit: limit},
	)
	if err != nil {
		return nil, err
	}
	result := make([]payments.Evidence, 0, len(rows))
	for _, row := range rows {
		record, decodeErr := reviewedEvidenceRecord(row.ShuttlePaymentEvidence, row.State, row.Review)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, record)
	}
	return result, nil
}

// PaymentFile resolves only a submitted proof's association in this deployment.
// External artifact refresh uses evidence IDs, never a foreign owner's file ID.
func (s *Store) PaymentFile(ctx context.Context, scope PaymentScope, evidenceID uuid.UUID) (File, error) {
	eventID, err := s.q.PaymentEvidenceEvent(
		ctx,
		sqlc.PaymentEvidenceEventParams{ID: evidenceID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return File{}, err
	}
	return s.PaymentEvidenceFile(ctx, scope, eventID, evidenceID)
}

func paymentSourceAvailable(ctx context.Context, q *sqlc.Queries, scope PaymentScope, source string) error {
	if err := q.LockEventOperation(
		ctx,
		fmt.Sprintf("payment-source:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.UserID, source),
	); err != nil {
		return err
	}
	used, err := q.HasUnrejectedPaymentSource(
		ctx,
		sqlc.HasUnrejectedPaymentSourceParams{UserID: scope.UserID, SourceUniqueID: source},
	)
	if err != nil {
		return err
	}
	if used {
		return ErrConflict
	}
	return nil
}

// PaymentEventChoice makes assigned expectations discoverable even after sales
// close. Own pass terms are private; other users' passes are never listed here.
type PaymentEventChoice struct {
	ID       uuid.UUID           `json:"id"`
	Name     string              `json:"name"`
	Timezone string              `json:"timezone"`
	StartsAt time.Time           `json:"starts_at"`
	EndsAt   time.Time           `json:"ends_at"`
	OwnPass  *registrations.Pass `json:"own_pass,omitempty"`
}

func (s *Store) PaymentEvents(
	ctx context.Context,
	scope PaymentScope,
	after uuid.UUID,
	limit int32,
) ([]PaymentEventChoice, error) {
	if limit < 1 || limit > 20 {
		return nil, payments.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	rows, err := q.ListPaymentEvents(
		ctx,
		sqlc.ListPaymentEventsParams{
			UserID:         scope.UserID,
			Subject:        scope.Subject,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			GlobalAdmin:    scope.GlobalAdmin,
			AfterID:        after,
			PageLimit:      limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]PaymentEventChoice, 0, len(rows))
	for _, row := range rows {
		choice := PaymentEventChoice{
			ID:       row.ShuttleEvent.ID,
			Name:     row.ShuttleEvent.Name,
			Timezone: row.ShuttleEvent.Timezone,
			StartsAt: row.ShuttleEvent.StartsAt.Time.UTC(),
			EndsAt:   row.ShuttleEvent.EndsAt.Time.UTC(),
		}
		if row.OwnPassID != uuid.Nil {
			entry, readErr := q.ReadPaymentPass(ctx, sqlc.ReadPaymentPassParams{ID: row.OwnPassID, EventID: choice.ID})
			if readErr != nil {
				return nil, readErr
			}
			registration, decodeErr := registrationRecord(ctx, q, entry)
			if decodeErr != nil {
				return nil, decodeErr
			}
			choice.OwnPass = registration.Pass
		}
		result = append(result, choice)
	}
	return result, nil
}

func paymentReviewResult(
	target paymentReviewTarget,
	balance payments.Balance,
	row sqlc.ShuttlePaymentReview,
) payments.ReviewResult {
	target.Evidence.State = row.Decision
	result := payments.ReviewResult{
		Evidence: target.Evidence,
		Balance:  balance,
		Review: payments.Review{
			ID:            row.ID,
			EvidenceID:    row.EvidenceID,
			ActorID:       row.ActorID,
			AuthorityRole: row.AuthorityRole,
			Decision:      row.Decision,
			AmountMinor:   row.AmountMinor,
			Reason:        row.Reason,
			ReviewedAt:    row.ReviewedAt.Time.UTC(),
		},
	}
	result.Evidence.Review = &result.Review
	return result
}
