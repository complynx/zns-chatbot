// Package applicationauth verifies the principal for one application operation.
package applicationauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")

// VerifyOwner resolves a credential using the live provider. Identity denial
// sentinels mean invalid credentials; other errors mean verification failed.
type VerifyOwner func(context.Context, string) (string, error)

type OwnerQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Principal is the result of one verification, not a reusable authorization grant.
type Principal struct{ owner string }

func (p Principal) Owner() string { return p.owner }

type Authorizer struct {
	DB     OwnerQuery
	Verify VerifyOwner
}

// Authorize must run for every operation, before accessing owner-scoped data.
func (a Authorizer) Authorize(ctx context.Context, token string) (Principal, error) {
	if token == "" || a.Verify == nil {
		return Principal{}, ErrUnauthorized
	}
	owner, err := a.Verify(ctx, token)
	if err != nil {
		if errors.Is(err, identity.ErrZitadelIdentity) || errors.Is(err, identity.ErrZitadelUserInactive) ||
			errors.Is(err, identity.ErrSandboxIdentity) ||
			errors.Is(err, ErrUnauthorized) {
			return Principal{}, fmt.Errorf("%w: %w", ErrUnauthorized, err)
		}
		return Principal{}, err
	}
	if owner == "" {
		return Principal{}, ErrUnauthorized
	}
	var exists bool
	if err = a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, owner).
		Scan(&exists); err != nil {
		return Principal{}, err
	}
	if !exists {
		return Principal{}, ErrForbidden
	}
	return Principal{owner: owner}, nil
}
