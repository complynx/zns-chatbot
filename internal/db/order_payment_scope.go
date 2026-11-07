package db

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderModuleServices = "orders"
const orderModuleMeals = "meals"

func orderPaymentModule(kind OrderKind) (string, error) {
	switch kind {
	case OrderKindService:
		return orderModuleServices, nil
	case OrderKindMeal:
		return orderModuleMeals, nil
	default:
		return "", orders.ErrSelection
	}
}

// The caller holds the event and current owner locks. A module toggle never
// grants access to another kind; assigned unpaid passes remain eligible.
func authorizeOrderPaymentKind(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event uuid.UUID, kind OrderKind,
) error {
	module, err := orderPaymentModule(kind)
	if err != nil {
		return err
	}
	allowed, err := q.UserModuleAvailable(ctx, sqlc.UserModuleAvailableParams{
		ID: event, BotID: scope.BotID, OrganizationID: scope.Organization, ID_2: scope.UserID, Column5: module,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return pgx.ErrNoRows
	}
	return nil
}

func authorizeAnyOrderPayment(ctx context.Context, q *sqlc.Queries, scope ProfileScope, event uuid.UUID) error {
	for _, kind := range []OrderKind{OrderKindService, OrderKindMeal} {
		err := authorizeOrderPaymentKind(ctx, q, scope, event, kind)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return pgx.ErrNoRows
}

// Evidence lists may span both enabled modules; target-specific operations
// repeat authorization against the persisted kind before reading any receipt.
func (s *Store) orderPaymentOwnerTx(ctx context.Context, scope ProfileScope,
	event, operation uuid.UUID,
) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	if operation != uuid.Nil {
		err = q.LockEventOperation(ctx, fmt.Sprintf("orders:%s:%s", scope.UserID, operation))
	}
	if err == nil {
		_, err = q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
			ID: event, BotID: scope.BotID, OrganizationID: scope.Organization,
		})
	}
	if err == nil {
		err = lockProfileOwner(ctx, tx, scope, false)
	}
	if err == nil {
		err = authorizeAnyOrderPayment(ctx, q, scope, event)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}

// Event -> actor locking is shared by both payment kinds. A zero evidence ID
// is discovery/list authorization only; list queries must filter each row by kind.
func lockOrderPaymentEvent(ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	event, evidence uuid.UUID, authority bool,
) (sqlc.ShuttleEvent, string, error) {
	row, err := q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
		ID: event, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return row, "", err
	}
	module := orderModuleServices
	if evidence != uuid.Nil {
		proof, readErr := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: evidence, EventID: event})
		if readErr != nil {
			return row, "", readErr
		}
		module, err = orderPaymentModule(OrderKind(proof.OrderKind))
		if err != nil {
			return row, "", err
		}
	} else if !slices.Contains(row.Modules, module) {
		module = orderModuleMeals
	}
	return lockPaymentModuleEvent(ctx, q, scope, event, module, authority)
}

func authorizeOrderEvidenceOwner(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event, id uuid.UUID,
) error {
	row, err := q.ReadOrderEvidence(ctx, sqlc.ReadOrderEvidenceParams{ID: id, EventID: event})
	if err != nil {
		return err
	}
	if row.UserID != scope.UserID {
		return pgx.ErrNoRows
	}
	return authorizeOrderPaymentKind(ctx, q, scope, event, OrderKind(row.OrderKind))
}
