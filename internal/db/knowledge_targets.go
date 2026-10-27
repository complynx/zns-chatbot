package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func kbResolveModerator(ctx context.Context, q *sqlc.Queries, scope PaymentScope, c KnowledgeChange) (kbTarget, error) {
	var t kbTarget
	if err := kbActor(ctx, q, scope); err != nil {
		return t, err
	}
	if !scope.GlobalAdmin {
		return t, ErrKnowledgeDenied
	}
	if err := kbModeratorFence(ctx, q, scope); err != nil {
		return t, err
	}
	if _, err := q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: c.ModeratorUserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	); err != nil {
		return t, err
	}
	m, err := q.ReadKnowledgeModerator(
		ctx,
		sqlc.ReadKnowledgeModeratorParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			UserID:         c.ModeratorUserID,
		},
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return t, err
	}
	if m.Version != c.ExpectedModeratorVersion {
		return t, ErrConflict
	}
	t.moderator = &m
	return t, nil
}

func kbResolveOrigin(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	c KnowledgeChange,
) (uuid.UUID, *uuid.UUID, error) {
	articleID, eventID := c.ArticleID, c.EventID
	if c.Kind == kbReview {
		sg, err := q.ReadKnowledgeSuggestion(
			ctx,
			sqlc.ReadKnowledgeSuggestionParams{
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				ID:             c.SuggestionID,
			},
		)
		if err != nil {
			return articleID, eventID, err
		}
		articleID, eventID = sg.ArticleID, kbUUIDPointer(sg.EventID)
	}
	row, err := q.ReadKnowledgeArticle(
		ctx,
		sqlc.ReadKnowledgeArticleParams{BotID: scope.BotID, OrganizationID: scope.Organization, ArticleID: articleID},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return articleID, eventID, nil
	}
	if err != nil {
		return articleID, eventID, err
	}
	origin := kbUUIDPointer(row.ShuttleKnowledgeArticle.EventID)
	if c.Kind == kbReview && row.ShuttleKnowledgeArticle.EventID != kbEvent(eventID) {
		return articleID, eventID, ErrConflict
	}
	if eventID != nil && (origin == nil || *origin != *eventID) {
		return articleID, eventID, ErrConflict
	}
	return articleID, origin, nil
}
func kbResolveArticle(ctx context.Context, q *sqlc.Queries, scope PaymentScope, c KnowledgeChange) (kbTarget, error) {
	var t kbTarget
	articleID, eventID, err := kbResolveOrigin(ctx, q, scope, c)
	if err != nil {
		return t, err
	}
	t.event, err = kbLockEvent(ctx, q, scope, eventID)
	if err != nil {
		return t, err
	}
	if err = kbActor(ctx, q, scope); err != nil {
		return t, err
	}
	// Serialize absent-target creation as well as existing article changes.
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("knowledge-article:%d:%s:%s", scope.BotID, scope.Organization, articleID),
	); err != nil {
		return t, err
	}
	t.article, err = kbCurrentArticle(ctx, q, scope, articleID)
	if err != nil {
		return t, err
	}
	if t.article != nil && t.article.EventID != kbEvent(eventID) {
		return t, ErrConflict
	}
	if c.Kind == kbReview {
		sg, lockErr := q.LockKnowledgeSuggestion(
			ctx,
			sqlc.LockKnowledgeSuggestionParams{
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				ID:             c.SuggestionID,
			},
		)
		if lockErr != nil {
			return t, lockErr
		}
		t.suggestion = &sg
		if sg.State != kbPending {
			return t, ErrConflict
		}
		if sg.ProposerID == scope.UserID {
			return t, ErrKnowledgeDenied
		}
	}
	if t.event != nil {
		origin, originErr := q.KnowledgeEventArchiveOrigin(
			ctx,
			sqlc.KnowledgeEventArchiveOriginParams{
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				EventID:        kbUUID(t.event.ID),
			},
		)
		if originErr == nil {
			t.archiveOrigin = &origin
		} else if !errors.Is(originErr, pgx.ErrNoRows) {
			return t, originErr
		}
	}
	return t, nil
}
func kbCheckRevision(ctx context.Context, q *sqlc.Queries, scope PaymentScope, c KnowledgeChange, t kbTarget) error {
	current := int64(0)
	if t.article != nil {
		current = t.article.CurrentRevision
	}
	if current != c.ExpectedRevision {
		return ErrConflict
	}
	if c.Kind != kbReview || c.Decision != kbApprove {
		return nil
	}
	base := t.suggestion.BaseRevision
	if base == current {
		return nil
	}
	if base == 0 || base > current {
		return ErrConflict
	}
	// Only the deterministic archive revision may separate the proposed base and
	// the freshly reviewed current revision. Substantive edits never auto-merge.
	unchanged, err := q.KnowledgeOnlyArchiveGap(
		ctx,
		sqlc.KnowledgeOnlyArchiveGapParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ArticleID:      t.suggestion.ArticleID,
			BaseRevision:   base,
		},
	)
	if err != nil {
		return err
	}
	if !unchanged {
		return ErrConflict
	}
	return nil
}

func kbRefreshTarget(ctx context.Context, q *sqlc.Queries, scope PaymentScope, c KnowledgeChange, t *kbTarget) error {
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	t.now = now.Time
	t.historical = t.archiveOrigin != nil || kbHistorical(t.event, t.article, t.now)
	if c.Kind == kbEdit || c.Kind == kbReview {
		return kbAuthorize(ctx, q, scope, t.event, t.article, t.now)
	}
	return nil
}
