package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const maxMealDraftBytes = 60 << 10

type MealDraftChange struct {
	OperationID            uuid.UUID              `json:"operation_id"`
	EventID                uuid.UUID              `json:"event_id"`
	ExpectedEventVersion   int64                  `json:"expected_event_version"`
	ExpectedCatalogVersion int64                  `json:"expected_catalog_version"`
	ExpectedDraftVersion   int64                  `json:"expected_draft_version"`
	Choices                []orders.MealDayChoice `json:"choices"`
}

// MealDraft is unsubmitted state. Evaluation and timezone are historical, not a
// current price/sale guarantee; version0 has no saved evaluation.
type MealDraft struct {
	EventID        uuid.UUID              `json:"event_id"`
	EventVersion   int64                  `json:"event_version"`
	CatalogVersion int64                  `json:"catalog_version"`
	Version        int64                  `json:"version"`
	Timezone       string                 `json:"timezone"`
	Cleared        bool                   `json:"cleared"`
	Evaluation     *orders.MealEvaluation `json:"evaluation,omitempty"`
	SavedAt        *time.Time             `json:"saved_at,omitempty"`
}

// Hash normalization does not read the latest catalog. Receipt replay must work
// when that catalog has changed or no longer contains the original item IDs.
func normalizeMealDraftChange(a MealDraftChange) (MealDraftChange, []byte, error) {
	if a.OperationID == uuid.Nil || a.EventID == uuid.Nil || a.ExpectedEventVersion < 1 ||
		a.ExpectedCatalogVersion < 1 ||
		a.ExpectedDraftVersion < 0 ||
		a.ExpectedDraftVersion == math.MaxInt64 ||
		a.Choices == nil ||
		len(a.Choices) > orders.MaxMealDays {
		return a, nil, orders.ErrMealChoices
	}
	a.Choices = append([]orders.MealDayChoice{}, a.Choices...)
	for i, choice := range a.Choices {
		if choice.DayID == "" || len(choice.DayID) > 32 || len(choice.DinnerIDs) > orders.MaxItems {
			return a, nil, orders.ErrMealChoices
		}
		dinner, err := mealRequestIDs(choice.DinnerIDs)
		if err != nil {
			return a, nil, err
		}
		choice.DinnerIDs = dinner
		if choice.Lunch != nil {
			lunch := *choice.Lunch
			if len(lunch.SoupID) > 32 || len(lunch.MainID) > 32 || len(lunch.SideID) > 32 || len(lunch.SaladID) > 32 ||
				len(lunch.Mode) > 32 {
				return a, nil, orders.ErrMealChoices
			}
			ids, idsErr := mealRequestIDs(lunch.IndividualIDs)
			if idsErr != nil {
				return a, nil, idsErr
			}
			lunch.IndividualIDs = ids
			choice.Lunch = &lunch
		}
		a.Choices[i] = choice
	}
	slices.SortFunc(a.Choices, func(x, y orders.MealDayChoice) int { return strings.Compare(x.DayID, y.DayID) })
	raw, err := json.Marshal(a)
	if err != nil {
		return a, nil, err
	}
	if len(raw) > maxMealDraftBytes {
		return a, nil, orders.ErrMealChoices
	}
	hash := sha256.Sum256(raw)
	return a, hash[:], nil
}

func mealRequestIDs(ids []string) ([]string, error) {
	if len(ids) > orders.MaxItems {
		return nil, orders.ErrMealChoices
	}
	result := append([]string{}, ids...)
	for _, id := range result {
		if id == "" || len(id) > 32 {
			return nil, orders.ErrMealChoices
		}
	}
	slices.Sort(result)
	return result, nil
}

// authorizeMealDraft requires the event and owner locks. Meals are independent
// of the service-orders toggle; assigned unpaid passes retain access.
func authorizeMealDraft(ctx context.Context, tx pgx.Tx, scope ProfileScope, event uuid.UUID) error {
	allowed, err := sqlc.New(tx).
		UserModuleAvailable(ctx, sqlc.UserModuleAvailableParams{ID: event, BotID: scope.BotID, OrganizationID: scope.Organization, ID_2: scope.UserID, Column5: orderModuleMeals})
	if err != nil {
		return err
	}
	if !allowed {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) mealDraftTx(
	ctx context.Context,
	scope ProfileScope,
	event, operation uuid.UUID,
) (pgx.Tx, sqlc.ShuttleEvent, error) {
	tx, record, err := s.mealEventTx(ctx, scope, event, operation, "meal-draft")
	if err != nil {
		return nil, record, err
	}
	if err = authorizeMealDraft(ctx, tx, scope, event); err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, record, err
	}
	return tx, record, nil
}

func readMealDraft(ctx context.Context, q *sqlc.Queries, owner, event uuid.UUID) (MealDraft, error) {
	raw, err := q.ReadMealDraft(ctx, sqlc.ReadMealDraftParams{UserID: owner, EventID: event})
	if errors.Is(err, pgx.ErrNoRows) {
		return MealDraft{EventID: event}, nil
	}
	var result MealDraft
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}

