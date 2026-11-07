package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// KnowledgeNotification carries locators and delivery state, never proposed text.
type KnowledgeNotification struct {
	ID            uuid.UUID       `json:"id"`
	SuggestionID  uuid.UUID       `json:"suggestion_id"`
	RecipientID   uuid.UUID       `json:"recipient_id"`
	Kind          string          `json:"kind"`
	State         string          `json:"state"`
	Version       int64           `json:"version"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	CreatedAt     time.Time       `json:"created_at"`
	ArticleID     uuid.UUID       `json:"-"`
	EventID       *uuid.UUID      `json:"-"`
	AttemptID     uuid.UUID       `json:"-"`
	Receipt       json.RawMessage `json:"-"`
}

type KnowledgeNotificationPage struct {
	Items []KnowledgeNotification `json:"items"`
	Next  *uuid.UUID              `json:"next,omitempty"`
}

func kbNotice(row sqlc.ShuttleKnowledgeNotice) KnowledgeNotification {
	return KnowledgeNotification{ID: row.ID, SuggestionID: row.SuggestionID, RecipientID: row.RecipientID,
		Kind: row.Kind, State: row.State, Version: row.Version, NextAttemptAt: row.NextAttemptAt.Time.UTC(),
		CreatedAt: row.CreatedAt.Time.UTC(), AttemptID: uuid.UUID(row.AttemptID.Bytes), Receipt: row.Receipt}
}

// EnsureKnowledgeNotifications repairs at most100 missing recipients from the
// committed suggestions. Repeated scans and later authority changes are safe.
func (s *Store) EnsureKnowledgeNotifications(ctx context.Context, scope EventScope) error {
	if scope.BotID <= 0 || scope.Organization == "" {
		return kbInvalid("deployment scope required")
	}
	_, err := s.q.EnsureKnowledgeNotifications(
		ctx,
		sqlc.EnsureKnowledgeNotificationsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return err
}

func (s *Store) ReadyKnowledgeNotifications(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.q.ReadyKnowledgeNotifications(
		ctx,
		sqlc.ReadyKnowledgeNotificationsParams{BotID: scope.BotID, OrganizationID: scope.Organization, AfterID: after},
	)
	if err != nil {
		return nil, err
	}
	result := make([]NotificationJob, len(rows))
	for i, row := range rows {
		result[i] = NotificationJob{ID: row.ID, Version: row.Version}
	}
	return result, nil
}

func readKnowledgeNotification(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	id uuid.UUID,
) (KnowledgeNotification, error) {
	row, err := q.GetKnowledgeNotification(
		ctx,
		sqlc.GetKnowledgeNotificationParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return KnowledgeNotification{}, err
	}
	n := kbNotice(row.ShuttleKnowledgeNotice)
	n.ArticleID = row.ArticleID
	n.EventID = kbUUIDPointer(row.EventID)
	return n, nil
}

func (s *Store) KnowledgeNotification(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (KnowledgeNotification, error) {
	return readKnowledgeNotification(ctx, s.q, scope, id)
}

func (s *Store) KnowledgeNotificationRecipient(ctx context.Context, scope EventScope, id uuid.UUID) (User, error) {
	row, err := s.q.KnowledgeNotificationRecipient(
		ctx,
		sqlc.KnowledgeNotificationRecipientParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return user(row), err
}

// ReserveKnowledgeNotification checks current local authority after IAM verification. Lock ordering
// matches publication: event, actor, article, suggestion, moderator fence.
func (s *Store) ReserveKnowledgeNotification(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	version int64,
	payload json.RawMessage,
) (KnowledgeNotification, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgeNotification{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	n, err := readKnowledgeNotification(ctx, q, scope.EventScope, id)
	if err != nil {
		return n, err
	}
	if n.RecipientID != scope.UserID {
		return n, ErrKnowledgeDenied
	}
	target, err := kbResolveArticle(
		ctx,
		q,
		scope,
		KnowledgeChange{Kind: kbEdit, ArticleID: n.ArticleID, EventID: n.EventID},
	)
	if err != nil {
		return n, err
	}
	if _, err = q.LockKnowledgeSuggestion(
		ctx,
		sqlc.LockKnowledgeSuggestionParams{ID: n.SuggestionID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return n, err
	}
	if n.Kind == kbPending {
		now, e := q.RegistrationNow(ctx)
		if e != nil {
			return n, e
		}
		if err = kbAuthorize(ctx, q, scope, target.event, target.article, now.Time); err != nil {
			return n, err
		}
	}
	recipient, err := q.KnowledgeNotificationRecipient(
		ctx,
		sqlc.KnowledgeNotificationRecipientParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return n, err
	}
	if recipient.ID != scope.UserID {
		return n, ErrKnowledgeDenied
	}
	row, err := q.ReserveKnowledgeNotification(
		ctx,
		sqlc.ReserveKnowledgeNotificationParams{
			ID:        id,
			Version:   version,
			AttemptID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
			Payload:   payload,
		},
	)
	if err != nil {
		return n, err
	}
	n = kbNotice(row)
	return n, tx.Commit(ctx)
}

func (s *Store) BlockKnowledgeNotification(ctx context.Context, scope EventScope, id uuid.UUID, version int64) error {
	_, err := s.q.BlockKnowledgeNotification(
		ctx,
		sqlc.BlockKnowledgeNotificationParams{
			ID:             id,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Version:        version,
		},
	)
	return err
}

func (s *Store) FinishKnowledgeNotification(
	ctx context.Context,
	n KnowledgeNotification,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	if !notificationOutcomeValid(state) {
		return errors.New("invalid knowledge notice outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	changed, err := s.q.FinishKnowledgeNotification(
		ctx,
		sqlc.FinishKnowledgeNotificationParams{
			ID:        n.ID,
			AttemptID: pgtype.UUID{Bytes: n.AttemptID, Valid: true},
			State:     state,
			Receipt:   receipt,
			DelayMs:   delay.Milliseconds(),
		},
	)
	if err == nil && changed != 1 {
		return ErrConflict
	}
	return err
}

// RecoverKnowledgeNotifications requires the exclusive application instance lock. An interrupted send
// remains uncertain and is not automatically sent again.
func (s *Store) RecoverKnowledgeNotifications(ctx context.Context, scope EventScope) error {
	_, err := s.q.RecoverKnowledgeNotifications(
		ctx,
		sqlc.RecoverKnowledgeNotificationsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return err
}

// ListKnowledgeNotifications returns actor-owned delivery metadata. Global admins can inspect
// deployment-wide delivery state without reading proposed content or payloads.
func (s *Store) ListKnowledgeNotifications(
	ctx context.Context,
	scope PaymentScope,
	after uuid.UUID,
	limit int,
) (KnowledgeNotificationPage, error) {
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return KnowledgeNotificationPage{}, kbInvalid("limit must be 1..20")
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeNotificationPage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.ListKnowledgeNotifications(
		ctx,
		sqlc.ListKnowledgeNotificationsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			GlobalAdmin:    scope.GlobalAdmin,
			UserID:         scope.UserID,
			AfterID:        after,
			PageLimit:      int32(limit + 1),
		},
	)
	if err != nil {
		return KnowledgeNotificationPage{}, err
	}
	page := KnowledgeNotificationPage{Items: make([]KnowledgeNotification, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		id := rows[limit-1].ID
		page.Next = &id
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(
			page.Items,
			KnowledgeNotification{
				ID:            row.ID,
				SuggestionID:  row.SuggestionID,
				RecipientID:   row.RecipientID,
				Kind:          row.Kind,
				State:         row.State,
				Version:       row.Version,
				NextAttemptAt: row.NextAttemptAt.Time.UTC(),
				CreatedAt:     row.CreatedAt.Time.UTC(),
			},
		)
	}
	return page, nil
}
