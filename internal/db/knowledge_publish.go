package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func kbValidEventScope(scope PaymentScope, eventID *uuid.UUID) bool {
	return kbScope(scope) && (eventID == nil || *eventID != uuid.Nil)
}

func kbRevisionParameters(
	scope PaymentScope,
	id uuid.UUID,
	kind, reason string,
	suggestion *sqlc.ShuttleKnowledgeSuggestion,
) sqlc.InsertKnowledgeRevisionParams {
	rev := sqlc.InsertKnowledgeRevisionParams{
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		ArticleID:      id,
		ChangeKind:     kind,
		AuthorID:       kbUUID(scope.UserID),
		Reason:         reason,
	}
	if suggestion != nil {
		rev.ProposerID = kbUUID(suggestion.ProposerID)
		rev.ReviewerID = kbUUID(scope.UserID)
		rev.SuggestionID = kbUUID(suggestion.ID)
	}
	return rev
}

func kbArchiveEventSnapshot(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	event sqlc.ShuttleEvent,
) (sqlc.ShuttleEvent, error) {
	origin, err := q.KnowledgeEventArchiveOrigin(
		ctx,
		sqlc.KnowledgeEventArchiveOriginParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			EventID:        kbUUID(event.ID),
		},
	)
	switch {
	case err == nil:
		event.Name = origin.EventName.String
		event.Timezone = origin.EventTimezone.String
		event.StartsAt = origin.EventStartsAt
		event.EndsAt = origin.EventEndsAt
		return event, nil
	case errors.Is(err, pgx.ErrNoRows):
		return event, nil
	default:
		return event, err
	}
}

func kbApply(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	c KnowledgeChange,
	t kbTarget,
) (KnowledgeResult, error) {
	result := KnowledgeResult{Kind: c.Kind}
	switch c.Kind {
	case kbSetModerator:
		row, err := q.SetKnowledgeModerator(
			ctx,
			sqlc.SetKnowledgeModeratorParams{
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				UserID:         c.ModeratorUserID,
				Enabled:        *c.ModeratorEnabled,
				ChangedBy:      scope.UserID,
			},
		)
		if err != nil {
			return result, err
		}
		m := kbModerator(row)
		result.Moderator = &m
	case kbSuggest:
		return kbSubmitSuggestion(ctx, q, scope, c, t)
	case kbEdit:
		kind := kbEdit
		if t.article == nil {
			kind = "create"
		}
		article, err := kbPublish(ctx, q, scope, c.ArticleID, *c.Content, c.Reason, kind, t, nil)
		if err != nil {
			return result, err
		}
		result.Article = &article
	case kbReview:
		return kbDecideSuggestion(ctx, q, scope, c, t)
	}
	return result, nil
}

// Publish copies current locked event provenance, preserving the original event
// identity and any irreversible archive marker. It never rewrites factual text.
func kbPublish(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	id uuid.UUID,
	content knowledge.Content,
	reason, kind string,
	t kbTarget,
	suggestion *sqlc.ShuttleKnowledgeSuggestion,
) (KnowledgeArticle, error) {
	content, err := content.Normalize()
	if err != nil {
		return KnowledgeArticle{}, err
	}
	sources, err := json.Marshal(content.Sources)
	if err != nil {
		return KnowledgeArticle{}, err
	}
	var eventID pgtype.UUID
	var name, zone pgtype.Text
	var starts, ends, archived pgtype.Timestamptz
	if t.event != nil {
		eventID = kbUUID(t.event.ID)
		name = pgtype.Text{String: t.event.Name, Valid: true}
		zone = pgtype.Text{String: t.event.Timezone, Valid: true}
		starts = t.event.StartsAt
		ends = t.event.EndsAt
		if t.archiveOrigin != nil {
			name = t.archiveOrigin.EventName
			zone = t.archiveOrigin.EventTimezone
			starts = t.archiveOrigin.EventStartsAt
			ends = t.archiveOrigin.EventEndsAt
			// Newly published content for an already archived event is historical.
			if t.article == nil {
				archived = pgtype.Timestamptz{Time: t.now, Valid: true}
			}
		}
		if t.article != nil && t.article.ArchivedAt.Valid {
			// Archival provenance stays frozen after rescheduling. Copying a future end
			// into an old archive would both change history and violate its time bound.
			name = t.article.EventName
			zone = t.article.EventTimezone
			starts = t.article.EventStartsAt
			ends = t.article.EventEndsAt
			archived = t.article.ArchivedAt
		}
	}
	var row sqlc.ShuttleKnowledgeArticle
	if t.article == nil {
		row, err = q.InsertKnowledgeArticle(
			ctx,
			sqlc.InsertKnowledgeArticleParams{
				ID:             id,
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				EventID:        eventID,
				Slug:           content.Slug,
				Title:          content.Title,
				Language:       content.Language,
				Topics:         content.Topics,
				Body:           content.Body,
				Sources:        sources,
				EventName:      name,
				EventStartsAt:  starts,
				EventEndsAt:    ends,
				EventTimezone:  zone,
				ArchivedAt:     archived,
			},
		)
	} else {
		row, err = q.UpdateKnowledgeArticle(
			ctx,
			sqlc.UpdateKnowledgeArticleParams{
				BotID:            scope.BotID,
				OrganizationID:   scope.Organization,
				ID:               id,
				ExpectedRevision: t.article.CurrentRevision,
				Slug:             content.Slug,
				Title:            content.Title,
				Language:         content.Language,
				Topics:           content.Topics,
				Body:             content.Body,
				Sources:          sources,
				EventName:        name,
				EventStartsAt:    starts,
				EventEndsAt:      ends,
				EventTimezone:    zone,
				ArchivedAt:       archived,
			},
		)
	}
	if err != nil {
		return KnowledgeArticle{}, err
	}
	rev := kbRevisionParameters(scope, id, kind, reason, suggestion)
	if err = q.InsertKnowledgeRevision(ctx, rev); err != nil {
		return KnowledgeArticle{}, err
	}
	return kbArticle(row, t.historical)
}

