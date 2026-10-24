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
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func kbNormalize(c KnowledgeChange) (KnowledgeChange, []byte, error) {
	c.Reason = strings.TrimSpace(c.Reason)
	if !utf8.ValidString(c.Reason) || utf8.RuneCountInString(c.Reason) < 1 || utf8.RuneCountInString(c.Reason) > 2000 ||
		strings.IndexFunc(c.Reason, unicode.IsControl) >= 0 {
		return c, nil, kbInvalid("reason must contain 1..2000 characters without controls")
	}
	if c.ExpectedRevision < 0 || c.ExpectedModeratorVersion < 0 || c.EventID != nil && *c.EventID == uuid.Nil {
		return c, nil, kbInvalid("invalid revision or event ID")
	}
	if err := kbValidateChangeFields(c); err != nil {
		return c, nil, err
	}
	if c.Content != nil {
		content, err := c.Content.Normalize()
		if err != nil {
			return c, nil, err
		}
		c.Content = &content
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return c, nil, err
	}
	hash := sha256.Sum256(raw)
	return c, hash[:], nil
}
func kbLockEvent(ctx context.Context, q *sqlc.Queries, scope PaymentScope, id *uuid.UUID) (*sqlc.ShuttleEvent, error) {
	if id == nil {
		return nil, nil //nolint:nilnil // A general article has no event origin.
	}
	e, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: *id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	return &e, err
}
func kbModeratorFence(ctx context.Context, q *sqlc.Queries, scope PaymentScope) error {
	return q.LockEventOperation(ctx, fmt.Sprintf("knowledge-moderators:%d:%s", scope.BotID, scope.Organization))
}

func kbCurrentArticle(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	id uuid.UUID,
) (*sqlc.ShuttleKnowledgeArticle, error) {
	a, err := q.LockKnowledgeArticle(
		ctx,
		sqlc.LockKnowledgeArticleParams{BotID: scope.BotID, OrganizationID: scope.Organization, ID: id},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // An absent article is a valid creation target.
	}
	return &a, err
}
func kbHistorical(event *sqlc.ShuttleEvent, article *sqlc.ShuttleKnowledgeArticle, now time.Time) bool {
	return event != nil && (article != nil && article.ArchivedAt.Valid || !now.Before(event.EndsAt.Time))
}

