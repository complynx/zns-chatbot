package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

var (
	ErrAdminInvalid = errors.New("invalid administrative operation")
	ErrAdminDenied  = errors.New("active global administrator required")
)

// AdminScope comes from verified identity, never tool arguments.
type AdminScope struct {
	BotID                 int64
	Organization, Subject string
	UserID                uuid.UUID
	GlobalAdmin           bool
}

func adminScopeValid(scope AdminScope, operation string) bool {
	if scope.BotID <= 0 || scope.Organization == "" || scope.Subject == "" || scope.UserID == uuid.Nil ||
		len(operation) == 0 || len(operation) > 128 {
		return false
	}
	for i, c := range operation {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i == 0 || !strings.ContainsRune("._:-", c) {
			return false
		}
	}
	return true
}

// Lock the namespaced operation, then exact active actor, before business effects.
// IAM roles remain the API's responsibility.
func (s *Store) beginAdminOperation(
	ctx context.Context, scope AdminScope, kind, op string,
) (pgx.Tx, *sqlc.Queries, error) {
	if !adminScopeValid(scope, op) {
		return nil, nil, ErrAdminInvalid
	}
	if !scope.GlobalAdmin {
		return nil, nil, ErrAdminDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := s.q.WithTx(tx)
	err = q.LockEventOperation(
		ctx,
		fmt.Sprintf("%s:%d:%s:%s:%s", kind, scope.BotID, scope.Organization, scope.Subject, op),
	)
	if err == nil {
		var actor sqlc.ShuttleUser
		actor, err = q.LockRegistrationUser(ctx, sqlc.LockRegistrationUserParams{
			ID: scope.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization,
		})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && actor.ZitadelSubject != scope.Subject {
			err = ErrAdminDenied
		}
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, q, nil
}
