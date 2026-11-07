package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

type MealCatalog struct {
	EventID  uuid.UUID          `json:"event_id"`
	Version  int64              `json:"version"`
	Document orders.MealCatalog `json:"document"`
}

type MealCatalogChange struct {
	EventID              uuid.UUID          `json:"event_id"`
	ExpectedEventVersion int64              `json:"expected_event_version"`
	ExpectedVersion      int64              `json:"expected_version"`
	Document             orders.MealCatalog `json:"document"`
}

type MealCatalogPreparation struct {
	ID        uuid.UUID         `json:"proposal_id"`
	Arguments MealCatalogChange `json:"arguments"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type MealCatalogView struct {
	MealCatalog

	EventVersion int64     `json:"event_version"`
	EventName    string    `json:"event_name"`
	Timezone     string    `json:"timezone"`
	ObservedAt   time.Time `json:"observed_at"`
}

func readMealCatalog(ctx context.Context, q *sqlc.Queries, event uuid.UUID, version int64) (MealCatalog, error) {
	var row sqlc.ShuttleMealCatalog
	var err error
	if version == 0 {
		row, err = q.ReadMealCatalog(ctx, event)
	} else {
		row, err = q.ReadMealCatalogVersion(ctx, sqlc.ReadMealCatalogVersionParams{EventID: event, Version: version})
	}
	if err != nil {
		return MealCatalog{}, err
	}
	result := MealCatalog{EventID: row.EventID, Version: row.Version}
	if err = json.Unmarshal(row.Document, &result.Document); err != nil {
		return MealCatalog{}, err
	}
	result.Document, err = orders.NormalizeMeals(result.Document)
	return result, err
}

func normalizeMealCatalogChange(a MealCatalogChange) (MealCatalogChange, []byte, error) {
	if a.EventID == uuid.Nil || a.ExpectedEventVersion < 1 || a.ExpectedVersion < 0 ||
		a.ExpectedVersion == math.MaxInt64 {
		return a, nil, orders.ErrMealCatalog
	}
	document, err := orders.NormalizeMeals(a.Document)
	if err != nil {
		return a, nil, err
	}
	a.Document = document
	raw, err := json.Marshal(a)
	hash := sha256.Sum256(raw)
	return a, hash[:], err
}

// mealEventTx serializes event changes before the current actor. Only an
// operation namespace is shared; catalogs and drafts have separate receipts.
func (s *Store) mealEventTx(
	ctx context.Context,
	scope ProfileScope,
	event, operation uuid.UUID,
	kind string,
) (pgx.Tx, sqlc.ShuttleEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, sqlc.ShuttleEvent{}, err
	}
	q := s.q.WithTx(tx)
	if operation != uuid.Nil {
		err = q.LockEventOperation(ctx, fmt.Sprintf("%s:%s:%s", kind, scope.UserID, operation))
	}
	var record sqlc.ShuttleEvent
	if err == nil {
		record, err = q.LockRegistrationEvent(
			ctx,
			sqlc.LockRegistrationEventParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization},
		)
	}
	if err == nil {
		err = lockProfileOwner(ctx, tx, scope, false)
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, sqlc.ShuttleEvent{}, err
	}
	return tx, record, nil
}

// MealCatalogConfiguration is an admin configuration read, not owner access.
// Its API caller must check current global administrator authority.
func (s *Store) MealCatalogConfiguration(
	ctx context.Context,
	scope ProfileScope,
	event uuid.UUID,
	version int64,
) (MealCatalog, error) {
	if version < 0 {
		return MealCatalog{}, orders.ErrMealCatalog
	}
	tx, _, err := s.mealEventTx(ctx, scope, event, uuid.Nil, "meal-catalog")
	if err != nil {
		return MealCatalog{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readMealCatalog(ctx, s.q.WithTx(tx), event, version)
}

// OwnMealCatalog reads latest (version0) or retained historical terms with current
// meals entitlement. It never substitutes the latest catalog for an old version.
func (s *Store) OwnMealCatalog(
	ctx context.Context,
	scope ProfileScope,
	event uuid.UUID,
	version int64,
) (MealCatalogView, error) {
	if version < 0 {
		return MealCatalogView{}, orders.ErrMealCatalog
	}
	tx, record, err := s.mealDraftTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return MealCatalogView{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	catalog, err := readMealCatalog(ctx, s.q.WithTx(tx), event, version)
	if err != nil {
		return MealCatalogView{}, err
	}
	now, err := s.q.WithTx(tx).RegistrationNow(ctx)
	if err != nil {
		return MealCatalogView{}, err
	}
	zone, err := time.LoadLocation(record.Timezone)
	if err != nil {
		return MealCatalogView{}, err
	}
	for i := range catalog.Document.Days {
		day := &catalog.Document.Days[i]
		if day.Lunch != nil {
			day.Lunch.Service = mealServiceInZone(day.Lunch.Service, zone)
		}
		if day.Dinner != nil {
			service := mealServiceInZone(*day.Dinner, zone)
			day.Dinner = &service
		}
	}
	return MealCatalogView{
		MealCatalog:  catalog,
		EventVersion: record.Version,
		EventName:    record.Name,
		Timezone:     record.Timezone,
		ObservedAt:   now.Time.UTC(),
	}, nil
}

func mealServiceInZone(service orders.MealService, zone *time.Location) orders.MealService {
	service.StartsAt, service.EndsAt = service.StartsAt.In(zone), service.EndsAt.In(zone)
	service.SalesOpensAt, service.SalesClosesAt = service.SalesOpensAt.In(zone), service.SalesClosesAt.In(zone)
	return service
}
