package db

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// MiniAppKnowledgeSession binds native knowledge writes to the current browser
// session. AuthorityUnchanged checks only local invalidation and token expiry.
type MiniAppKnowledgeSession struct {
	Contract           KnowledgeContract
	UserID             uuid.UUID
	Bearer, Generation [sha256.Size]byte
	AuthorityUnchanged func() bool
}

func (s *Store) PrepareMiniAppKnowledge(ctx context.Context, scope PaymentScope, operation string,
	change KnowledgeChange, session MiniAppKnowledgeSession,
) (KnowledgePreparation, error) {
	return s.prepareKnowledge(ctx, scope, operation, change, knowledgeMiniAppGuard(scope, session), session.Contract)
}

func (s *Store) ConfirmMiniAppKnowledge(ctx context.Context, scope PaymentScope, operation string,
	change KnowledgeChange, proposal uuid.UUID, session MiniAppKnowledgeSession,
) (KnowledgeResult, error) {
	return s.confirmKnowledge(ctx, scope, operation, change, proposal, knowledgeMiniAppGuard(scope, session))
}

// The mutation engine invokes this after event/actor locks and again after any
// publication wait, before the effect and its receipt commit together.
func knowledgeMiniAppGuard(
	scope PaymentScope,
	session MiniAppKnowledgeSession,
) func(context.Context, *sqlc.Queries) error {
	return func(ctx context.Context, q *sqlc.Queries) error {
		if session.UserID != scope.UserID || session.AuthorityUnchanged == nil || !session.AuthorityUnchanged() {
			return ErrMiniAppDenied
		}
		owner, err := q.LockAuthorityActor(
			ctx,
			sqlc.LockAuthorityActorParams{
				BotID:          scope.BotID,
				ZitadelOrg:     scope.Organization,
				ZitadelSubject: scope.Subject,
			},
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMiniAppDenied
		}
		if err != nil {
			return err
		}
		if owner.ID != session.UserID {
			return ErrMiniAppDenied
		}
		if err = checkMiniAppActorSession(ctx, q, owner, session.Bearer, session.Generation); err != nil {
			return err
		}
		if !session.AuthorityUnchanged() {
			return ErrMiniAppDenied
		}
		return nil
	}
}
