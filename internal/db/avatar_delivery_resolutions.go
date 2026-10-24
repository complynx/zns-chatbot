package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const avatarResolutionPreparationTTL = 5 * time.Minute

// AvatarDeliveryAdminScope comes from current verified IAM identity, not tool arguments.
type AvatarDeliveryAdminScope struct {
	ProfileScope

	GlobalAdmin bool
}

type AvatarDeliveryResolution struct {
	DeliveryID      uuid.UUID `json:"delivery_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
	Action          string    `json:"action"                       enum:"retry,dismiss"`
	Reason          string    `json:"reason"                                            minLength:"1" maxLength:"512"`
}
type AvatarDeliveryPreparation struct {
	ID          uuid.UUID                `json:"proposal_id"`
	Arguments   AvatarDeliveryResolution `json:"arguments"`
	Delivery    AvatarDelivery           `json:"delivery"`
	Description string                   `json:"description"`
	ExpiresAt   time.Time                `json:"expires_at"`
}
type AvatarDeliveryResolutionResult struct {
	DeliveryID        uuid.UUID       `json:"delivery_id"`
	RecipientID       uuid.UUID       `json:"recipient_id"`
	PreviousState     string          `json:"previous_state"`
	PreviousVersion   int64           `json:"previous_version"`
	PreviousAttemptID uuid.UUID       `json:"previous_attempt_id"`
	PreviousReceipt   json.RawMessage `json:"previous_receipt"`
	State             string          `json:"state"`
	Version           int64           `json:"version"`
	Actor             string          `json:"actor"`
	Reason            string          `json:"reason"`
	ResolvedAt        time.Time       `json:"resolved_at"`
}

func normalizeAvatarResolution(
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
	a AvatarDeliveryResolution,
) (AvatarDeliveryResolution, []byte, error) {
	a.Reason = strings.TrimSpace(a.Reason)
	if scope.BotID <= 0 || scope.Organization == "" || scope.Subject == "" || scope.UserID == uuid.Nil ||
		op == uuid.Nil ||
		a.DeliveryID == uuid.Nil ||
		a.ExpectedVersion < 1 ||
		a.Reason == "" ||
		!utf8.ValidString(a.Reason) ||
		utf8.RuneCountInString(a.Reason) > 512 ||
		(a.Action != notificationRetry && a.Action != "dismiss") {
		return a, nil, ErrInvalidAvatar
	}
	if !scope.GlobalAdmin {
		return a, nil, payments.ErrDenied
	}
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return a, hash[:], err
}
func avatarResolutionKey(scope AvatarDeliveryAdminScope, op uuid.UUID) sqlc.GetAvatarDeliveryResolutionParams {
	return sqlc.GetAvatarDeliveryResolutionParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		OperationID:    op,
	}
}

// beginAvatarResolution checks current actor access even for an exact result replay.
func (s *Store) beginAvatarResolution(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
) (pgx.Tx, *sqlc.Queries, error) {
	return s.beginAvatarResolutionSession(ctx, scope, op, nil)
}

func (s *Store) beginAvatarResolutionSession(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
	session *MiniAppAvatarAdminSession,
) (pgx.Tx, *sqlc.Queries, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("avatar-resolution:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, op),
	)
	if err == nil {
		var actor sqlc.ShuttleUser
		actor, err = q.LockAuthorityActor(
			ctx,
			sqlc.LockAuthorityActorParams{
				BotID:          scope.BotID,
				ZitadelOrg:     scope.Organization,
				ZitadelSubject: scope.Subject,
			},
		)
		if err == nil && actor.ID != scope.UserID {
			err = payments.ErrDenied
		}
		if err == nil && session != nil {
			err = lockAvatarAdminSession(ctx, q, actor, *session)
		}
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, q, nil
}

// lockAvatarResolutionTarget locks the owner before delivery, as the worker does.
// SHARE fences revocation without deadlocking admins who repair each other's delivery.
func lockAvatarResolutionTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	a AvatarDeliveryResolution,
) (AvatarDelivery, error) {
	row, err := q.GetAvatarDelivery(
		ctx,
		sqlc.GetAvatarDeliveryParams{ID: a.DeliveryID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	if err != nil {
		return AvatarDelivery{}, err
	}
	_, err = q.LockAvatarDeliveryResolutionOwner(
		ctx,
		sqlc.LockAvatarDeliveryResolutionOwnerParams{
			ID:         row.UserID,
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
		},
	)
	if err != nil {
		return AvatarDelivery{}, err
	}
	row, err = q.LockAvatarDeliveryResolutionTarget(
		ctx,
		sqlc.LockAvatarDeliveryResolutionTargetParams{
			ID:         a.DeliveryID,
			BotID:      scope.BotID,
			ZitadelOrg: scope.Organization,
		},
	)
	if err != nil {
		return AvatarDelivery{}, err
	}
	n := avatarDelivery(row)
	if n.Version != a.ExpectedVersion ||
		(n.State != deliveryUnknown && n.State != deliveryFailed && n.State != deliveryBlocked) {
		return n, ErrConflict
	}
	if a.Action == notificationRetry {
		_, err = q.AvatarDeliveryRecipient(
			ctx,
			sqlc.AvatarDeliveryRecipientParams{ID: n.ID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
		)
	}
	return n, err
}

// PrepareAvatarDeliveryResolution makes no delivery change. Uncertain retries require explicit confirmation.
func (s *Store) PrepareAvatarDeliveryResolution(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
	a AvatarDeliveryResolution,
) (AvatarDeliveryPreparation, error) {
	return s.prepareAvatarDeliveryResolution(ctx, scope, op, uuid.Nil, a, nil)
}

func (s *Store) prepareAvatarDeliveryResolution(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op, resume uuid.UUID,
	a AvatarDeliveryResolution,
	session *MiniAppAvatarAdminSession,
) (AvatarDeliveryPreparation, error) {
	a, hash, err := normalizeAvatarResolution(scope, op, a)
	if err != nil {
		return AvatarDeliveryPreparation{}, err
	}
	tx, q, err := s.beginAvatarResolutionSession(ctx, scope, op, session)
	if err != nil {
		return AvatarDeliveryPreparation{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := q.GetAvatarDeliveryResolution(ctx, avatarResolutionKey(scope, op))
	if err == nil {
		result, replayErr := avatarResolutionPreparation(ctx, q, prior, hash, resume, session != nil)
		if replayErr != nil {
			return result, replayErr
		}
		return finishAvatarResolutionPreparation(ctx, tx, q, scope, op, result, session)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AvatarDeliveryPreparation{}, err
	}
	if resume != uuid.Nil {
		result, adoptErr := adoptAvatarDeliveryResolution(ctx, q, scope, op, resume, hash)
		if adoptErr != nil {
			return AvatarDeliveryPreparation{}, adoptErr
		}
		return finishAvatarResolutionPreparation(ctx, tx, q, scope, op, result, session)
	}
	n, err := lockAvatarResolutionTarget(ctx, q, scope, a)
	if err != nil {
		return AvatarDeliveryPreparation{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return AvatarDeliveryPreparation{}, err
	}
	result := AvatarDeliveryPreparation{
		ID:        uuid.New(),
		Arguments: a,
		Delivery:  n,
		ExpiresAt: now.Time.UTC().Add(avatarResolutionPreparationTTL),
		Description: fmt.Sprintf(
			"%s avatar delivery %s for recipient %s, state %s, version %d. Reason: %s" +
". Dismiss does not prove delivery. Retry checks current recipient access" +
" before sending.",
			a.Action,
			n.ID,
			n.UserID,
			n.State,
			n.Version,
			a.Reason,
		),
	}
	if a.Action == notificationRetry && n.State == deliveryUnknown {
		result.Description += " Telegram may already have received this document. Retry can send a dupl" +
"icate. Check the receipt and chat first; retry only as a last resort."
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return AvatarDeliveryPreparation{}, err
	}
	err = q.InsertAvatarDeliveryResolution(
		ctx,
		sqlc.InsertAvatarDeliveryResolutionParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    op,
			ProposalID:     result.ID,
			DeliveryID:     a.DeliveryID,
			RequestHash:    hash,
			ExpiresAt:      pgtype.Timestamptz{Time: result.ExpiresAt, Valid: true},
			Document:       raw,
		},
	)
	if err != nil {
		return result, err
	}
	return finishAvatarResolutionPreparation(ctx, tx, q, scope, op, result, session)
}

// ResolveAvatarDelivery confirms the prepared action and atomically retains the old attempt evidence.
// Exact replay precedes target/expiry checks, but always requires current administrator access.
func (s *Store) ResolveAvatarDelivery(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
	a AvatarDeliveryResolution,
	proposal uuid.UUID,
) (AvatarDeliveryResolutionResult, error) {
	return s.resolveAvatarDelivery(ctx, scope, op, a, proposal, nil)
}

func (s *Store) resolveAvatarDelivery(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
	a AvatarDeliveryResolution,
	proposal uuid.UUID,
	session *MiniAppAvatarAdminSession,
) (AvatarDeliveryResolutionResult, error) {
	a, hash, err := normalizeAvatarResolution(scope, op, a)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	if proposal == uuid.Nil {
		return AvatarDeliveryResolutionResult{}, ErrInvalidAvatar
	}
	tx, q, err := s.beginAvatarResolutionSession(ctx, scope, op, session)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := lockAvatarDeliveryResolution(ctx, q, scope, op)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return AvatarDeliveryResolutionResult{}, ErrConflict
	}
	if len(prior.Result) > 0 {
		var result AvatarDeliveryResolutionResult
		if err = json.Unmarshal(prior.Result, &result); err != nil {
			return result, err
		}
		return finishAvatarResolutionConfirmation(ctx, tx, q, scope, op, prior.ProposalID, result, session)
	}
	n, err := lockAvatarResolutionTarget(ctx, q, scope, a)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	if !now.Time.Before(prior.ExpiresAt.Time) {
		return AvatarDeliveryResolutionResult{}, ErrConflict
	}
	state := "dismissed"
	if a.Action == notificationRetry {
		state = deliveryPending
	}
	changed, err := q.ApplyAvatarDeliveryResolution(
		ctx,
		sqlc.ApplyAvatarDeliveryResolutionParams{ID: n.ID, ExpectedVersion: n.Version, Outcome: state},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrConflict
	}
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	result := AvatarDeliveryResolutionResult{
		DeliveryID:        n.ID,
		RecipientID:       n.UserID,
		PreviousState:     n.State,
		PreviousVersion:   n.Version,
		PreviousAttemptID: n.AttemptID,
		PreviousReceipt:   n.Receipt,
		State:             changed.State,
		Version:           changed.Version,
		Actor:             scope.Subject,
		Reason:            a.Reason,
		ResolvedAt:        now.Time.UTC(),
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	count, err := q.SaveAvatarDeliveryResolution(
		ctx,
		sqlc.SaveAvatarDeliveryResolutionParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    prior.OperationID,
			Result:         raw,
		},
	)
	if err == nil && count != 1 {
		err = ErrConflict
	}
	if err != nil {
		return result, err
	}
	return finishAvatarResolutionConfirmation(ctx, tx, q, scope, op, prior.ProposalID, result, session)
}

// AvatarDeliveryResolutionReceipt returns the immutable result to its current authorized actor.
func (s *Store) AvatarDeliveryResolutionReceipt(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
) (AvatarDeliveryResolutionResult, error) {
	if !scope.GlobalAdmin {
		return AvatarDeliveryResolutionResult{}, payments.ErrDenied
	}
	if scope.BotID <= 0 || scope.Organization == "" || scope.Subject == "" || scope.UserID == uuid.Nil ||
		op == uuid.Nil {
		return AvatarDeliveryResolutionResult{}, ErrInvalidAvatar
	}
	tx, q, err := s.beginAvatarResolution(ctx, scope, op)
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.GetAvatarDeliveryResolution(ctx, avatarResolutionKey(scope, op))
	if err != nil {
		return AvatarDeliveryResolutionResult{}, err
	}
	if len(row.Result) == 0 {
		return AvatarDeliveryResolutionResult{}, pgx.ErrNoRows
	}
	var result AvatarDeliveryResolutionResult
	err = json.Unmarshal(row.Result, &result)
	return result, err
}
