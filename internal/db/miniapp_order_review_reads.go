package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// MiniAppOrderReviewDetail exposes sold terms and display identity, not private
// registration fields. Workspace is this reviewer's current saved decision.
type MiniAppOrderReviewDetail struct {
	Payer     PaymentReviewPayer    `json:"payer"`
	Order     SubmittedOrder        `json:"order"`
	Evidence  OrderPaymentEvidence  `json:"evidence"`
	Workspace *OrderReviewWorkspace `json:"workspace"`
}

func (s *Store) miniAppOrderReviewReadTx(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, event uuid.UUID,
) (pgx.Tx, sqlc.ShuttleEvent, error) {
	var record sqlc.ShuttleEvent
	if !paymentScopeValid(scope, "read") || event == uuid.Nil {
		return nil, record, orders.ErrSelection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, record, err
	}
	record, _, err = lockPaymentModuleEvent(ctx, s.q.WithTx(tx), scope, event, "orders", true)
	if err == nil {
		err = (paymentReviewOrigin{session: &session}).authorize(ctx, tx, scope)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, record, err
	}
	return tx, record, nil
}

// MiniAppOrderReviewEvents is discovery only. Event reads repeat role and session
// checks under that event's lock; this list does not grant access to its contents.
func (s *Store) MiniAppOrderReviewEvents(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, after uuid.UUID,
) (events.ContextList, error) {
	result := events.ContextList{Events: []events.ContextEvent{}}
	if !paymentScopeValid(scope, "read") {
		return result, orders.ErrSelection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = (paymentReviewOrigin{session: &session}).authorize(ctx, tx, scope); err != nil {
		return result, err
	}
	rows, err := s.q.WithTx(tx).ListOrderReviewEvents(ctx, sqlc.ListOrderReviewEventsParams{
		UserID: scope.UserID, Subject: scope.Subject, BotID: scope.BotID, OrganizationID: scope.Organization,
		GlobalAdmin: scope.GlobalAdmin, AfterID: after, PageLimit: events.ContextChoiceLimit + 1,
	})
	if err != nil {
		return result, err
	}
	more := len(rows) > events.ContextChoiceLimit
	if more {
		rows = rows[:events.ContextChoiceLimit]
	}
	for _, row := range rows {
		result.Events = append(result.Events, orderReviewContextEvent(row))
	}
	if more {
		result.Next = &result.Events[len(result.Events)-1].ID
	}
	return result, nil
}

func orderReviewContextEvent(row sqlc.ShuttleEvent) events.ContextEvent {
	return events.ContextEvent{ID: row.ID, Version: row.Version, Name: row.Name, Timezone: row.Timezone,
		StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC()}
}

func (s *Store) MiniAppOrderReviewEvent(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, event uuid.UUID,
) (events.ContextEvent, error) {
	tx, row, err := s.miniAppOrderReviewReadTx(ctx, scope, session, event)
	if err != nil {
		return events.ContextEvent{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return orderReviewContextEvent(row), nil
}

func (s *Store) MiniAppOrderReviewQueue(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, event, after uuid.UUID, limit int32,
) (OrderEvidencePage, error) {
	if limit < 1 || limit > 20 {
		return OrderEvidencePage{}, orders.ErrSelection
	}
	tx, _, err := s.miniAppOrderReviewReadTx(ctx, scope, session, event)
	if err != nil {
		return OrderEvidencePage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readOrderEvidencePage(ctx, s.q.WithTx(tx), sqlc.ListOrderEvidenceParams{
		EventID: event, BotID: scope.BotID, OrganizationID: scope.Organization,
		UnresolvedOnly: true, AfterID: after, PageLimit: limit + 1,
	})
}

func (s *Store) MiniAppOrderReviewDetail(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, event, evidence uuid.UUID,
) (MiniAppOrderReviewDetail, error) {
	var result MiniAppOrderReviewDetail
	tx, _, err := s.miniAppOrderReviewReadTx(ctx, scope, session, event)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	result.Evidence, err = currentAuthorityOrderEvidence(ctx, q, scope, event, evidence)
	if err != nil {
		return result, err
	}
	row, err := q.ReadSubmittedOrder(ctx, sqlc.ReadSubmittedOrderParams{
		ID: result.Evidence.OrderID, UserID: result.Evidence.UserID, EventID: event,
	})
	if err != nil {
		return result, err
	}
	result.Order, err = submittedOrderRecord(row)
	if err != nil {
		return result, err
	}
	presentation, err := q.ReadOrderReviewPresentation(ctx, sqlc.ReadOrderReviewPresentationParams{
		ActorID: scope.UserID, EvidenceID: evidence, EventID: event, BotID: scope.BotID,
		Organization: scope.Organization, Subject: scope.Subject,
	})
	if err != nil {
		return result, err
	}
	result.Payer = PaymentReviewPayer{UserID: result.Evidence.UserID, FullName: presentation.PayerName}
	id, err := q.CurrentOrderReviewWorkspace(ctx, sqlc.CurrentOrderReviewWorkspaceParams{
		ActorID: scope.UserID, EventID: event, EvidenceID: evidence,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	workspace, err := readOrderReviewWorkspace(ctx, q, scope.UserID, id)
	if err != nil {
		return result, err
	}
	result.Workspace = &workspace
	return result, nil
}

// MiniAppOrderReviewFile is evidence-scoped, even for revoked payers. Call it
// again after external IO; it releases all event/session locks before returning.
func (s *Store) MiniAppOrderReviewFile(ctx context.Context, scope PaymentScope,
	session MiniAppPaymentSession, event, evidence uuid.UUID,
) (File, error) {
	tx, _, err := s.miniAppOrderReviewReadTx(ctx, scope, session, event)
	if err != nil {
		return File{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).GetOrderEvidenceFile(ctx, sqlc.GetOrderEvidenceFileParams{
		ID: evidence, EventID: event, BotID: scope.BotID, ZitadelOrg: scope.Organization,
	})
	if err != nil {
		return File{}, err
	}
	return storedFile(row), nil
}