// Event roles and knowledge publication serialize on the same event row. Named
// moderators serialize with assignment/revocation through a scope-local fence.
func kbAuthorize(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	event *sqlc.ShuttleEvent,
	article *sqlc.ShuttleKnowledgeArticle,
	now time.Time,
) error {
	archived := false
	if event != nil {
		var err error
		archived, err = q.KnowledgeEventArchived(
			ctx,
			sqlc.KnowledgeEventArchivedParams{
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				EventID:        kbUUID(event.ID),
			},
		)
		if err != nil {
			return err
		}
	}
	if event == nil || archived || kbHistorical(event, article, now) {
		if err := kbModeratorFence(ctx, q, scope); err != nil {
			return err
		}
		m, err := q.ReadKnowledgeModerator(
			ctx,
			sqlc.ReadKnowledgeModeratorParams{
				BotID:          scope.BotID,
				OrganizationID: scope.Organization,
				UserID:         scope.UserID,
			},
		)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !m.Enabled {
			return ErrKnowledgeDenied
		}
		return err
	}
	if scope.GlobalAdmin {
		return nil
	}
	r, err := q.ReadEventAuthority(
		ctx,
		sqlc.ReadEventAuthorityParams{
			EventID:        event.ID,
			UserID:         scope.UserID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && r.Role != AuthorityAdministrator {
		return ErrKnowledgeDenied
	}
	return err
}

type kbPreparationDocument struct {
	Contract     KnowledgeContract    `json:"contract"`
	Preparation  KnowledgePreparation `json:"preparation"`
	EventID      *uuid.UUID           `json:"event_id,omitempty"`
	EventVersion int64                `json:"event_version,omitempty"`
	Historical   bool                 `json:"historical"`
}
type kbTarget struct {
	article       *sqlc.ShuttleKnowledgeArticle
	event         *sqlc.ShuttleEvent
	suggestion    *sqlc.ShuttleKnowledgeSuggestion
	moderator     *sqlc.ShuttleKnowledgeModerator
	archiveOrigin *sqlc.ShuttleKnowledgeArticle
	historical    bool
	now           time.Time
}

func kbOperationLock(ctx context.Context, q *sqlc.Queries, scope PaymentScope, kind, op string) error {
	if !kbScope(scope) ||
		!adminScopeValid(
			AdminScope{
				BotID:        scope.BotID,
				Organization: scope.Organization,
				Subject:      scope.Subject,
				UserID:       scope.UserID,
			},
			op,
		) {
		return kbInvalid("valid scope and operation ID required")
	}
	return q.LockEventOperation(
		ctx,
		fmt.Sprintf("knowledge:%d:%s:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, kind, op),
	)
}

// Resolve immutable origins before event/actor/article locks. Article origins
// never change; conflicting creations are serialized again by article ID.
func kbResolve(ctx context.Context, q *sqlc.Queries, scope PaymentScope, c KnowledgeChange) (kbTarget, error) {
	var t kbTarget
	var err error
	if c.Kind == kbSetModerator {
		t, err = kbResolveModerator(ctx, q, scope, c)
	} else {
		t, err = kbResolveArticle(ctx, q, scope, c)
	}
	if err != nil {
		return t, err
	}
	if c.Kind != kbSetModerator {
		if err = kbCheckRevision(ctx, q, scope, c, t); err != nil {
			return t, err
		}
	}
	if err = kbRefreshTarget(ctx, q, scope, c, &t); err != nil {
		return t, err
	}
	// Named-moderator authorization may wait. Read time/lifecycle again afterward.
	if err = kbRefreshTarget(ctx, q, scope, c, &t); err != nil {
		return t, err
	}
	return t, nil
}
func kbPreparationContext(t kbTarget) kbPreparationDocument {
	p := kbPreparationDocument{Historical: t.historical}
	if t.event != nil {
		id := t.event.ID
		p.EventID = &id
		p.EventVersion = t.event.Version
	}
	return p
}
func kbDescription(c KnowledgeChange, t kbTarget) string {
	switch c.Kind {
	case kbSetModerator:
		return fmt.Sprintf(
			"Set knowledge moderator %s to enabled=%t at assignment version %d. Reaso" +
"n (untrusted text): %q. This controls general and historical knowledge e" +
"diting and review.",
			c.ModeratorUserID,
			*c.ModeratorEnabled,
			c.ExpectedModeratorVersion,
			c.Reason,
		)
	case kbReview:
		action := "Approve"
		if c.Decision == kbReject {
			action = "Reject"
		}
		return fmt.Sprintf(
			"%s suggestion %s by another user for article %s at revision %d. Proposed" +
" title (untrusted text): %q. Decision reason (untrusted text): %q. Appro" +
"val publishes the exact suggested content; rejection changes no article.",
			action,
			c.SuggestionID,
			t.suggestion.ArticleID,
			c.ExpectedRevision,
			t.suggestion.Title,
			c.Reason,
		)
	default:
		collection := "general knowledge"
		if t.event != nil {
			collection = "event knowledge"
			if t.historical {
				collection = "historical event knowledge"
			}
		}
		effect := "Publish exact replacement content"
		if c.ExpectedRevision == 0 {
			effect = "Publish a new article"
		}
		if c.Kind == kbSuggest {
			effect = "Submit exact content for independent moderator review; do not publish"
		}
		return fmt.Sprintf(
			"%s in %s: article %s, expected revision %d. Title (untrusted text): %q. " +
"Language: %s. Body: %d bytes; sources: %d. Reason (untrusted text): %q. " +
"The complete content remains in the exact arguments.",
			effect,
			collection,
			c.ArticleID,
			c.ExpectedRevision,
			c.Content.Title,
			c.Content.Language,
			len(c.Content.Body),
			len(c.Content.Sources),
			c.Reason,
		)
	}
}

func (s *Store) PrepareKnowledge(
	ctx context.Context,
	scope PaymentScope,
	op string,
	c KnowledgeChange,
) (KnowledgePreparation, error) {
	return s.prepareKnowledge(ctx, scope, op, c, nil, KnowledgeContract{})
}

// PrepareKnowledgeForContract records the issuing API contract with the original
// preparation. Older preparations are never relabelled during inspection.
func (s *Store) PrepareKnowledgeForContract(ctx context.Context, scope PaymentScope, op string,
	c KnowledgeChange, contract KnowledgeContract,
) (KnowledgePreparation, error) {
	return s.prepareKnowledge(ctx, scope, op, c, nil, contract)
}

// guard is a KB-local seam for native session/credential checks in this same
// transaction. Agent callers use the public method with no native session.
func (s *Store) prepareKnowledge(
	ctx context.Context,
	scope PaymentScope,
	op string,
	c KnowledgeChange,
	guard func(context.Context, *sqlc.Queries) error,
	contract KnowledgeContract,
) (KnowledgePreparation, error) {
	c, hash, err := kbNormalize(c)
	if err != nil {
		return KnowledgePreparation{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgePreparation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = kbOperationLock(ctx, q, scope, c.Kind, op); err != nil {
		return KnowledgePreparation{}, err
	}
	prior, err := q.ReadKnowledgePreparation(
		ctx,
		sqlc.ReadKnowledgePreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     c.Kind,
			OperationID:    op,
		},
	)
	if err == nil {
		return kbReplayPreparation(ctx, q, scope, prior, hash, guard)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return KnowledgePreparation{}, err
	}
	t, err := kbResolve(ctx, q, scope, c)
	if err != nil {
		return KnowledgePreparation{}, err
	}
	if err = kbRunGuard(ctx, q, guard); err != nil {
		return KnowledgePreparation{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return KnowledgePreparation{}, err
	}
	t.now = now.Time
	t.historical = t.archiveOrigin != nil || kbHistorical(t.event, t.article, t.now)
	if c.Kind == kbEdit || c.Kind == kbReview {
		if err = kbAuthorize(ctx, q, scope, t.event, t.article, t.now); err != nil {
			return KnowledgePreparation{}, err
		}
	}
	p := kbPreparationContext(t)
	p.Contract = contract
	p.Preparation = KnowledgePreparation{
		ID:          uuid.New(),
		Arguments:   c,
		Description: kbDescription(c, t),
		ExpiresAt:   t.now.Add(kbPreparationTTL),
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return KnowledgePreparation{}, err
	}
	err = q.InsertKnowledgePreparation(
		ctx,
		sqlc.InsertKnowledgePreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActorID:        scope.UserID,
			ActionKind:     c.Kind,
			OperationID:    op,
			ProposalID:     p.Preparation.ID,
			RequestHash:    hash,
			Document:       raw,
			ExpiresAt:      pgtype.Timestamptz{Time: p.Preparation.ExpiresAt, Valid: true},
		},
	)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return p.Preparation, err
}

func (s *Store) ConfirmKnowledge(
	ctx context.Context,
	scope PaymentScope,
	op string,
	c KnowledgeChange,
	proposal uuid.UUID,
) (KnowledgeResult, error) {
	return s.confirmKnowledge(ctx, scope, op, c, proposal, nil)
}

func (s *Store) confirmKnowledge(
	ctx context.Context,
	scope PaymentScope,
	op string,
	c KnowledgeChange,
	proposal uuid.UUID,
	guard func(context.Context, *sqlc.Queries) error,
) (KnowledgeResult, error) {
	c, hash, err := kbNormalize(c)
	if err != nil {
		return KnowledgeResult{}, err
	}
	if proposal == uuid.Nil {
		return KnowledgeResult{}, kbInvalid("exact proposal ID required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = kbOperationLock(ctx, q, scope, c.Kind, op); err != nil {
		return KnowledgeResult{}, err
	}
	prior, err := q.ReadKnowledgeOperation(
		ctx,
		sqlc.ReadKnowledgeOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     c.Kind,
			OperationID:    op,
		},
	)
	if err == nil {
		return kbReplayResult(ctx, q, scope, prior, hash, proposal, guard)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return KnowledgeResult{}, err
	}
	return kbConfirmNew(ctx, tx, q, scope, op, c, hash, proposal, guard)
}
func (s *Store) KnowledgeOperation(ctx context.Context, scope PaymentScope, kind, op string) (KnowledgeResult, error) {
	if kind != kbSuggest && kind != kbEdit && kind != kbReview && kind != kbSetModerator {
		return KnowledgeResult{}, kbInvalid("unknown knowledge action")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return KnowledgeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = kbOperationLock(ctx, q, scope, kind, op); err != nil {
		return KnowledgeResult{}, err
	}
	if err = kbActor(ctx, q, scope); err != nil {
		return KnowledgeResult{}, err
	}
	row, err := q.ReadKnowledgeOperation(
		ctx,
		sqlc.ReadKnowledgeOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     kind,
			OperationID:    op,
		},
	)
	if err != nil {
		return KnowledgeResult{}, err
	}
	if row.ActorID != scope.UserID {
		return KnowledgeResult{}, ErrKnowledgeDenied
	}
	var result KnowledgeResult
	err = json.Unmarshal(row.Result, &result)
	return result, err
}

func kbValidateChangeFields(c KnowledgeChange) error {
	moderatorFields := c.ModeratorUserID != uuid.Nil || c.ModeratorEnabled != nil || c.ExpectedModeratorVersion != 0
	switch c.Kind {
	case kbSuggest, kbEdit:
		if c.ArticleID == uuid.Nil || c.Content == nil || c.SuggestionID != uuid.Nil || c.Decision != "" ||
			moderatorFields {
			return kbInvalid("article ID, content and only edit/suggestion fields required")
		}
	case kbReview:
		if c.SuggestionID == uuid.Nil || c.ArticleID != uuid.Nil || c.Content != nil || c.EventID != nil ||
			moderatorFields ||
			(c.Decision != kbApprove && c.Decision != kbReject) {
			return kbInvalid("suggestion ID, expected revision, approve/reject and reason required")
		}
	case kbSetModerator:
		if c.ModeratorUserID == uuid.Nil || c.ModeratorEnabled == nil || c.ArticleID != uuid.Nil || c.Content != nil ||
			c.EventID != nil ||
			c.SuggestionID != uuid.Nil ||
			c.Decision != "" ||
			c.ExpectedRevision != 0 {
			return kbInvalid("only moderator target, enabled, expected version and reason allowed")
		}
	default:
		return kbInvalid("unknown knowledge action")
	}
	return nil
}

func kbConfirmNew(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	scope PaymentScope,
	op string,
	c KnowledgeChange,
	hash []byte,
	proposal uuid.UUID,
	guard func(context.Context, *sqlc.Queries) error,
) (KnowledgeResult, error) {
	prep, err := q.ReadKnowledgePreparation(
		ctx,
		sqlc.ReadKnowledgePreparationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActionKind:     c.Kind,
			OperationID:    op,
		},
	)
	if err != nil {
		return KnowledgeResult{}, err
	}
	if prep.ActorID != scope.UserID || prep.ProposalID != proposal || !bytes.Equal(prep.RequestHash, hash) {
		return KnowledgeResult{}, ErrConflict
	}
	var document kbPreparationDocument
	if err = json.Unmarshal(prep.Document, &document); err != nil {
		return KnowledgeResult{}, err
	}
	t, err := kbResolve(ctx, q, scope, c)
	if err != nil {
		return KnowledgeResult{}, err
	}
	if err = kbRunGuard(ctx, q, guard); err != nil {
		return KnowledgeResult{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return KnowledgeResult{}, err
	}
	t.now = now.Time
	t.historical = t.archiveOrigin != nil || kbHistorical(t.event, t.article, t.now)
	current := kbPreparationContext(t)
	if !t.now.Before(prep.ExpiresAt.Time) || document.EventVersion != current.EventVersion ||
		document.Historical != current.Historical {
		return KnowledgeResult{}, ErrConflict
	}
	result, err := kbApply(ctx, q, scope, c, t)
	if err != nil {
		return KnowledgeResult{}, paymentStorageError(err)
	}
	// Uniqueness constraints and native guards may also wait. No effect survives
	// if consent expired or the event ended during that wait.
	if err = kbRunGuard(ctx, q, guard); err != nil {
		return KnowledgeResult{}, err
	}
	now, err = q.RegistrationNow(ctx)
	if err != nil {
		return KnowledgeResult{}, err
	}
	if !now.Time.Before(prep.ExpiresAt.Time) ||
		document.Historical != (t.archiveOrigin != nil || kbHistorical(t.event, t.article, now.Time)) {
		return KnowledgeResult{}, ErrConflict
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return KnowledgeResult{}, err
	}
	err = q.InsertKnowledgeOperation(
		ctx,
		sqlc.InsertKnowledgeOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ActorID:        scope.UserID,
			ActionKind:     c.Kind,
			OperationID:    op,
			ProposalID:     proposal,
			RequestHash:    hash,
			Result:         raw,
		},
	)
	if err == nil {
		err = tx.Commit(ctx)
	}
	return result, err
}
