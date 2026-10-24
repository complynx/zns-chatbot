package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const paymentWorkspaceCancelled = "cancelled"

// PaymentReviewWorkspace is one immutable prepared revision, not approval.
// A replacement has a new ID and a greater revision for the same actor/evidence.
type PaymentReviewWorkspace struct {
	ID          uuid.UUID          `json:"workspace_id"`
	EventID     uuid.UUID          `json:"event_id"`
	EvidenceID  uuid.UUID          `json:"evidence_id"`
	Revision    int64              `json:"revision"`
	State       string             `json:"state"`
	Current     bool               `json:"current"`
	Preparation PaymentPreparation `json:"preparation"`
}

type PaymentReviewDraftChange struct {
	Review           payments.ReviewChange `json:"review"`
	ExpectedRevision int64                 `json:"expected_revision" minimum:"0"`
}

type PaymentReviewWorkspaceChange struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Revision    int64     `json:"revision"     minimum:"1"`
}

// PaymentReviewCommitChange repeats the exact review so approval discloses the
// decision, money, reason and balance version, rather than an opaque ID alone.
type PaymentReviewCommitChange struct {
	WorkspaceID uuid.UUID             `json:"workspace_id"`
	Revision    int64                 `json:"revision"     minimum:"1"`
	Review      payments.ReviewChange `json:"review"`
}

type PaymentReviewCommitPreparation struct {
	ID          uuid.UUID                 `json:"proposal_id"`
	Arguments   PaymentReviewCommitChange `json:"arguments"`
	Description string                    `json:"description"`
	ExpiresAt   time.Time                 `json:"expires_at"`
}

type PaymentWorkspaceReviewResult struct {
	CanonicalOperationID string                `json:"canonical_operation_id"`
	Review               payments.ReviewResult `json:"review"`
}

type PaymentReviewWorkspaceReceipt struct {
	Kind              string                          `json:"kind"`
	Completed         bool                            `json:"completed"`
	UnavailableReason string                          `json:"unavailable_reason,omitempty"`
	Workspace         *PaymentReviewWorkspace         `json:"workspace,omitempty"`
	Review            *PaymentWorkspaceReviewResult   `json:"review,omitempty"`
	Preparation       *PaymentReviewCommitPreparation `json:"preparation,omitempty"`
}

type paymentReviewCall struct {
	Source      string
	Kind        string
	Hash        []byte
	WorkspaceID uuid.UUID
	Proposal    *uuid.UUID
	Expires     *time.Time
	Result      []byte
}

func reviewWorkspaceHash(value any) ([sha256.Size]byte, error) {
	raw, err := json.Marshal(value)
	return sha256.Sum256(raw), err
}

