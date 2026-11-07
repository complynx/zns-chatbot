package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const (
	paymentExpectationDefaultLimit = 5
	paymentExpectationMaxLimit     = 20
	paymentExpectationPageBytes    = 32 << 10
)

type PaymentExpectation struct {
	EventID              uuid.UUID        `json:"event_id"`
	EventName            string           `json:"event_name"`
	Timezone             string           `json:"timezone"`
	Balance              payments.Balance `json:"balance"`
	Purpose              string           `json:"purpose"`
	InitialDueAt         time.Time        `json:"initial_due_at"`
	FinalDueAt           time.Time        `json:"final_due_at"`
	DueAt                time.Time        `json:"due_at"`
	Overdue              bool             `json:"overdue"`
	PendingEvidence      int64            `json:"pending_evidence"`
	AwaitingConfirmation bool             `json:"awaiting_confirmation"`
}

type PaymentExpectations struct {
	ObservedAt   time.Time            `json:"observed_at"`
	Expectations []PaymentExpectation `json:"expectations"`
	HasMore      bool                 `json:"has_more"`
	Next         *uuid.UUID           `json:"next,omitempty"`
}

// OwnPaymentExpectations reads only current assigned obligations, never authority
// queues. Pending evidence remains separate from the human-confirmed balance.
// The transaction owns its connection; helpers must not acquire another one.
func (s *Store) OwnPaymentExpectations(
	ctx context.Context,
	scope ProfileScope,
	after uuid.UUID,
	limit int32,
) (PaymentExpectations, error) {
	result := PaymentExpectations{Expectations: []PaymentExpectation{}}
	if limit == 0 {
		limit = paymentExpectationDefaultLimit
	}
	if limit < 1 || limit > paymentExpectationMaxLimit {
		return result, payments.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return result, err
	}
	q := s.q.WithTx(tx)
	owner, err := q.GetUserBySubject(
		ctx,
		sqlc.GetUserBySubjectParams{BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject},
	)
	if err != nil {
		return result, err
	}
	if owner.ID != scope.UserID || owner.Revoked {
		return result, pgx.ErrNoRows
	}
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&result.ObservedAt); err != nil {
		return result, err
	}
	result.ObservedAt = result.ObservedAt.UTC()
	rows, err := q.ListOwnPaymentExpectations(ctx, sqlc.ListOwnPaymentExpectationsParams{UserID: scope.UserID,
		BotID: scope.BotID, OrganizationID: scope.Organization, AfterID: after, PageLimit: limit + 1})
	if err != nil {
		return result, err
	}
	return paymentExpectationPage(ctx, q, result, rows, limit)
}

func paymentExpectationPage(ctx context.Context, q *sqlc.Queries, result PaymentExpectations,
	rows []sqlc.ListOwnPaymentExpectationsRow, limit int32) (PaymentExpectations, error) {
	encodedBytes := 0
	for _, row := range rows {
		item, readErr := paymentExpectation(ctx, q, row, result.ObservedAt)
		if readErr != nil {
			return result, readErr
		}
		raw, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return result, marshalErr
		}
		if len(result.Expectations) == int(limit) || encodedBytes+len(raw) > paymentExpectationPageBytes {
			if len(result.Expectations) == 0 {
				return result, payments.ErrInvalid
			}
			result.HasMore = true
			last := result.Expectations[len(result.Expectations)-1].Balance.PassID
			result.Next = &last
			break
		}
		result.Expectations = append(result.Expectations, item)
		encodedBytes += len(raw)
	}
	return result, nil
}

func paymentExpectation(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ListOwnPaymentExpectationsRow,
	observed time.Time,
) (PaymentExpectation, error) {
	result := PaymentExpectation{
		EventID:   row.ShuttleEvent.ID,
		EventName: row.ShuttleEvent.Name,
		Timezone:  row.ShuttleEvent.Timezone,
	}
	entry, err := registrationRecord(ctx, q, row.ShuttleRegistration)
	if err != nil {
		return result, err
	}
	if entry.Pass == nil {
		return result, payments.ErrInvalid
	}
	result.Balance, err = paymentBalance(ctx, q, *entry.Pass)
	if err != nil {
		return result, err
	}
	location, err := time.LoadLocation(result.Timezone)
	if err != nil {
		return result, err
	}
	result.InitialDueAt = entry.Pass.PaymentDueAt.In(location)
	result.FinalDueAt = result.InitialDueAt
	result.Purpose = payments.Initial
	if entry.Pass.Deposit != nil {
		result.FinalDueAt = entry.Pass.Deposit.FinalDueAt.In(location)
		if result.Balance.DepositSatisfied {
			result.Purpose = payments.TopUp
		}
	}
	deadline, err := payments.Deadline(*entry.Pass, result.Purpose)
	if err != nil {
		return result, err
	}
	result.DueAt, result.Overdue = deadline.In(location), observed.After(deadline)
	result.PendingEvidence, err = q.PendingPassEvidence(ctx, entry.Pass.ID)
	result.AwaitingConfirmation = result.PendingEvidence > 0
	return result, err
}
