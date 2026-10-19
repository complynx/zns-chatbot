package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func (s *Store) User(ctx context.Context, id uuid.UUID) (User, error) { return s.GetUser(ctx, id) }

// SetUserRevoked records a local revoke. On an ambiguous commit, a nonzero result
// supplies trusted subject metadata for cache invalidation, not proof of success.
func (s *Store) SetUserRevoked(ctx context.Context, id uuid.UUID, actor string) (User, error) {
	if actor == "" {
		return User{}, fmt.Errorf("revocation actor required")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return User{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	u, e := revokeUserTx(ctx, q, id, actor)
	if e != nil {
		return User{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return u, e
	}
	return u, nil
}

// revokeUserTx writes revocation and audit. The caller owns the transaction and
// authorization; an agent receipt can therefore share the same commit.
func revokeUserTx(ctx context.Context, q *sqlc.Queries, id uuid.UUID, actor string) (User, error) {
	n, e := q.RevokeUser(ctx, sqlc.RevokeUserParams{ID: id, Revoked: true})
	if e != nil {
		return User{}, e
	}
	if n == 0 {
		return User{}, pgx.ErrNoRows
	}
	if e = q.AuditIdentity(ctx, sqlc.AuditIdentityParams{UserID: id, Actor: actor, Revoked: true}); e != nil {
		return User{}, e
	}
	u, e := q.GetUser(ctx, id)
	if e != nil {
		return User{}, e
	}
	return user(u), nil
}