func lockReviewCall(ctx context.Context, q *sqlc.Queries, scope PaymentScope, op string) error {
	return q.LockEventOperation(
		ctx,
		fmt.Sprintf("payment-review-call:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, op),
	)
}

func readReviewCall(ctx context.Context, tx pgx.Tx, scope PaymentScope, op string) (paymentReviewCall, error) {
	var c paymentReviewCall
	err := tx.QueryRow(ctx, `SELECT kind,request_hash,workspace_id,proposal_id,expires_at,result,source
 FROM shuttle_payment_review_calls WHERE actor_id=$1 AND operation_id=$2`, scope.UserID, op).
		Scan(&c.Kind, &c.Hash, &c.WorkspaceID, &c.Proposal, &c.Expires, &c.Result, &c.Source)
	if err == nil && c.Expires != nil {
		expiry := c.Expires.UTC()
		c.Expires = &expiry
	}
	return c, err
}

func matchReviewCall(c paymentReviewCall, kind string, hash [sha256.Size]byte, source string) error {
	if c.Source != source || c.Kind != kind || !bytes.Equal(c.Hash, hash[:]) {
		return ErrConflict
	}
	return nil
}

func saveReviewCall(ctx context.Context, tx pgx.Tx, scope PaymentScope, op, kind string,
	hash [sha256.Size]byte, workspace uuid.UUID, result any, source string) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_payment_review_calls
 (actor_id,operation_id,kind,request_hash,workspace_id,result,source,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp())`, scope.UserID, op, kind, hash[:], workspace, raw, source)
	return err
}

// readReviewWorkspace never accepts the actor from a workspace or caller argument.
func readReviewWorkspace(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	id uuid.UUID,
) (PaymentReviewWorkspace, error) {
	var w PaymentReviewWorkspace
	var doc []byte
	err := tx.QueryRow(ctx, `SELECT w.id,w.event_id,w.evidence_id,w.revision,w.state,p.document,
 NOT EXISTS(SELECT 1 FROM shuttle_payment_review_workspaces newer WHERE newer.actor_id=w.actor_id
 AND newer.evidence_id=w.evidence_id AND newer.revision>w.revision)
 FROM shuttle_payment_review_workspaces w
 JOIN shuttle_payment_preparations p ON p.proposal_id=w.proposal_id
 WHERE w.id=$1 AND w.actor_id=$2 AND p.bot_id=$3 AND p.organization_id=$4 AND p.actor=$5`,
		id, scope.UserID, scope.BotID, scope.Organization, scope.Subject).
		Scan(&w.ID, &w.EventID, &w.EvidenceID, &w.Revision, &w.State, &doc, &w.Current)
	if err == nil {
		err = json.Unmarshal(doc, &w.Preparation)
	}
	return w, err
}

// lockReviewWorkspace takes canonical operation -> event -> actor locks before
// any workspace state. All review/cancellation paths share the event lock.
func lockReviewWorkspace(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	id uuid.UUID,
) (PaymentReviewWorkspace, error) {
	w, err := readReviewWorkspace(ctx, tx, scope, id)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	q := sqlc.New(tx)
	if err = q.LockEventOperation(ctx, paymentOperationLock(scope, id.String())); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if _, _, err = lockPaymentEvent(ctx, q, scope, w.EventID, true); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	return readReviewWorkspace(ctx, tx, scope, id)
}

// PaymentReviewWorkspace reads an exact revision with current event authority.
func (s *Store) PaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
) (PaymentReviewWorkspace, error) {
	if !paymentScopeValid(scope, "read") || id == uuid.Nil {
		return PaymentReviewWorkspace{}, payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return lockReviewWorkspace(ctx, tx, scope, id)
}

// CurrentPaymentReviewWorkspace discovers this actor's latest evidence decision.
func (s *Store) CurrentPaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	eventID, evidenceID uuid.UUID,
) (PaymentReviewWorkspace, error) {
	if !paymentScopeValid(scope, "read") || eventID == uuid.Nil || evidenceID == uuid.Nil {
		return PaymentReviewWorkspace{}, payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if _, _, err = lockPaymentEvent(ctx, s.q.WithTx(tx), scope, eventID, true); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, "SELECT id FROM shuttle_payment_review_workspaces WHERE actor_id=$1 AND e" +
"vent_id=$2 AND evidence_id=$3 ORDER BY revision DESC LIMIT 1", scope.UserID, eventID, evidenceID).
		Scan(&id)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	return readReviewWorkspace(ctx, tx, scope, id)
}

// PreparePaymentReviewWorkspace saves intent only. Retrying op returns its
// original snapshot; changing the exact decision requires a new op and revision.
func (s *Store) PreparePaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PaymentReviewDraftChange,
) (PaymentReviewWorkspace, error) {
	return s.preparePaymentReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{})
}

func (s *Store) preparePaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PaymentReviewDraftChange,
	origin paymentReviewOrigin,
) (PaymentReviewWorkspace, error) {
	normalized, err := args.Review.Normalize()
	if err != nil || !paymentScopeValid(scope, op) || args.ExpectedRevision < 0 {
		return PaymentReviewWorkspace{}, payments.ErrInvalid
	}
	args.Review = normalized
	hash, err := reviewWorkspaceHash(args)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	if err = lockReviewCall(ctx, q, scope, op); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	call, err := readReviewCall(ctx, tx, scope, op)
	if err == nil {
		if err = matchReviewCall(call, "prepare", hash, origin.source()); err != nil {
			return PaymentReviewWorkspace{}, err
		}
		return replayReviewWorkspace(ctx, tx, scope, call, origin)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentReviewWorkspace{}, err
	}
	id := uuid.New()
	if err = q.LockEventOperation(ctx, paymentOperationLock(scope, id.String())); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if _, _, err = lockPaymentEvent(ctx, q, scope, args.Review.EventID, true); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if err = origin.authorize(ctx, tx, scope); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	var latest int64
	err = tx.QueryRow(ctx, "SELECT COALESCE(max(revision),0) FROM shuttle_payment_review_workspaces " +
"WHERE actor_id=$1 AND evidence_id=$2", scope.UserID, args.Review.EvidenceID).
		Scan(&latest)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if latest != args.ExpectedRevision {
		return PaymentReviewWorkspace{}, ErrConflict
	}
	reviewHash, err := reviewWorkspaceHash(args.Review)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	prep, _, err := preparePaymentReview(ctx, q, scope, id.String(), args.Review, reviewHash)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	w := PaymentReviewWorkspace{
		ID:          id,
		EventID:     args.Review.EventID,
		EvidenceID:  args.Review.EvidenceID,
		Revision:    latest + 1,
		State:       paymentWorkspacePrepared,
		Current:     true,
		Preparation: prep,
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_payment_review_workspaces(id,actor_id,event_id,evide" +
"nce_id,revision,proposal_id,state) VALUES($1,$2,$3,$4,$5,$6,'prepared')",
		id,
		scope.UserID,
		w.EventID,
		w.EvidenceID,
		w.Revision,
		prep.ID,
	)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if err = saveReviewCall(ctx, tx, scope, op, "prepare", hash, id, w, origin.source()); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if err = origin.checkAuthority(); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	return w, tx.Commit(ctx)
}

// replayReviewWorkspace rechecks current authority and session before returning
// the immutable saved-intent receipt, even after a later revision exists.
func replayReviewWorkspace(ctx context.Context, tx pgx.Tx, scope PaymentScope,
	call paymentReviewCall, origin paymentReviewOrigin,
) (PaymentReviewWorkspace, error) {
	if _, err := lockReviewWorkspace(ctx, tx, scope, call.WorkspaceID); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if err := origin.authorize(ctx, tx, scope); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	var result PaymentReviewWorkspace
	err := json.Unmarshal(call.Result, &result)
	return result, err
}

func validWorkspaceChange(id uuid.UUID, revision int64) bool {
	return id != uuid.Nil && revision > 0
}

func pendingReviewWorkspace(w PaymentReviewWorkspace, revision int64) error {
	if w.Revision != revision || !w.Current || w.State != paymentWorkspacePrepared {
		return ErrConflict
	}
	return nil
}

func (s *Store) CancelPaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PaymentReviewWorkspaceChange,
) (PaymentReviewWorkspace, error) {
	return s.cancelPaymentReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{})
}

func (s *Store) cancelPaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PaymentReviewWorkspaceChange,
	origin paymentReviewOrigin,
) (PaymentReviewWorkspace, error) {
	if !paymentScopeValid(scope, op) || !validWorkspaceChange(args.WorkspaceID, args.Revision) {
		return PaymentReviewWorkspace{}, payments.ErrInvalid
	}
	hash, err := reviewWorkspaceHash(args)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockReviewCall(ctx, s.q.WithTx(tx), scope, op); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	w, err := lockReviewWorkspace(ctx, tx, scope, args.WorkspaceID)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if err = origin.authorize(ctx, tx, scope); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	call, err := readReviewCall(ctx, tx, scope, op)
	if err == nil {
		if err = matchReviewCall(call, "cancel", hash, origin.source()); err != nil {
			return PaymentReviewWorkspace{}, err
		}
		var result PaymentReviewWorkspace
		err = json.Unmarshal(call.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentReviewWorkspace{}, err
	}
	if err = pendingReviewWorkspace(w, args.Revision); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	// A canonical receipt may exist after a direct domain call. Never present money as cancelled.
	var prior payments.ReviewResult
	reviewHash, err := reviewWorkspaceHash(w.Preparation.Arguments)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	found, err := paymentReceipt(ctx, s.q.WithTx(tx), scope, w.ID.String(), paymentReview, reviewHash[:], &prior)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if found {
		return PaymentReviewWorkspace{}, ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE shuttle_payment_review_workspaces SET state='cancelled' WHERE id=$1`, w.ID)
	if err != nil {
		return PaymentReviewWorkspace{}, err
	}
	w.State = paymentWorkspaceCancelled
	if err = saveReviewCall(ctx, tx, scope, op, "cancel", hash, w.ID, w, origin.source()); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	if err = origin.checkAuthority(); err != nil {
		return PaymentReviewWorkspace{}, err
	}
	return w, tx.Commit(ctx)
}

