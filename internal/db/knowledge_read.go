package db

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func kbUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil} }
func kbUUIDPointer(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := uuid.UUID(id.Bytes)
	return &v
}
func kbTime(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}
func kbEvent(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return kbUUID(*id)
}
func kbInvalid(message string) error { return fmt.Errorf("%w: %s", knowledge.ErrInvalid, message) }
func kbScope(scope PaymentScope) bool {
	return scope.BotID > 0 && scope.Organization != "" && scope.Subject != "" && scope.UserID != uuid.Nil
}
func kbActor(ctx context.Context, q *sqlc.Queries, scope PaymentScope) error {
	if !kbScope(scope) {
		return kbInvalid("authenticated knowledge scope required")
	}
	u, err := q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && u.ID != scope.UserID {
		return ErrKnowledgeDenied
	}
	return err
}
func (s *Store) kbRead(ctx context.Context, scope PaymentScope) (pgx.Tx, *sqlc.Queries, error) {
	if !kbScope(scope) {
		return nil, nil, kbInvalid("authenticated knowledge scope required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	if err = kbActor(ctx, q, scope); err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, q, nil
}
func kbArticle(row sqlc.ShuttleKnowledgeArticle, historical bool) (KnowledgeArticle, error) {
	a := KnowledgeArticle{
		ID:              row.ID,
		Revision:        row.CurrentRevision,
		CurrentRevision: row.CurrentRevision,
		Content: knowledge.Content{
			Slug:     row.Slug,
			Title:    row.Title,
			Language: row.Language,
			Topics:   row.Topics,
			Body:     row.Body,
		},
		EventID:       kbUUIDPointer(row.EventID),
		EventName:     row.EventName.String,
		EventStartsAt: kbTime(row.EventStartsAt),
		EventEndsAt:   kbTime(row.EventEndsAt),
		EventTimezone: row.EventTimezone.String,
		ArchivedAt:    kbTime(row.ArchivedAt),
		Historical:    historical,
		UpdatedAt:     row.UpdatedAt.Time,
	}
	err := json.Unmarshal(row.Sources, &a.Content.Sources)
	return a, err
}
func kbSuggestion(row sqlc.ShuttleKnowledgeSuggestion) (KnowledgeSuggestion, error) {
	a := KnowledgeSuggestion{
		ID:           row.ID,
		ArticleID:    row.ArticleID,
		BaseRevision: row.BaseRevision,
		EventID:      kbUUIDPointer(row.EventID),
		EventVersion: row.EventVersion.Int64,
		ProposerID:   row.ProposerID,
		Reason:       row.Reason,
		Content: knowledge.Content{
			Slug:     row.Slug,
			Title:    row.Title,
			Language: row.Language,
			Topics:   row.Topics,
			Body:     row.Body,
		},
		State:             row.State,
		ReviewerID:        kbUUIDPointer(row.ReviewerID),
		DecisionReason:    row.DecisionReason.String,
		PublishedRevision: row.PublishedRevision.Int64,
		CreatedAt:         row.CreatedAt.Time,
		DecidedAt:         kbTime(row.DecidedAt),
	}
	err := json.Unmarshal(row.Sources, &a.Content.Sources)
	return a, err
}
func kbModerator(row sqlc.ShuttleKnowledgeModerator) KnowledgeModerator {
	return KnowledgeModerator{
		UserID:    row.UserID,
		Enabled:   row.Enabled,
		Version:   row.Version,
		ChangedBy: row.ChangedBy,
		ChangedAt: row.ChangedAt.Time,
	}
}

// KnowledgeModerators exposes assignment versions only to current global admins.
func (s *Store) KnowledgeModerators(
	ctx context.Context,
	scope PaymentScope,
	after uuid.UUID,
	limit int,
) (KnowledgeModeratorPage, error) {
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return KnowledgeModeratorPage{}, kbInvalid("limit must be 1..20")
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeModeratorPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if !scope.GlobalAdmin {
		return KnowledgeModeratorPage{}, ErrKnowledgeDenied
	}
	rows, err := q.ListKnowledgeModerators(
		ctx,
		sqlc.ListKnowledgeModeratorsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			UserID:         after,
			Limit:          int32(limit + 1),
		},
	)
	if err != nil {
		return KnowledgeModeratorPage{}, err
	}
	page := KnowledgeModeratorPage{Items: make([]KnowledgeModerator, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		id := rows[limit-1].UserID
		page.Next = &id
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(page.Items, kbModerator(row))
	}
	return page, nil
}

// ReadKnowledge returns only published, immutable revisions. Zero selects current.
func (s *Store) ReadKnowledge(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	revision int64,
) (KnowledgeArticle, error) {
	if id == uuid.Nil || revision < 0 {
		return KnowledgeArticle{}, kbInvalid("article ID and nonnegative revision required")
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeArticle{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	a, err := q.ReadKnowledgeArticle(
		ctx,
		sqlc.ReadKnowledgeArticleParams{BotID: scope.BotID, OrganizationID: scope.Organization, ArticleID: id},
	)
	if err != nil {
		return KnowledgeArticle{}, err
	}
	if revision == 0 || revision == a.ShuttleKnowledgeArticle.CurrentRevision {
		return kbArticle(a.ShuttleKnowledgeArticle, a.Historical)
	}
	r, err := q.ReadKnowledgeRevision(
		ctx,
		sqlc.ReadKnowledgeRevisionParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ArticleID:      id,
			Revision:       revision,
		},
	)
	if err != nil {
		return KnowledgeArticle{}, err
	}
	result, err := kbArticle(
		sqlc.ShuttleKnowledgeArticle{
			ID:              id,
			CurrentRevision: r.Revision,
			Slug:            r.Slug,
			Title:           r.Title,
			Language:        r.Language,
			Topics:          r.Topics,
			Body:            r.Body,
			Sources:         r.Sources,
			EventID:         r.EventID,
			EventName:       r.EventName,
			EventStartsAt:   r.EventStartsAt,
			EventEndsAt:     r.EventEndsAt,
			EventTimezone:   r.EventTimezone,
			ArchivedAt:      r.ArchivedAt,
			UpdatedAt:       r.CreatedAt,
		},
		a.Historical,
	)
	result.CurrentRevision = a.ShuttleKnowledgeArticle.CurrentRevision
	return result, err
}

func (s *Store) KnowledgeRevisions(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	before int64,
	limit int,
) (KnowledgeRevisionPage, error) {
	if id == uuid.Nil || before < 0 {
		return KnowledgeRevisionPage{}, kbInvalid("article ID and nonnegative revision cursor required")
	}
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return KnowledgeRevisionPage{}, kbInvalid("limit must be 1..20")
	}
	if before == 0 {
		before = math.MaxInt64
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeRevisionPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = q.ReadKnowledgeArticle(
		ctx,
		sqlc.ReadKnowledgeArticleParams{BotID: scope.BotID, OrganizationID: scope.Organization, ArticleID: id},
	); err != nil {
		return KnowledgeRevisionPage{}, err
	}
	rows, err := q.ListKnowledgeRevisions(
		ctx,
		sqlc.ListKnowledgeRevisionsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			ArticleID:      id,
			BeforeRevision: before,
			PageLimit:      int32(limit + 1),
		},
	)
	if err != nil {
		return KnowledgeRevisionPage{}, err
	}
	p := KnowledgeRevisionPage{Items: make([]KnowledgeRevision, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		v := rows[limit-1].Revision
		p.Next = &v
		rows = rows[:limit]
	}
	for _, r := range rows {
		p.Items = append(
			p.Items,
			KnowledgeRevision{
				Revision:         r.Revision,
				PreviousRevision: r.PreviousRevision.Int64,
				ChangeKind:       r.ChangeKind,
				AuthorID:         kbUUIDPointer(r.AuthorID),
				ProposerID:       kbUUIDPointer(r.ProposerID),
				ReviewerID:       kbUUIDPointer(r.ReviewerID),
				Reason:           r.Reason,
				CreatedAt:        r.CreatedAt.Time,
			},
		)
	}
	return p, nil
}

type kbSearchCursor struct {
	Offset int32    `json:"offset"`
	Hash   [32]byte `json:"hash"`
}

func (s *Store) SearchKnowledge(
	ctx context.Context,
	scope PaymentScope,
	r knowledge.SearchRequest,
) (KnowledgeSearchPage, error) {
	r, err := r.Normalize()
	if err != nil {
		return KnowledgeSearchPage{}, err
	}
	if r.Limit < 1 || r.Limit > knowledge.MaxLimit {
		return KnowledgeSearchPage{}, kbInvalid("limit must be 1..20")
	}
	cursor := r.Cursor
	r.Cursor = ""
	raw, _ := json.Marshal(r)
	hash := sha256.Sum256(raw)
	var offset int32
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		var c kbSearchCursor
		if e != nil || json.Unmarshal(b, &c) != nil || c.Hash != hash || c.Offset < 0 || c.Offset > 1000000 {
			return KnowledgeSearchPage{}, kbInvalid("cursor does not match search filters")
		}
		offset = c.Offset
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeSearchPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := q.SearchKnowledgeArticles(
		ctx,
		sqlc.SearchKnowledgeArticlesParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Queries:        r.Queries,
			Collection:     string(r.Collection),
			EventID:        kbEvent(r.EventID),
			Language:       r.Language,
			Topic:          r.Topic,
			PageLimit:      int32(r.Limit + 1),
			PageOffset:     offset,
		},
	)
	if err != nil {
		return KnowledgeSearchPage{}, err
	}
	p := KnowledgeSearchPage{Items: make([]KnowledgeSearchHit, 0, min(r.Limit, len(rows)))}
	if len(rows) > r.Limit {
		b, _ := json.Marshal(kbSearchCursor{Offset: offset + int32(r.Limit), Hash: hash})
		p.Next = base64.RawURLEncoding.EncodeToString(b)
		rows = rows[:r.Limit]
	}
	for _, row := range rows {
		a, e := kbArticle(
			sqlc.ShuttleKnowledgeArticle{
				ID:              row.ID,
				CurrentRevision: row.CurrentRevision,
				Slug:            row.Slug,
				Title:           row.Title,
				Language:        row.Language,
				Topics:          row.Topics,
				Body:            row.Body,
				Sources:         row.Sources,
				EventID:         row.EventID,
				EventName:       row.EventName,
				EventStartsAt:   row.EventStartsAt,
				EventEndsAt:     row.EventEndsAt,
				EventTimezone:   row.EventTimezone,
				ArchivedAt:      row.ArchivedAt,
				UpdatedAt:       row.UpdatedAt,
			},
			row.Historical,
		)
		if e != nil {
			return KnowledgeSearchPage{}, e
		}
		p.Items = append(p.Items, KnowledgeSearchHit{Article: a, MatchedQueries: row.MatchedQueries})
	}
	return p, nil
}

func (s *Store) ListKnowledge(
	ctx context.Context,
	scope PaymentScope,
	r KnowledgeListRequest,
) (KnowledgeArticlePage, error) {
	filter, err := (knowledge.SearchRequest{Collection: r.Collection, EventID: r.EventID, Language: r.Language, Topic: r.Topic, Queries: []string{"browse"}, Limit: r.Limit}).Normalize()
	if err != nil {
		return KnowledgeArticlePage{}, err
	}
	if filter.Limit < 1 || filter.Limit > knowledge.MaxLimit {
		return KnowledgeArticlePage{}, kbInvalid("limit must be 1..20")
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeArticlePage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := q.ListKnowledgeArticles(
		ctx,
		sqlc.ListKnowledgeArticlesParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Collection:     string(filter.Collection),
			EventID:        kbEvent(filter.EventID),
			Language:       filter.Language,
			Topic:          filter.Topic,
			AfterID:        r.After,
			PageLimit:      int32(filter.Limit + 1),
		},
	)
	if err != nil {
		return KnowledgeArticlePage{}, err
	}
	p := KnowledgeArticlePage{Items: make([]KnowledgeArticle, 0, min(filter.Limit, len(rows)))}
	if len(rows) > filter.Limit {
		v := rows[filter.Limit-1].ShuttleKnowledgeArticle.ID
		p.Next = &v
		rows = rows[:filter.Limit]
	}
	for _, r := range rows {
		a, e := kbArticle(r.ShuttleKnowledgeArticle, r.Historical)
		if e != nil {
			return KnowledgeArticlePage{}, e
		}
		p.Items = append(p.Items, a)
	}
	return p, nil
}
func (s *Store) KnowledgeOverview(ctx context.Context, scope PaymentScope) (KnowledgeOverview, error) {
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeOverview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := q.KnowledgeOverviewCounts(
		ctx,
		sqlc.KnowledgeOverviewCountsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return KnowledgeOverview{}, err
	}
	topics, err := q.KnowledgeOverviewTopics(
		ctx,
		sqlc.KnowledgeOverviewTopicsParams{BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return KnowledgeOverview{}, err
	}
	p := KnowledgeOverview{
		Items:     []KnowledgeOverviewItem{},
		Topics:    []KnowledgeTopic{},
		Truncated: len(rows) > kbOverviewLimit || len(topics) > kbTopicLimit,
	}
	for _, r := range rows[:min(kbOverviewLimit, len(rows))] {
		var eventID *uuid.UUID
		if r.EventKey != "" {
			id, e := uuid.Parse(r.EventKey)
			if e != nil {
				return KnowledgeOverview{}, e
			}
			eventID = &id
		}
		p.Items = append(
			p.Items,
			KnowledgeOverviewItem{
				Collection: knowledge.Collection(r.Collection),
				EventID:    eventID,
				EventName:  r.EventName,
				Count:      r.Count,
			},
		)
	}
	for _, r := range topics[:min(kbTopicLimit, len(topics))] {
		p.Topics = append(p.Topics, KnowledgeTopic{Topic: r.Topic, Count: r.Count})
	}
	return p, nil
}

func (s *Store) KnowledgeSuggestions(
	ctx context.Context,
	scope PaymentScope,
	r KnowledgeSuggestionRequest,
) (KnowledgeSuggestionPage, error) {
	if r.Limit == 0 {
		r.Limit = 10
	}
	if r.Limit < 1 || r.Limit > 20 || r.EventID != nil && *r.EventID == uuid.Nil {
		return KnowledgeSuggestionPage{}, kbInvalid("valid event ID and limit 1..20 required")
	}
	tx, q, err := s.kbRead(ctx, scope)
	if err != nil {
		return KnowledgeSuggestionPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := q.ListKnowledgeSuggestions(
		ctx,
		sqlc.ListKnowledgeSuggestionsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			UserID:         scope.UserID,
			GlobalAdmin:    scope.GlobalAdmin,
			Pending:        r.Pending,
			EventID:        kbEvent(r.EventID),
			AfterID:        r.After,
			PageLimit:      int32(r.Limit + 1),
		},
	)
	if err != nil {
		return KnowledgeSuggestionPage{}, err
	}
	p := KnowledgeSuggestionPage{Items: make([]KnowledgeSuggestion, 0, min(r.Limit, len(rows)))}
	if len(rows) > r.Limit {
		v := rows[r.Limit-1].ID
		p.Next = &v
		rows = rows[:r.Limit]
	}
	for _, row := range rows {
		v, e := kbSuggestion(row)
		if e != nil {
			return KnowledgeSuggestionPage{}, e
		}
		p.Items = append(p.Items, v)
	}
	return p, nil
}

func (s *Store) ReadKnowledgeSuggestion(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
) (KnowledgeSuggestion, error) {
	if id == uuid.Nil {
		return KnowledgeSuggestion{}, kbInvalid("suggestion ID required")
	}
	// Resolve immutable origin before taking the shared event -> actor lock order.
	row, err := s.q.ReadKnowledgeSuggestion(
		ctx,
		sqlc.ReadKnowledgeSuggestionParams{BotID: scope.BotID, OrganizationID: scope.Organization, ID: id},
	)
	if err != nil {
		return KnowledgeSuggestion{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgeSuggestion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	event, err := kbLockEvent(ctx, q, scope, kbUUIDPointer(row.EventID))
	if err != nil {
		return KnowledgeSuggestion{}, err
	}
	if err = kbActor(ctx, q, scope); err != nil {
		return KnowledgeSuggestion{}, err
	}
	if row.ProposerID != scope.UserID {
		a, e := kbCurrentArticle(ctx, q, scope, row.ArticleID)
		if e != nil {
			return KnowledgeSuggestion{}, e
		}
		now, e := q.RegistrationNow(ctx)
		if e != nil {
			return KnowledgeSuggestion{}, e
		}
		if e = kbAuthorize(ctx, q, scope, event, a, now.Time); e != nil {
			return KnowledgeSuggestion{}, e
		}
	}
	row, err = q.ReadKnowledgeSuggestion(
		ctx,
		sqlc.ReadKnowledgeSuggestionParams{BotID: scope.BotID, OrganizationID: scope.Organization, ID: id},
	)
	if err != nil {
		return KnowledgeSuggestion{}, err
	}
	return kbSuggestion(row)
}
