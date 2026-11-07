package db

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const (
	accountDefaultLimit = 5
	accountMaxLimit     = 20
	accountPageBytes    = 32 << 10
)

type AccountRegistration struct {
	ID          uuid.UUID `json:"id"`
	Version     int64     `json:"version"`
	Role        string    `json:"role"`
	State       string    `json:"state"`
	RequestedAt time.Time `json:"requested_at"`
}

type AccountPass struct {
	Balance         payments.Balance `json:"balance"`
	AssignedAt      time.Time        `json:"assigned_at"`
	InitialDueAt    time.Time        `json:"initial_due_at"`
	FinalDueAt      time.Time        `json:"final_due_at"`
	DueAt           time.Time        `json:"due_at"`
	Purpose         string           `json:"purpose"`
	Overdue         bool             `json:"overdue"`
	PendingEvidence int64            `json:"pending_evidence"`
}

type AccountEvent struct {
	EventID             uuid.UUID           `json:"event_id"`
	EventName           string              `json:"event_name"`
	Timezone            string              `json:"timezone"`
	StartsAt            time.Time           `json:"starts_at"`
	EndsAt              time.Time           `json:"ends_at"`
	Registration        AccountRegistration `json:"registration"`
	RegistrationEnabled bool                `json:"registration_enabled"`
	Pass                *AccountPass        `json:"pass,omitempty"`
}

type AccountEvents struct {
	ObservedAt time.Time      `json:"observed_at"`
	Events     []AccountEvent `json:"events"`
	HasMore    bool           `json:"has_more"`
	Next       *uuid.UUID     `json:"next,omitempty"`
}

// OwnAccountEvents includes closed/ended and paid registrations, never authority
// discovery. All helpers use this snapshot's connection, including with a one-connection pool.
func (s *Store) OwnAccountEvents(
	ctx context.Context,
	scope ProfileScope,
	after uuid.UUID,
	limit int32,
) (AccountEvents, error) {
	result := AccountEvents{Events: []AccountEvent{}}
	if limit == 0 {
		limit = accountDefaultLimit
	}
	if limit < 1 || limit > accountMaxLimit {
		return result, registrations.ErrInvalid
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
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&result.ObservedAt); err != nil {
		return result, err
	}
	result.ObservedAt = result.ObservedAt.UTC()
	rows, err := q.ListOwnAccountEvents(ctx, sqlc.ListOwnAccountEventsParams{
		UserID:         scope.UserID,
		Subject:        scope.Subject,
		BotID:          scope.BotID,
		OrganizationID: scope.Organization,
		AfterID:        after,
		PageLimit:      limit + 1,
	})
	if err != nil {
		return result, err
	}
	encodedBytes := 0
	for _, row := range rows {
		if len(result.Events) == int(limit) {
			result.HasMore = true
			break
		}
		item, readErr := accountEvent(ctx, q, row, result.ObservedAt)
		if readErr != nil {
			return result, readErr
		}
		raw, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return result, marshalErr
		}
		if encodedBytes+len(raw) > accountPageBytes {
			if len(result.Events) == 0 {
				return result, registrations.ErrInvalid
			}
			result.HasMore = true
			break
		}
		result.Events = append(result.Events, item)
		encodedBytes += len(raw)
	}
	if result.HasMore {
		last := result.Events[len(result.Events)-1].EventID
		result.Next = &last
	}
	return result, nil
}

func accountEvent(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ListOwnAccountEventsRow,
	observed time.Time,
) (AccountEvent, error) {
	event, registration := row.ShuttleEvent, row.ShuttleRegistration
	location, err := time.LoadLocation(event.Timezone)
	if err != nil {
		return AccountEvent{}, err
	}
	result := AccountEvent{EventID: event.ID, EventName: event.Name, Timezone: event.Timezone,
		StartsAt: event.StartsAt.Time.In(location), EndsAt: event.EndsAt.Time.In(location),
		Registration: AccountRegistration{
			ID:          registration.ID,
			Version:     registration.Version,
			Role:        registration.Role,
			State:       registration.State,
			RequestedAt: registration.RequestedAt.Time.In(location),
		},
		RegistrationEnabled: slices.Contains(event.Modules, "registrations")}
	if !result.RegistrationEnabled || registration.State != registrations.Assigned {
		return result, nil
	}
	entry, err := registrationRecord(ctx, q, registration)
	if err != nil {
		return result, err
	}
	if entry.Pass == nil {
		return result, registrations.ErrInvalid
	}
	result.Pass, err = accountPass(ctx, q, *entry.Pass, location, observed)
	return result, err
}

func accountPass(
	ctx context.Context,
	q *sqlc.Queries,
	pass registrations.Pass,
	location *time.Location,
	observed time.Time,
) (*AccountPass, error) {
	balance, err := paymentBalance(ctx, q, pass)
	if err != nil {
		return nil, err
	}
	result := &AccountPass{
		Balance:    balance,
		AssignedAt: pass.AssignedAt.In(location),
		InitialDueAt: pass.PaymentDueAt.In(
			location,
		),
		FinalDueAt: pass.PaymentDueAt.In(location),
		Purpose:    payments.Initial,
	}
	if pass.Deposit != nil {
		result.FinalDueAt = pass.Deposit.FinalDueAt.In(location)
		if balance.DepositSatisfied {
			result.Purpose = payments.TopUp
		}
	}
	deadline, err := payments.Deadline(pass, result.Purpose)
	if err != nil {
		return nil, err
	}
	result.DueAt = deadline.In(location)
	result.Overdue = !balance.FullyPaid && observed.After(deadline)
	result.PendingEvidence, err = q.PendingPassEvidence(ctx, pass.ID)
	return result, err
}
