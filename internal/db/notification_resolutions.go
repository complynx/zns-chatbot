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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const notificationRetry = "retry"

const pairNotificationKind = "pair"

const orderNotificationKind = "order"
const knowledgeNotificationKind = "knowledge"
const massageNotificationKind = "massage"
const massageSpecialistNotificationKind = "massage_specialist"

// NotificationResolution changes delivery only; it cannot modify a payment.
type NotificationResolution struct {
	NotificationKind string `json:"notification_kind,omitempty"`

	EventID         uuid.UUID `json:"event_id,omitempty"`
	NotificationID  uuid.UUID `json:"notification_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
	Action          string    `json:"action" enum:"retry,dismiss"`
	Reason          string    `json:"reason" minLength:"1" maxLength:"512"`
}

type NotificationPreparation struct {
	ID          uuid.UUID              `json:"proposal_id"`
	Arguments   NotificationResolution `json:"arguments"`
	Description string                 `json:"description"`
	ExpiresAt   time.Time              `json:"expires_at"`
}

type NotificationResolutionResult struct {
	NotificationKind string          `json:"notification_kind,omitempty"`
	NotificationID   uuid.UUID       `json:"notification_id"`
	RecipientID      uuid.UUID       `json:"recipient_id"`
	PreviousState    string          `json:"previous_state"`
	PreviousReceipt  json.RawMessage `json:"previous_receipt"`
	State            string          `json:"state"`
	Version          int64           `json:"version"`
	Actor            string          `json:"actor"`
	Reason           string          `json:"reason"`
	ResolvedAt       time.Time       `json:"resolved_at"`
}

func normalizedNotificationResolution(
	scope PaymentScope,
	op string,
	args NotificationResolution,
) (NotificationResolution, []byte, error) {
	args.Reason = strings.TrimSpace(args.Reason)
	if !paymentScopeValid(scope, op) ||
		(args.EventID == uuid.Nil && args.NotificationKind != knowledgeNotificationKind) ||
		args.NotificationID == uuid.Nil ||
		(args.NotificationKind != "" &&
			args.NotificationKind != "payment" &&
			args.NotificationKind != pairNotificationKind &&
			args.NotificationKind != orderNotificationKind &&
			args.NotificationKind != massageNotificationKind &&
			args.NotificationKind != massageSpecialistNotificationKind &&
			args.NotificationKind != knowledgeNotificationKind) ||
		args.ExpectedVersion < 1 ||
		len(args.Reason) == 0 ||
		len(args.Reason) > payments.MaxReasonLength ||
		(args.Action != notificationRetry && args.Action != "dismiss") {
		return args, nil, payments.ErrInvalid
	}
	if !scope.GlobalAdmin {
		return args, nil, payments.ErrDenied
	}
	raw, err := json.Marshal(args)
	hash := sha256.Sum256(raw)
	return args, hash[:], err
}

func notificationResolutionKey(scope PaymentScope, op string) sqlc.GetNotificationResolutionParams {
	return sqlc.GetNotificationResolutionParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		OperationID:    op,
	}
}

type notificationResolutionTarget struct {
	ID, RecipientID uuid.UUID
	State           string
	Version         int64
	Receipt         json.RawMessage
}

func loadResolutionTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args NotificationResolution,
) (notificationResolutionTarget, error) {
	if args.NotificationKind == knowledgeNotificationKind {
		return loadKnowledgeResolutionTarget(ctx, q, scope, args)
	}
	if args.NotificationKind == massageSpecialistNotificationKind {
		return loadMassageSpecialistResolutionTarget(ctx, q, scope, args)
	}
	if args.NotificationKind == massageNotificationKind {
		n, err := readMassageReminder(ctx, q, scope.EventScope, args.NotificationID)
		if err != nil {
			return notificationResolutionTarget{}, err
		}
		if n.Booking.EventID != args.EventID {
			return notificationResolutionTarget{}, pgx.ErrNoRows
		}
		return notificationResolutionTarget{
			ID:          n.ID,
			RecipientID: n.RecipientID,
			State:       n.State,
			Version:     n.Version,
			Receipt:     n.Receipt,
		}, nil
	}
	if args.NotificationKind == orderNotificationKind {
		n, err := readOrderNotification(ctx, q, scope.EventScope, args.NotificationID)
		if err != nil {
			return notificationResolutionTarget{}, err
		}
		if n.Evidence.EventID != args.EventID {
			return notificationResolutionTarget{}, pgx.ErrNoRows
		}
		return notificationResolutionTarget{
			ID:          n.ID,
			RecipientID: n.RecipientID,
			State:       n.State,
			Version:     n.Version,
			Receipt:     n.Receipt,
		}, nil
	}
	if args.NotificationKind == pairNotificationKind {
		row, err := q.GetPairInvitationNotification(ctx, sqlc.GetPairInvitationNotificationParams{
			ID: args.NotificationID, BotID: scope.BotID, OrganizationID: scope.Organization,
		})
		if err != nil {
			return notificationResolutionTarget{}, err
		}
		if row.ShuttlePairInvitation.EventID != args.EventID {
			return notificationResolutionTarget{}, pgx.ErrNoRows
		}
		n := row.ShuttlePairNotification
		return notificationResolutionTarget{
			ID:          n.ID,
			RecipientID: n.RecipientID,
			State:       n.State,
			Version:     n.Version,
			Receipt:     n.Receipt,
		}, nil
	}
	row, err := q.GetPaymentNotification(ctx, sqlc.GetPaymentNotificationParams{
		ID: args.NotificationID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return notificationResolutionTarget{}, err
	}
	n, err := notificationRecord(row)
	if err != nil {
		return notificationResolutionTarget{}, err
	}
	if n.Document.Evidence.EventID != args.EventID {
		return notificationResolutionTarget{}, pgx.ErrNoRows
	}
	return notificationResolutionTarget{
		ID:          n.ID,
		RecipientID: n.RecipientID,
		State:       n.State,
		Version:     n.Version,
		Receipt:     n.Receipt,
	}, nil
}

// Lock order matches notification reservation: operation, event, active actor.
// Dismiss remains available after the event module has been disabled.
func (s *Store) beginNotificationResolution(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args NotificationResolution,
) (pgx.Tx, *sqlc.Queries, notificationResolutionTarget, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, notificationResolutionTarget{}, err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("notification:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, op),
	)
	if err == nil && args.EventID != uuid.Nil {
		_, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: args.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
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
	}
	var n notificationResolutionTarget
	if err == nil {
		n, err = loadResolutionTarget(ctx, q, scope, args)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, n, err
	}
	return tx, q, n, nil
}

func resolutionAvailable(n notificationResolutionTarget, args NotificationResolution) bool {
	return n.Version == args.ExpectedVersion &&
		(n.State == deliveryUnknown || n.State == deliveryFailed || n.State == deliveryBlocked)
}

func (s *Store) NotificationResolutionReceipt(
	ctx context.Context,
	scope PaymentScope,
	op string,
) (*NotificationResolutionResult, error) {
	if !paymentScopeValid(scope, op) {
		return nil, payments.ErrInvalid
	}
	if !scope.GlobalAdmin {
		return nil, payments.ErrDenied
	}
	row, err := s.q.GetNotificationResolution(ctx, notificationResolutionKey(scope, op))
	if err != nil {
		return nil, err
	}
	if len(row.Result) == 0 {
		return nil, pgx.ErrNoRows
	}
	var result NotificationResolutionResult
	err = json.Unmarshal(row.Result, &result)
	return &result, err
}

// PrepareNotificationResolution stores the exact action and warns about uncertain delivery.
func (s *Store) PrepareNotificationResolution(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args NotificationResolution,
) (NotificationPreparation, error) {
	args, hash, err := normalizedNotificationResolution(scope, op, args)
	if err != nil {
		return NotificationPreparation{}, err
	}
	tx, q, n, err := s.beginNotificationResolution(ctx, scope, op, args)
	if err != nil {
		return NotificationPreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetNotificationResolution(ctx, notificationResolutionKey(scope, op))
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash) {
			return NotificationPreparation{}, ErrConflict
		}
		var result NotificationPreparation
		err = json.Unmarshal(prior.Document, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return NotificationPreparation{}, err
	}
	if !resolutionAvailable(n, args) {
		return NotificationPreparation{}, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return NotificationPreparation{}, err
	}
	location := " in event " + args.EventID.String()
	if args.NotificationKind == knowledgeNotificationKind && args.EventID == uuid.Nil {
		location = " (general/history; no event origin)"
	}
	result := NotificationPreparation{
		ID:        uuid.New(),
		Arguments: args,
		ExpiresAt: now.Time.Add(paymentPreparationTTL),
		Description: fmt.Sprintf(
			"%s notification %s for recipient %s%s, state %s, version %d. Reason: %s. No payment "+
				"changes. Dismiss does not prove delivery. Retry rechecks recipient access before "+
				"sending.",
			args.Action,
			n.ID,
			n.RecipientID,
			location,
			n.State,
			n.Version,
			args.Reason,
		),
	}
	if args.Action == notificationRetry && n.State == deliveryUnknown {
		result.Description += " Telegram may already have received this message. Retry can send a duplicate. Check " +
			"its receipt and the chat first; retry only as a last resort."
	}
	if args.NotificationKind == knowledgeNotificationKind {
		result.Description = "Knowledge delivery only; no publication or moderation changes. " + result.Description
	}
	if args.NotificationKind == pairNotificationKind {
		result.Description = "Pair invitation delivery. " + result.Description
	}
	if args.NotificationKind == orderNotificationKind {
		result.Description = "Order payment evidence delivery. " + result.Description
	}
	if args.NotificationKind == massageNotificationKind {
		result.Description = "Massage reminder delivery only; no booking or payment changes. " + result.Description
	}
	if args.NotificationKind == massageSpecialistNotificationKind {
		result.Description = "Massage specialist notification delivery only; captured grant and preference " +
			"generations are retained. No booking or payment changes. " + result.Description
	}
	document, err := json.Marshal(result)
	if err != nil {
		return NotificationPreparation{}, err
	}
	err = q.InsertNotificationResolution(ctx, notificationResolutionParams(scope, op, result, hash, document))
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

// ResolveNotification commits a new delivery generation and an immutable original receipt.
// Exact operation replay returns that receipt even after expiry or subsequent delivery.
func (s *Store) ResolveNotification(
	ctx context.Context,
	scope PaymentScope,
	op string,
	args NotificationResolution,
	proposal uuid.UUID,
) (NotificationResolutionResult, error) {
	args, hash, err := normalizedNotificationResolution(scope, op, args)
	if err != nil {
		return NotificationResolutionResult{}, err
	}
	if proposal == uuid.Nil {
		return NotificationResolutionResult{}, payments.ErrInvalid
	}
	tx, q, n, err := s.beginNotificationResolution(ctx, scope, op, args)
	if err != nil {
		return NotificationResolutionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prior, err := q.GetNotificationResolution(ctx, notificationResolutionKey(scope, op))
	if err != nil {
		return NotificationResolutionResult{}, err
	}
	if prior.ProposalID != proposal || !bytes.Equal(prior.RequestHash, hash) {
		return NotificationResolutionResult{}, ErrConflict
	}
	if len(prior.Result) != 0 {
		var result NotificationResolutionResult
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return NotificationResolutionResult{}, err
	}
	if !now.Time.Before(prior.ExpiresAt.Time) || !resolutionAvailable(n, args) {
		return NotificationResolutionResult{}, ErrConflict
	}
	state := "dismissed"
	if args.Action == notificationRetry {
		state = deliveryPending
	}
	changed, err := applyNotificationResolution(ctx, q, scope, n, args, state)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationResolutionResult{}, ErrConflict
	}
	if err != nil {
		return NotificationResolutionResult{}, err
	}
	result := NotificationResolutionResult{
		NotificationKind: args.NotificationKind,
		NotificationID:   n.ID,
		RecipientID:      n.RecipientID,
		PreviousState:    n.State,
		PreviousReceipt:  n.Receipt,
		State:            changed.State,
		Version:          changed.Version,
		Actor:            scope.Subject,
		Reason:           args.Reason,
		ResolvedAt:       now.Time.UTC(),
	}
	receipt, err := json.Marshal(result)
	if err != nil {
		return NotificationResolutionResult{}, err
	}
	saved, err := q.SaveNotificationResolution(
		ctx,
		sqlc.SaveNotificationResolutionParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    op,
			Result:         receipt,
		},
	)
	if err == nil && saved != 1 {
		err = ErrConflict
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}

func applyNotificationResolution(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	n notificationResolutionTarget,
	args NotificationResolution,
	state string,
) (notificationResolutionTarget, error) {
	if args.NotificationKind == knowledgeNotificationKind {
		if state == deliveryPending {
			if _, err := q.KnowledgeNotificationRecipient(
				ctx,
				sqlc.KnowledgeNotificationRecipientParams{
					ID:             n.ID,
					BotID:          scope.BotID,
					OrganizationID: scope.Organization,
				},
			); err != nil {
				return n, err
			}
		}
		row, err := q.ResolveKnowledgeNotification(
			ctx,
			sqlc.ResolveKnowledgeNotificationParams{ID: n.ID, Version: args.ExpectedVersion, State: state},
		)
		n.State, n.Version = row.State, row.Version
		return n, err
	}
	if args.NotificationKind == massageSpecialistNotificationKind {
		row, err := resolveMassageSpecialistNotification(ctx, q, scope, args, state)
		n.State, n.Version = row.State, row.Version
		return n, err
	}
	if args.NotificationKind == massageNotificationKind {
		row, err := resolveMassageReminder(ctx, q, scope, args, state)
		n.State, n.Version = row.State, row.Version
		return n, err
	}
	if args.NotificationKind == orderNotificationKind {
		row, err := q.ResolveOrderNotification(
			ctx,
			sqlc.ResolveOrderNotificationParams{ID: n.ID, Version: args.ExpectedVersion, State: state},
		)
		n.State, n.Version = row.State, row.Version
		return n, err
	}
	if args.NotificationKind == pairNotificationKind {
		row, err := q.ResolvePairInvitationNotification(
			ctx,
			sqlc.ResolvePairInvitationNotificationParams{ID: n.ID, Version: args.ExpectedVersion, State: state},
		)
		n.State, n.Version = row.State, row.Version
		return n, err
	}
	row, err := q.ResolvePaymentNotification(
		ctx,
		sqlc.ResolvePaymentNotificationParams{ID: n.ID, Version: args.ExpectedVersion, State: state},
	)
	n.State, n.Version = row.State, row.Version
	return n, err
}

// resolveMassageReminder rechecks retry eligibility and rebases under settings SHARE.
func resolveMassageReminder(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args NotificationResolution,
	state string,
) (sqlc.ShuttleMassageReminder, error) {
	params := sqlc.ResolveMassageReminderParams{ID: args.NotificationID, Version: args.ExpectedVersion, State: state}
	if state != deliveryPending {
		return q.ResolveMassageReminder(ctx, params)
	}
	long, short, err := readMassageReminderLeads(ctx, q)
	if err != nil {
		return sqlc.ShuttleMassageReminder{}, err
	}
	if _, err = q.MassageReminderRecipient(
		ctx,
		sqlc.MassageReminderRecipientParams{
			ID:             args.NotificationID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	); err != nil {
		return sqlc.ShuttleMassageReminder{}, err
	}
	reminder, err := q.LockMassageReminder(ctx, args.NotificationID)
	if err != nil {
		return sqlc.ShuttleMassageReminder{}, err
	}
	booking, err := q.ReadMassageBookingByID(ctx, reminder.BookingID)
	if err != nil {
		return sqlc.ShuttleMassageReminder{}, err
	}
	if booking.State != massageBookingStateBooked {
		return sqlc.ShuttleMassageReminder{}, ErrConflict
	}
	params.LeadSeconds = short
	if reminder.Kind == MassageReminderLong {
		params.LeadSeconds = long
	}
	params.DueAt = massageTimestamp(booking.StartsAt.Time.Add(-time.Duration(params.LeadSeconds) * time.Second))
	return q.ResolveMassageReminder(ctx, params)
}

func loadMassageSpecialistResolutionTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args NotificationResolution,
) (notificationResolutionTarget, error) {
	n, err := readMassageSpecialistNotification(ctx, q, scope.EventScope, args.NotificationID)
	if err != nil {
		return notificationResolutionTarget{}, err
	}
	if n.Booking.EventID != args.EventID {
		return notificationResolutionTarget{}, pgx.ErrNoRows
	}
	return notificationResolutionTarget{
		ID:          n.ID,
		RecipientID: n.RecipientID,
		State:       n.State,
		Version:     n.Version,
		Receipt:     n.Receipt,
	}, nil
}

// Retry rechecks eligibility but never rebases either captured generation. The
// send reservation repeats this check after IAM/network waits. Dismiss remains
// available when the recipient or module is no longer available.
func resolveMassageSpecialistNotification(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args NotificationResolution,
	state string,
) (sqlc.ShuttleMassageSpecialistNotification, error) {
	if state == deliveryPending {
		if _, err := q.MassageSpecialistNotificationRecipient(
			ctx,
			sqlc.MassageSpecialistNotificationRecipientParams{
				ID:             args.NotificationID,
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
			},
		); err != nil {
			return sqlc.ShuttleMassageSpecialistNotification{}, err
		}
	}
	return q.ResolveMassageSpecialistNotification(
		ctx,
		sqlc.ResolveMassageSpecialistNotificationParams{
			ID:      args.NotificationID,
			Version: args.ExpectedVersion,
			State:   state,
		},
	)
}

// Exactly one FK identifies the notification family in the immutable proposal.
func notificationResolutionParams(
	scope PaymentScope,
	op string,
	prep NotificationPreparation,
	hash, document []byte,
) sqlc.InsertNotificationResolutionParams {
	result := sqlc.InsertNotificationResolutionParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		Actor:          scope.Subject,
		OperationID:    op,
		ProposalID:     prep.ID,
		RequestHash:    hash,
		ExpiresAt:      pgtype.Timestamptz{Time: prep.ExpiresAt, Valid: true},
		Document:       document,
	}
	target := pgtype.UUID{Bytes: prep.Arguments.NotificationID, Valid: true}
	switch prep.Arguments.NotificationKind {
	case knowledgeNotificationKind:
		result.KnowledgeNoticeID = target
	case pairNotificationKind:
		result.PairNotificationID = target
	case orderNotificationKind:
		result.OrderNotificationID = target
	case massageNotificationKind:
		result.MassageReminderID = target
	case massageSpecialistNotificationKind:
		result.MassageSpecialistNotificationID = target
	default:
		result.NotificationID = target
	}
	return result
}

func loadKnowledgeResolutionTarget(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	args NotificationResolution,
) (notificationResolutionTarget, error) {
	n, err := readKnowledgeNotification(ctx, q, scope.EventScope, args.NotificationID)
	if err != nil {
		return notificationResolutionTarget{}, err
	}
	if n.EventID == nil && args.EventID != uuid.Nil || n.EventID != nil && *n.EventID != args.EventID {
		return notificationResolutionTarget{}, pgx.ErrNoRows
	}
	return notificationResolutionTarget{
		ID:          n.ID,
		RecipientID: n.RecipientID,
		State:       n.State,
		Version:     n.Version,
		Receipt:     n.Receipt,
	}, nil
}