func checkCommitWorkspace(w PaymentReviewWorkspace, args PaymentReviewCommitChange) error {
	if w.ID != args.WorkspaceID || w.Revision != args.Revision || w.Preparation.Arguments != args.Review {
		return ErrConflict
	}
	return nil
}

func (s *Store) PreparePaymentReviewCommit(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PaymentReviewCommitChange,
) (PaymentReviewCommitPreparation, error) {
	normalized, err := args.Review.Normalize()
	if err != nil || !paymentScopeValid(scope, op) || !validWorkspaceChange(args.WorkspaceID, args.Revision) {
		return PaymentReviewCommitPreparation{}, payments.ErrInvalid
	}
	args.Review = normalized
	hash, err := reviewWorkspaceHash(args)
	if err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockReviewCall(ctx, s.q.WithTx(tx), scope, op); err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	w, err := lockReviewWorkspace(ctx, tx, scope, args.WorkspaceID)
	if err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	if err = checkCommitWorkspace(w, args); err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	call, err := readReviewCall(ctx, tx, scope, op)
	if err == nil {
		if err = matchReviewCall(call, "commit", hash, paymentReviewAgent); err != nil {
			return PaymentReviewCommitPreparation{}, err
		}
		return PaymentReviewCommitPreparation{
			ID:          *call.Proposal,
			Arguments:   args,
			Description: w.Preparation.Description,
			ExpiresAt:   *call.Expires,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentReviewCommitPreparation{}, err
	}
	if err = pendingReviewWorkspace(w, args.Revision); err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	now, err := s.q.WithTx(tx).RegistrationNow(ctx)
	if err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	if !now.Time.Before(w.Preparation.ExpiresAt) {
		return PaymentReviewCommitPreparation{}, ErrConflict
	}
	result := PaymentReviewCommitPreparation{
		ID:          uuid.New(),
		Arguments:   args,
		Description: w.Preparation.Description,
		ExpiresAt:   w.Preparation.ExpiresAt,
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_payment_review_calls(actor_id,operation_id,kind,requ" +
"est_hash,workspace_id,proposal_id,expires_at,source)\n VALUES($1,$2,'comm" +
"it',$3,$4,$5,$6,'agent')",
		scope.UserID,
		op,
		hash[:],
		w.ID,
		result.ID,
		result.ExpiresAt,
	)
	if err != nil {
		return PaymentReviewCommitPreparation{}, err
	}
	return result, tx.Commit(ctx)
}

// CommitPaymentReviewWorkspace joins a transport approval to its immutable
// canonical decision. Money, notification, workspace and outer receipt commit once.
func (s *Store) CommitPaymentReviewWorkspace(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args PaymentReviewCommitChange,
	proposal uuid.UUID,
) (PaymentWorkspaceReviewResult, error) {
	normalized, err := args.Review.Normalize()
	if err != nil || !paymentScopeValid(scope, op) || !validWorkspaceChange(args.WorkspaceID, args.Revision) ||
		proposal == uuid.Nil {
		return PaymentWorkspaceReviewResult{}, payments.ErrInvalid
	}
	args.Review = normalized
	hash, err := reviewWorkspaceHash(args)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockReviewCall(ctx, s.q.WithTx(tx), scope, op); err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	call, err := readReviewCall(ctx, tx, scope, op)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if err = matchReviewCall(call, "commit", hash, paymentReviewAgent); err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if call.Proposal == nil || *call.Proposal != proposal || call.WorkspaceID != args.WorkspaceID {
		return PaymentWorkspaceReviewResult{}, ErrConflict
	}
	w, err := lockReviewWorkspace(ctx, tx, scope, call.WorkspaceID)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if err = checkCommitWorkspace(w, args); err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if len(call.Result) > 0 {
		var result PaymentWorkspaceReviewResult
		err = json.Unmarshal(call.Result, &result)
		return result, err
	}
	result, err := completeWorkspaceReview(ctx, tx, scope, op, w, args, call)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	return result, tx.Commit(ctx)
}

// completeWorkspaceReview commits no transaction; its caller owns the full boundary.
func completeWorkspaceReview(
	ctx context.Context, tx pgx.Tx, scope PaymentScope, op string,
	w PaymentReviewWorkspace, args PaymentReviewCommitChange, call paymentReviewCall,
) (PaymentWorkspaceReviewResult, error) {
	prior, err := applyWorkspaceReview(ctx, tx, scope, w, args, call)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	result := PaymentWorkspaceReviewResult{CanonicalOperationID: w.ID.String(), Review: prior}
	raw, err := json.Marshal(result)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	_, err = tx.Exec(
		ctx,
		"UPDATE shuttle_payment_review_calls SET result=$3,completed_at=clock_tim" +
"estamp() WHERE actor_id=$1 AND operation_id=$2",
		scope.UserID,
		op,
		raw,
	)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	return result, nil
}

// applyWorkspaceReview recovers a canonical receipt before testing whether a
// new effect is still allowed. It never changes a later workspace revision.
func applyWorkspaceReview(
	ctx context.Context, tx pgx.Tx, scope PaymentScope, w PaymentReviewWorkspace,
	args PaymentReviewCommitChange, call paymentReviewCall,
) (payments.ReviewResult, error) {
	q := sqlc.New(tx)
	reviewHash, err := reviewWorkspaceHash(args.Review)
	if err != nil {
		return payments.ReviewResult{}, err
	}
	var prior payments.ReviewResult
	found, err := paymentReceipt(ctx, q, scope, w.ID.String(), paymentReview, reviewHash[:], &prior)
	if err != nil {
		return payments.ReviewResult{}, err
	}
	if found {
		err = q.CompletePaymentReviewWorkspace(
			ctx,
			sqlc.CompletePaymentReviewWorkspaceParams{OperationID: w.ID.String(), UserID: scope.UserID},
		)
		return prior, err
	}
	if err = pendingReviewWorkspace(w, args.Revision); err != nil {
		return payments.ReviewResult{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return payments.ReviewResult{}, err
	}
	if call.Expires == nil || !now.Time.Before(*call.Expires) {
		return payments.ReviewResult{}, ErrConflict
	}
	result, _, err := reviewPaymentEvidence(ctx, q, scope, w.ID.String(), args.Review, w.Preparation.ID, reviewHash)
	if err != nil {
		return payments.ReviewResult{}, err
	}
	return result, nil
}

// PaymentReviewWorkspaceOperation takes only the original transport operation.
// Completed=false is a prepared intent, never proof of a financial effect.
func (s *Store) PaymentReviewWorkspaceOperation(
	ctx context.Context,
	scope PaymentScope,
	op string,
) (PaymentReviewWorkspaceReceipt, error) {
	return s.paymentReviewWorkspaceOperation(ctx, scope, op, paymentReviewOrigin{})
}

func (s *Store) paymentReviewWorkspaceOperation(
	ctx context.Context,
	scope PaymentScope,
	op string,
	origin paymentReviewOrigin,
) (PaymentReviewWorkspaceReceipt, error) {
	if !paymentScopeValid(scope, op) {
		return PaymentReviewWorkspaceReceipt{}, payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockReviewCall(ctx, s.q.WithTx(tx), scope, op); err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	c, err := readReviewCall(ctx, tx, scope, op)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	w, err := lockReviewWorkspace(ctx, tx, scope, c.WorkspaceID)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	if err = origin.authorize(ctx, tx, scope); err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	result := PaymentReviewWorkspaceReceipt{Kind: c.Kind, Completed: len(c.Result) > 0}
	if !result.Completed {
		return pendingReviewCallReceipt(ctx, s.q.WithTx(tx), scope, w, c)
	}
	if c.Kind == "commit" {
		result.Review = &PaymentWorkspaceReviewResult{}
		err = json.Unmarshal(c.Result, result.Review)
		if err == nil && c.Source == paymentReviewAgent {
			result.Preparation, err = reviewCallPreparation(w, c)
		}
	} else {
		result.Workspace = &PaymentReviewWorkspace{}
		err = json.Unmarshal(c.Result, result.Workspace)
	}
	return result, err
}

// pendingReviewCallReceipt is read-only. Another approved interface may already
// have completed the canonical decision without executing this outer call.
func pendingReviewCallReceipt(
	ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	w PaymentReviewWorkspace, c paymentReviewCall,
) (PaymentReviewWorkspaceReceipt, error) {
	prepared, err := reviewCallPreparation(w, c)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	reviewHash, err := reviewWorkspaceHash(prepared.Arguments.Review)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	var prior payments.ReviewResult
	found, err := paymentReceipt(ctx, q, scope, w.ID.String(), paymentReview, reviewHash[:], &prior)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	if found {
		return PaymentReviewWorkspaceReceipt{Kind: c.Kind, Completed: true, Preparation: prepared,
			Review: &PaymentWorkspaceReviewResult{CanonicalOperationID: w.ID.String(), Review: prior}}, nil
	}
	reason, err := reviewCallUnavailableReason(ctx, q, scope, w, c, reviewHash)
	if err != nil {
		return PaymentReviewWorkspaceReceipt{}, err
	}
	return PaymentReviewWorkspaceReceipt{Kind: c.Kind, Workspace: &w, UnavailableReason: reason,
		Preparation: prepared}, nil
}

// Preserve the original agent consent even when another interface has already
// completed its decision. This read does not prepare or execute a new operation.
func reviewCallPreparation(w PaymentReviewWorkspace, c paymentReviewCall) (*PaymentReviewCommitPreparation, error) {
	args := workspaceCommitArguments(w)
	hash, err := reviewWorkspaceHash(args)
	if err != nil {
		return nil, err
	}
	if err = matchReviewCall(c, "commit", hash, paymentReviewAgent); err != nil {
		return nil, err
	}
	if c.Proposal == nil || c.Expires == nil {
		return nil, ErrConflict
	}
	return &PaymentReviewCommitPreparation{ID: *c.Proposal, Arguments: args,
		Description: w.Preparation.Description, ExpiresAt: *c.Expires}, nil
}

// The event/canonical locks and database clock establish whether a pending call
// can still produce an effect. Missing receipts alone cannot establish that.
func reviewCallUnavailableReason(
	ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	w PaymentReviewWorkspace, c paymentReviewCall, hash [sha256.Size]byte,
) (string, error) {
	if !w.Current {
		return "superseded", nil
	}
	if w.State == paymentWorkspaceCancelled {
		return paymentWorkspaceCancelled, nil
	}
	expires, err := paymentProposalExpiry(ctx, q, scope, w.ID.String(), w.Preparation.ID, hash[:])
	if err != nil {
		return "", err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return "", err
	}
	if !now.Time.Before(expires) || !now.Time.Before(*c.Expires) {
		return "expired", nil
	}
	return "", nil
}

func workspaceCommitArguments(w PaymentReviewWorkspace) PaymentReviewCommitChange {
	return PaymentReviewCommitChange{WorkspaceID: w.ID, Revision: w.Revision, Review: w.Preparation.Arguments}
}
