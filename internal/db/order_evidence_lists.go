package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
	"github.com/complynx/zns-chatbot/internal/payments"
)

type OrderEvidencePage struct {
	Evidence []OrderPaymentEvidence `json:"evidence"`
	Next     *uuid.UUID             `json:"next,omitempty"`
}

// HasOrdersEvidenceAuthority is discovery only; each read checks its event role.
func (s *Store) HasOrdersEvidenceAuthority(ctx context.Context, scope PaymentScope) (bool, error) {
	actor, err := s.q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return false, err
	}
	if actor.ID != scope.UserID {
		return false, payments.ErrDenied
	}
	if scope.GlobalAdmin {
		return true, nil
	}
	return s.q.HasOrdersEvidenceAuthority(
		ctx,
		sqlc.HasOrdersEvidenceAuthorityParams{
			UserID:         scope.UserID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
		},
	)
}

// OrderEvidenceList returns owner history or a current authority's unresolved
// event queue. UUID order is a stable cursor, not submission priority.
func (s *Store) OrderEvidenceList(
	ctx context.Context,
	scope PaymentScope,
	event, order, after uuid.UUID,
	limit int32,
	audit bool,
) (OrderEvidencePage, error) {
	if limit < 1 || limit > 20 {
		return OrderEvidencePage{}, orders.ErrSelection
	}
	var tx pgx.Tx
	var err error
	owner := scope.UserID
	if audit {
		tx, err = s.orderEvidenceAuthorityTx(ctx, scope, event)
		owner = uuid.Nil
	} else {
		tx, err = s.orderTx(ctx, orderEvidenceProfile(scope), event, uuid.Nil)
	}
	if err != nil {
		return OrderEvidencePage{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	return readOrderEvidencePage(ctx, q, sqlc.ListOrderEvidenceParams{
		EventID:        event,
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		OwnerID:        owner,
		OrderID:        order,
		UnresolvedOnly: audit,
		AfterID:        after,
		PageLimit:      limit + 1,
	})
}

func readOrderEvidencePage(
	ctx context.Context,
	q *sqlc.Queries,
	args sqlc.ListOrderEvidenceParams,
) (OrderEvidencePage, error) {
	rows, err := q.ListOrderEvidence(ctx, args)
	if err != nil {
		return OrderEvidencePage{}, err
	}
	result := OrderEvidencePage{Evidence: []OrderPaymentEvidence{}}
	limit := args.PageLimit - 1
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		evidence, decodeErr := loadOrderEvidenceDecision(ctx, q, row)
		if decodeErr != nil {
			return OrderEvidencePage{}, decodeErr
		}
		result.Evidence = append(result.Evidence, evidence)
	}
	if more {
		result.Next = &result.Evidence[len(result.Evidence)-1].ID
	}
	return result, nil
}
