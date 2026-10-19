package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const (
	paymentReviewAgent   = "agent"
	paymentReviewMiniApp = "miniapp"
)

// MiniAppPaymentSession is supplied by the session middleware after fresh IAM
// verification. The store rechecks it inside every native operation transaction.
type MiniAppPaymentSession struct {
	Bearer     [sha256.Size]byte
	Generation [sha256.Size]byte
}

// MiniAppPaymentReviewCommit binds explicit native confirmation to the displayed
// canonical proposal and full decision, including money and balance version.
type MiniAppPaymentReviewCommit struct {
	Arguments           PaymentReviewCommitChange `json:"arguments"`
	CanonicalProposalID uuid.UUID                 `json:"canonical_proposal_id"`
}

type paymentReviewOrigin struct{ session *MiniAppPaymentSession }

func (o paymentReviewOrigin) source() string {
	if o.session != nil {
		return paymentReviewMiniApp
	}
	return paymentReviewAgent
}

// authorize runs only after event and actor SHARE locks. Never upgrade the actor
// to UPDATE: concurrent reviews for different events would deadlock on upgrades.
func (o paymentReviewOrigin) authorize(ctx context.Context, tx pgx.Tx, scope PaymentScope) error {
	if o.session == nil {
		return nil
	}
	q := sqlc.New(tx)
	owner, err := q.LockAuthorityActor(ctx, sqlc.LockAuthorityActorParams{
		BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMiniAppDenied
	}
	if err != nil {
		return err
	}
	if owner.ID != scope.UserID {
		return ErrMiniAppDenied
	}
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: scope.BotID, BearerHash: o.session.Bearer[:]},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMiniAppDenied
	}
	if err != nil {
		return err
	}
	if initial.UserID != owner.ID {
		return ErrMiniAppDenied
	}
	row, err := q.LockMiniAppSession(ctx, initial.ID)
	if err != nil {
		return err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if row.State != miniAppActive || !hmac.Equal(row.BearerHash, o.session.Bearer[:]) ||
		!miniAppCurrent(row, owner, o.session.Generation, now.Time) {
		return ErrMiniAppDenied
	}
	return nil
}

// PrepareMiniAppPaymentReviewWorkspace saves intent without crediting money.
func (s *Store) PrepareMiniAppPaymentReviewWorkspace(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, op string, args PaymentReviewDraftChange,
) (PaymentReviewWorkspace, error) {
	return s.preparePaymentReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{session: &session})
}

// CancelMiniAppPaymentReviewWorkspace cancels only the saved pending decision.
func (s *Store) CancelMiniAppPaymentReviewWorkspace(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, op string, args PaymentReviewWorkspaceChange,
) (PaymentReviewWorkspace, error) {
	return s.cancelPaymentReviewWorkspace(ctx, scope, op, args, paymentReviewOrigin{session: &session})
}

