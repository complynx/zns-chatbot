package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

const detailsRequestPageSize = 20

type DetailsReadiness struct {
	EventID        uuid.UUID                   `json:"event_id"`
	EventVersion   int64                       `json:"event_version"`
	EventTimezone  string                      `json:"event_timezone"`
	SalesClosesAt  time.Time                   `json:"sales_closes_at"`
	DetailsVersion int64                       `json:"details_version"`
	MissingFields  []registrationdetails.Field `json:"missing_fields"`
	Ready          bool                        `json:"ready"`
	ObservedAt     time.Time                   `json:"observed_at"`
}

// DetailsReadiness is only a current fields snapshot, never registration consent.
func (s *Store) DetailsReadiness(ctx context.Context, scope ProfileScope, eventID uuid.UUID) (DetailsReadiness, error) {
	if eventID == uuid.Nil {
		return DetailsReadiness{}, registrationdetails.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DetailsReadiness{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	event, err := registrationEventForChange(
		ctx,
		q,
		EventScope{BotID: scope.BotID, Organization: scope.Organization},
		eventID,
	)
	if err != nil {
		return DetailsReadiness{}, err
	}
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return DetailsReadiness{}, err
	}
	status, err := registrationDetailsStatus(ctx, q, scope.UserID)
	if err != nil {
		return DetailsReadiness{}, err
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return DetailsReadiness{}, err
	}
	required := registrationdetails.Requirements{}
	if event.Document.Registration.DetailsRequirements != nil {
		required = event.Document.Registration.DetailsRequirements.Normalize()
	}
	missing := required.Missing(status)
	return DetailsReadiness{
		EventID:        event.ID,
		EventVersion:   event.Version,
		EventTimezone:  event.Document.Timezone,
		SalesClosesAt:  event.Document.Registration.ClosesAt,
		DetailsVersion: status.Version,
		MissingFields:  missing,
		Ready:          len(missing) == 0,
		ObservedAt:     now.Time,
	}, nil
}

func (s *Store) ListDetailsRequests(
	ctx context.Context,
	scope ProfileScope,
	after uuid.UUID,
	limit int32,
) ([]DetailsRequestStatus, error) {
	if limit < 1 || limit > detailsRequestPageSize {
		return nil, registrationdetails.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	rows, err := q.ListRegistrationDetailsRequests(
		ctx,
		sqlc.ListRegistrationDetailsRequestsParams{UserID: scope.UserID, ID: after, Limit: limit},
	)
	if err != nil {
		return nil, err
	}
	return detailsRequestSnapshots(ctx, q, scope.UserID, rows)
}

// RecentDetailsRequests excludes requests created after this user-triggered task.
func (s *Store) RecentDetailsRequests(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]DetailsRequestStatus, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	q := s.q.WithTx(tx)
	rows, err := q.RecentRegistrationDetailsRequests(
		ctx,
		sqlc.RecentRegistrationDetailsRequestsParams{ID: taskID, UserID: scope.UserID},
	)
	if err != nil {
		return nil, err
	}
	return detailsRequestSnapshots(ctx, q, scope.UserID, rows)
}

func detailsRequestSnapshots(
	ctx context.Context,
	q *sqlc.Queries,
	owner uuid.UUID,
	rows []sqlc.ShuttleRegistrationDetailsRequest,
) ([]DetailsRequestStatus, error) {
	status, err := registrationDetailsStatus(ctx, q, owner)
	if err != nil {
		return nil, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]DetailsRequestStatus, 0, len(rows))
	for _, row := range rows {
		snapshots = append(snapshots, detailsRequestStatus(row, status, now.Time))
	}
	return snapshots, nil
}