func (s *Store) OwnMealDraft(ctx context.Context, scope ProfileScope, event uuid.UUID) (MealDraft, error) {
	tx, _, err := s.mealDraftTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return MealDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readMealDraft(ctx, s.q.WithTx(tx), scope.UserID, event)
}

func (s *Store) OwnMealDraftOperation(
	ctx context.Context,
	scope ProfileScope,
	event, operation uuid.UUID,
) (MealDraft, error) {
	tx, _, err := s.mealDraftTx(ctx, scope, event, uuid.Nil)
	if err != nil {
		return MealDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := s.q.WithTx(tx).
		ReadMealDraftOperation(ctx, sqlc.ReadMealDraftOperationParams{UserID: scope.UserID, OperationID: operation})
	if err != nil {
		return MealDraft{}, err
	}
	if row.EventID != event {
		return MealDraft{}, pgx.ErrNoRows
	}
	var result MealDraft
	err = json.Unmarshal(row.Result, &result)
	return result, err
}

// SaveOwnMealDraft commits one full draft and immutable receipt. It creates no
// order, capacity hold or money; uncertain commits retry this same operation.
func (s *Store) SaveOwnMealDraft(ctx context.Context, scope ProfileScope, change MealDraftChange) (MealDraft, error) {
	a, hash, err := normalizeMealDraftChange(change)
	if err != nil {
		return MealDraft{}, err
	}
	tx, event, err := s.mealDraftTx(ctx, scope, a.EventID, a.OperationID)
	if err != nil {
		return MealDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	result, replayed, err := s.saveMealDraft(ctx, tx, scope, event, a, hash)
	if err != nil || replayed {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// saveMealDraft uses only the caller's transaction and locks; native session and
// journal wrappers can add their checks without a second connection or commit.
func (s *Store) saveMealDraft(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	event sqlc.ShuttleEvent,
	a MealDraftChange,
	hash []byte,
) (MealDraft, bool, error) {
	q := s.q.WithTx(tx)
	prior, err := q.ReadMealDraftOperation(
		ctx,
		sqlc.ReadMealDraftOperationParams{UserID: scope.UserID, OperationID: a.OperationID},
	)
	if err == nil {
		if prior.EventID != a.EventID || !bytes.Equal(prior.RequestHash, hash) {
			return MealDraft{}, true, ErrConflict
		}
		var result MealDraft
		err = json.Unmarshal(prior.Result, &result)
		return result, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return MealDraft{}, false, err
	}
	catalog, err := readMealCatalog(ctx, q, a.EventID, 0)
	if err != nil {
		return MealDraft{}, false, err
	}
	if event.Version != a.ExpectedEventVersion || catalog.Version != a.ExpectedCatalogVersion {
		return MealDraft{}, false, ErrConflict
	}
	current, err := readMealDraft(ctx, q, scope.UserID, a.EventID)
	if err != nil {
		return MealDraft{}, false, err
	}
	if current.Version != a.ExpectedDraftVersion {
		return MealDraft{}, false, ErrConflict
	}
	evaluation, err := orders.EvaluateMeals(catalog.Document, a.Choices)
	if err != nil {
		return MealDraft{}, false, err
	}
	var source orders.MealCatalog
	if current.Version != 0 {
		old, oldErr := readMealCatalog(ctx, q, a.EventID, current.CatalogVersion)
		if oldErr != nil {
			return MealDraft{}, false, oldErr
		}
		source = old.Document
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return MealDraft{}, false, err
	}
	if err = checkMealDraftChanges(source, catalog.Document, current.Evaluation, evaluation, now.Time); err != nil {
		return MealDraft{}, false, err
	}
	saved := now.Time.UTC()
	result := MealDraft{
		EventID:        a.EventID,
		EventVersion:   event.Version,
		CatalogVersion: catalog.Version,
		Version:        current.Version + 1,
		Timezone:       event.Timezone,
		Cleared:        len(a.Choices) == 0,
		Evaluation:     &evaluation,
		SavedAt:        &saved,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return MealDraft{}, false, err
	}
	if len(raw) > maxMealDraftBytes {
		return MealDraft{}, false, orders.ErrMealChoices
	}
	if err = q.SaveMealDraft(
		ctx,
		sqlc.SaveMealDraftParams{
			UserID:         scope.UserID,
			EventID:        a.EventID,
			CatalogVersion: catalog.Version,
			Version:        result.Version,
			Result:         raw,
		},
	); err != nil {
		return MealDraft{}, false, err
	}
	err = q.InsertMealDraftOperation(
		ctx,
		sqlc.InsertMealDraftOperationParams{
			UserID:         scope.UserID,
			OperationID:    a.OperationID,
			EventID:        a.EventID,
			CatalogVersion: catalog.Version,
			RequestHash:    hash,
			Result:         raw,
		},
	)
	return result, false, err
}