// CommitMiniAppPaymentReviewWorkspace creates its outer alias, canonical effect,
// notification and completed native action receipt in one transaction. Retrying
// the same operation never refreshes approval or creates another financial effect.
func (s *Store) CommitMiniAppPaymentReviewWorkspace(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, op string, request MiniAppPaymentReviewCommit,
) (PaymentWorkspaceReviewResult, error) {
	normalized, err := request.Arguments.Review.Normalize()
	if err != nil || !paymentScopeValid(scope, op) || request.CanonicalProposalID == uuid.Nil ||
		!validWorkspaceChange(request.Arguments.WorkspaceID, request.Arguments.Revision) {
		return PaymentWorkspaceReviewResult{}, payments.ErrInvalid
	}
	request.Arguments.Review = normalized
	hash, err := reviewWorkspaceHash(request)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := sqlc.New(tx)
	if err = lockReviewCall(ctx, q, scope, op); err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	w, err := lockReviewWorkspace(ctx, tx, scope, request.Arguments.WorkspaceID)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if err = (paymentReviewOrigin{session: &session}).authorize(ctx, tx, scope); err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if err = checkCommitWorkspace(w, request.Arguments); err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	if w.Preparation.ID != request.CanonicalProposalID {
		return PaymentWorkspaceReviewResult{}, ErrConflict
	}
	call, err := readReviewCall(ctx, tx, scope, op)
	if err == nil {
		if err = matchReviewCall(call, "commit", hash, paymentReviewMiniApp); err != nil {
			return PaymentWorkspaceReviewResult{}, err
		}
		if call.WorkspaceID != w.ID {
			return PaymentWorkspaceReviewResult{}, ErrConflict
		}
		if len(call.Result) > 0 {
			var result PaymentWorkspaceReviewResult
			err = json.Unmarshal(call.Result, &result)
			return result, err
		}
		// Native confirmation never persists an incomplete alias.
		return PaymentWorkspaceReviewResult{}, ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PaymentWorkspaceReviewResult{}, err
	}
	outer := uuid.New()
	expires := w.Preparation.ExpiresAt
	call = paymentReviewCall{
		Source:      paymentReviewMiniApp,
		Kind:        "commit",
		Hash:        hash[:],
		WorkspaceID: w.ID,
		Proposal:    &outer,
		Expires:     &expires,
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_payment_review_calls
 (actor_id,operation_id,kind,request_hash,workspace_id,proposal_id,expires_at,source)
 VALUES($1,$2,'commit',$3,$4,$5,$6,'miniapp')`, scope.UserID, op, hash[:], w.ID, outer, expires)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	// Recover canonical completion before new-effect expiry/current-revision gates.
	result, err := completeWorkspaceReview(ctx, tx, scope, op, w, request.Arguments, call)
	if err != nil {
		return PaymentWorkspaceReviewResult{}, err
	}
	return result, tx.Commit(ctx)
}

// MiniAppPaymentReviewWorkspaceOperation allows authorized recovery from either
// transport. Provenance remains immutable; reading a receipt never records consent.
func (s *Store) MiniAppPaymentReviewWorkspaceOperation(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, op string,
) (PaymentReviewWorkspaceReceipt, error) {
	return s.paymentReviewWorkspaceOperation(ctx, scope, op, paymentReviewOrigin{session: &session})
}

// CheckMiniAppPaymentReview checks reviewer authority and the current session
// together. Read adapters call it before disclosure, and around remote proof IO.
func (s *Store) CheckMiniAppPaymentReview(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, eventID uuid.UUID,
) error {
	if !paymentScopeValid(scope, "read") || eventID == uuid.Nil {
		return payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if _, _, err = lockPaymentEvent(ctx, sqlc.New(tx), scope, eventID, true); err != nil {
		return err
	}
	return (paymentReviewOrigin{session: &session}).authorize(ctx, tx, scope)
}

// PaymentReviewEvents lists only current event reviewers, never ordinary pass
// owners. GlobalAdmin must come from a verified IAM principal.
func (s *Store) PaymentReviewEvents(
	ctx context.Context,
	scope PaymentScope,
	after uuid.UUID,
) (events.ContextList, error) {
	if !paymentScopeValid(scope, "read") {
		return events.ContextList{}, payments.ErrInvalid
	}
	rows, err := s.q.ListPaymentReviewEvents(ctx, sqlc.ListPaymentReviewEventsParams{
		UserID: scope.UserID, Subject: scope.Subject, BotID: scope.BotID, OrganizationID: scope.Organization,
		GlobalAdmin: scope.GlobalAdmin, AfterID: after, PageLimit: events.ContextChoiceLimit + 1,
	})
	if err != nil {
		return events.ContextList{}, err
	}
	result := events.ContextList{Events: []events.ContextEvent{}}
	more := len(rows) > events.ContextChoiceLimit
	if more {
		rows = rows[:events.ContextChoiceLimit]
	}
	for _, row := range rows {
		result.Events = append(result.Events, events.ContextEvent{ID: row.ID, Version: row.Version, Name: row.Name,
			Timezone: row.Timezone, StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC()})
	}
	if more {
		result.Next = &result.Events[len(result.Events)-1].ID
	}
	return result, nil
}

// MiniAppPaymentReviewEvent resolves an event for a recovered or linked review.
// The same transaction checks reviewer authority and the active browser session.
func (s *Store) MiniAppPaymentReviewEvent(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, eventID uuid.UUID,
) (events.ContextEvent, error) {
	if !paymentScopeValid(scope, "read") || eventID == uuid.Nil {
		return events.ContextEvent{}, payments.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return events.ContextEvent{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	event, _, err := lockPaymentEvent(ctx, sqlc.New(tx), scope, eventID, true)
	if err != nil {
		return events.ContextEvent{}, err
	}
	if err = (paymentReviewOrigin{session: &session}).authorize(ctx, tx, scope); err != nil {
		return events.ContextEvent{}, err
	}
	return events.ContextEvent{
		ID:       event.ID,
		Version:  event.Version,
		Name:     event.Name,
		Timezone: event.Timezone,
		StartsAt: event.StartsAt.Time.UTC(),
		EndsAt:   event.EndsAt.Time.UTC(),
	}, nil
}