func (s *Store) KnowledgeCapabilities(
	ctx context.Context,
	scope PaymentScope,
	eventID *uuid.UUID,
) (KnowledgeCapabilities, error) {
	if !kbValidEventScope(scope, eventID) {
		return KnowledgeCapabilities{}, kbInvalid("valid caller/event required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgeCapabilities{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	event, err := kbLockEvent(ctx, q, scope, eventID)
	if err != nil {
		return KnowledgeCapabilities{}, err
	}
	if err = kbActor(ctx, q, scope); err != nil {
		return KnowledgeCapabilities{}, err
	}
	c := KnowledgeCapabilities{GlobalAdministrator: scope.GlobalAdmin}
	m, err := q.ReadKnowledgeModerator(
		ctx,
		sqlc.ReadKnowledgeModeratorParams{BotID: scope.BotID, OrganizationID: scope.Organization, UserID: scope.UserID},
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	c.Moderator = err == nil && m.Enabled
	if event == nil {
		c.EventAdministrator, err = q.KnowledgeAnyEventAdministrator(
			ctx,
			sqlc.KnowledgeAnyEventAdministratorParams{
				UserID:         scope.UserID,
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				GlobalAdmin:    scope.GlobalAdmin,
			},
		)
		if err != nil {
			return c, err
		}
		c.CanEdit = c.Moderator || c.EventAdministrator
		return c, nil
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return c, err
	}
	archived, err := q.KnowledgeEventArchived(
		ctx,
		sqlc.KnowledgeEventArchivedParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			EventID:        kbUUID(event.ID),
		},
	)
	if err != nil {
		return c, err
	}
	if archived || !now.Time.Before(event.EndsAt.Time) {
		c.CanEdit = c.Moderator
		return c, nil
	}
	role, err := q.ReadEventAuthority(
		ctx,
		sqlc.ReadEventAuthorityParams{
			EventID:        event.ID,
			UserID:         scope.UserID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	c.EventAdministrator = scope.GlobalAdmin || err == nil && role.Role == AuthorityAdministrator
	c.CanEdit = c.EventAdministrator
	return c, nil
}

// ArchiveEndedKnowledge is bounded, restart-safe maintenance. It locks the
// current event before inspecting its end time, then copies one archive revision.
func (s *Store) ArchiveEndedKnowledge(ctx context.Context, scope EventScope, cursor uuid.UUID) (uuid.UUID, error) {
	if scope.BotID <= 0 || scope.Organization == "" {
		return cursor, kbInvalid("deployment scope required")
	}
	ids, err := s.q.EndedKnowledgeArticles(
		ctx,
		sqlc.EndedKnowledgeArticlesParams{BotID: scope.BotID, OrganizationID: scope.Organization, ID: cursor},
	)
	if err != nil {
		return cursor, err
	}
	for _, id := range ids {
		if err = s.kbArchiveOne(ctx, scope, id); err != nil {
			return cursor, err
		}
		cursor = id
	}
	if len(ids) < kbArchiveBatch {
		cursor = uuid.Nil
	}
	return cursor, nil
}
func (s *Store) kbArchiveOne(ctx context.Context, scope EventScope, id uuid.UUID) error {
	a, err := s.q.ReadKnowledgeArticle(
		ctx,
		sqlc.ReadKnowledgeArticleParams{BotID: scope.BotID, OrganizationID: scope.Organization, ArticleID: id},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	origin := kbUUIDPointer(a.ShuttleKnowledgeArticle.EventID)
	if origin == nil {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	event, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: *origin, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return err
	}
	if err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("knowledge-article:%d:%s:%s", scope.BotID, scope.Organization, id),
	); err != nil {
		return err
	}
	row, err := q.LockKnowledgeArticle(
		ctx,
		sqlc.LockKnowledgeArticleParams{BotID: scope.BotID, OrganizationID: scope.Organization, ID: id},
	)
	if err != nil {
		return err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if row.ArchivedAt.Valid {
		return nil
	}
	event, err = kbArchiveEventSnapshot(ctx, q, scope, event)
	if err != nil {
		return err
	}
	if now.Time.Before(event.EndsAt.Time) {
		return nil
	}
	_, err = q.UpdateKnowledgeArticle(
		ctx,
		sqlc.UpdateKnowledgeArticleParams{
			BotID:            scope.BotID,
			OrganizationID:   scope.Organization,
			ID:               id,
			ExpectedRevision: row.CurrentRevision,
			Slug:             row.Slug,
			Title:            row.Title,
			Language:         row.Language,
			Topics:           row.Topics,
			Body:             row.Body,
			Sources:          row.Sources,
			EventName:        pgtype.Text{String: event.Name, Valid: true},
			EventStartsAt:    event.StartsAt,
			EventEndsAt:      event.EndsAt,
			EventTimezone:    pgtype.Text{String: event.Timezone, Valid: true},
			ArchivedAt:       now,
		},
	)
	if err != nil {
		return err
	}
	if err = q.InsertKnowledgeRevision(
		ctx,
		sqlc.InsertKnowledgeRevisionParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ArticleID:      id,
			ChangeKind:     "archive",
			Reason:         "Event ended; preserve published content as historical reference.",
		},
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func kbSubmitSuggestion(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	c KnowledgeChange,
	t kbTarget,
) (KnowledgeResult, error) {
	result := KnowledgeResult{Kind: c.Kind}

	sources, err := json.Marshal(c.Content.Sources)
	if err != nil {
		return result, err
	}
	var eventVersion pgtype.Int8
	var eventID pgtype.UUID
	if t.event != nil {
		eventID = kbUUID(t.event.ID)
		eventVersion = pgtype.Int8{Int64: t.event.Version, Valid: true}
	}
	row, err := q.InsertKnowledgeSuggestion(
		ctx,
		sqlc.InsertKnowledgeSuggestionParams{
			ID:             uuid.New(),
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ArticleID:      c.ArticleID,
			BaseRevision:   c.ExpectedRevision,
			EventID:        eventID,
			EventVersion:   eventVersion,
			ProposerID:     scope.UserID,
			Reason:         c.Reason,
			Slug:           c.Content.Slug,
			Title:          c.Content.Title,
			Language:       c.Content.Language,
			Topics:         c.Content.Topics,
			Body:           c.Content.Body,
			Sources:        sources,
		},
	)
	if err != nil {
		return result, err
	}
	sg, err := kbSuggestion(row)
	if err != nil {
		return result, err
	}
	result.Suggestion = &sg
	return result, nil
}

func kbDecideSuggestion(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	c KnowledgeChange,
	t kbTarget,
) (KnowledgeResult, error) {
	result := KnowledgeResult{Kind: c.Kind}

	state := "rejected"
	var published pgtype.Int8
	if c.Decision == kbApprove {
		sg, err := kbSuggestion(*t.suggestion)
		if err != nil {
			return result, err
		}
		content, err := sg.Content.Normalize()
		if err != nil {
			return result, err
		}
		article, err := kbPublish(
			ctx,
			q,
			scope,
			sg.ArticleID,
			content,
			c.Reason,
			"approved_suggestion",
			t,
			t.suggestion,
		)
		if err != nil {
			return result, err
		}
		result.Article = &article
		state = "approved"
		published = pgtype.Int8{Int64: article.Revision, Valid: true}
	}
	row, err := q.DecideKnowledgeSuggestion(
		ctx,
		sqlc.DecideKnowledgeSuggestionParams{
			BotID:             scope.BotID,
			OrganizationID:    scope.Organization,
			ID:                c.SuggestionID,
			State:             state,
			ReviewerID:        kbUUID(scope.UserID),
			DecisionReason:    pgtype.Text{String: c.Reason, Valid: true},
			PublishedRevision: published,
		},
	)
	if err != nil {
		return result, err
	}
	sg, err := kbSuggestion(row)
	if err != nil {
		return result, err
	}
	result.Suggestion = &sg
	return result, nil
}
