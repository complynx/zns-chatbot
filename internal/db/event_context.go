package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
)

const contextExplicit = "explicit"

type EventContextScope struct {
	EventScope

	UserID  uuid.UUID
	Subject string
}
type EventContextChoice struct {
	Module          string    `json:"module"           minLength:"1" maxLength:"128"`
	EventID         uuid.UUID `json:"event_id"`
	ExpectedVersion int64     `json:"expected_version"                               minimum:"0"`
	RunID           string    `json:"run_id"`
}

func (s EventContextScope) valid() bool {
	return s.BotID > 0 && s.Organization != "" && s.UserID != uuid.Nil && s.Subject != ""
}
func contextUser(ctx context.Context, q *sqlc.Queries, scope EventContextScope) error {
	user, err := q.GetUser(ctx, scope.UserID)
	if err != nil {
		return err
	}
	if user.BotID != scope.BotID || user.ZitadelOrg != scope.Organization || user.ZitadelSubject != scope.Subject ||
		user.Revoked {
		return pgx.ErrNoRows
	}
	return nil
}

func contextCandidates(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventContextScope,
	module string,
	id uuid.UUID,
	now pgtype.Timestamptz,
) ([]events.Event, error) {
	rows, err := q.ListContextEvents(
		ctx,
		sqlc.ListContextEventsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			UserID:         scope.UserID,
			ModuleID:       module,
			EventID:        id,
			AtTime:         now,
			PageLimit:      events.ContextChoiceLimit + 1,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]events.Event, 0, len(rows))
	for _, row := range rows {
		event, e := eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
		if e != nil {
			return nil, e
		}
		result = append(result, event)
	}
	return result, nil
}

// ResolveEventContext reads one database snapshot; it never changes a selection or grants access.
func (s *Store) ResolveEventContext(
	ctx context.Context,
	scope EventContextScope,
	request events.ContextRequest,
	runID string,
) (events.Context, error) {
	if !scope.valid() || !request.Valid() || len(runID) > events.MaxContextRunIDBytes {
		return events.Context{}, events.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return events.Context{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = contextUser(ctx, q, scope); err != nil {
		return events.Context{}, err
	}
	runPin, userPin, version, err := contextPins(ctx, q, scope.UserID, runID)
	if err != nil {
		return events.Context{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return events.Context{}, err
	}
	// Check explicit and saved targets before loading the bounded suggestion list.
	for _, choice := range []struct {
		id     *uuid.UUID
		source string
	}{{request.EventID, contextExplicit}, {&runPin, "pinned"}, {&userPin, "pinned"}, {request.HintID, "hint"}} {
		id := choice.id
		if id == nil || *id == uuid.Nil || (choice.source == "hint" && request.RequireExplicit) {
			continue
		}
		target, e := contextCandidates(ctx, q, scope, request.Module, *id, now)
		if e != nil {
			return events.Context{}, e
		}
		if len(target) == 0 {
			if choice.source == contextExplicit {
				return events.Context{}, events.ErrContextUnavailable
			}
			continue
		}
		view := events.ContextView(target[0])
		return events.Context{Source: choice.source, Selected: &view,
			Choices: []events.ContextEvent{}, SelectionVersion: version}, nil
	}
	candidates, err := contextCandidates(ctx, q, scope, request.Module, uuid.Nil, now)
	if err != nil {
		return events.Context{}, err
	}
	result, err := events.ResolveContext(request, uuid.Nil, candidates, now.Time)
	result.SelectionVersion = version
	return result, err
}

func contextPins(
	ctx context.Context,
	q *sqlc.Queries,
	userID uuid.UUID,
	runID string,
) (uuid.UUID, uuid.UUID, int64, error) {
	choice, err := q.GetUserEventContext(ctx, userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, 0, err
	}
	var pinned, runPin uuid.UUID
	if choice.EventID.Valid {
		pinned = uuid.UUID(choice.EventID.Bytes)
	}
	if runID != "" {
		runEvent, e := q.GetRunEventContext(ctx, sqlc.GetRunEventContextParams{UserID: userID, RunID: runID})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, 0, e
		}
		if e == nil {
			runPin = runEvent
		}
	}
	return runPin, pinned, choice.Version, nil
}

// ChooseEventContext atomically saves the user's pin, run pin and original operation receipt.
// Run IDs are caller metadata scoped to the authenticated user, never authority for another user.
func (s *Store) ChooseEventContext(
	ctx context.Context,
	scope EventContextScope,
	operation string,
	choice EventContextChoice,
) (events.Context, error) {
	if !scope.valid() || operation == "" || len(operation) > 128 || choice.EventID == uuid.Nil ||
		choice.ExpectedVersion < 0 || len(choice.RunID) > events.MaxContextRunIDBytes ||
		!(events.ContextRequest{Module: choice.Module}).Valid() {
		return events.Context{}, events.ErrInvalid
	}
	raw, err := json.Marshal(choice)
	if err != nil {
		return events.Context{}, err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return events.Context{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockEventOperation(ctx, fmt.Sprintf("event-context:%s:%s", scope.UserID, operation)); err != nil {
		return events.Context{}, err
	}
	if err = contextUser(ctx, q, scope); err != nil {
		return events.Context{}, err
	}
	prior, err := q.GetEventContextOperation(
		ctx,
		sqlc.GetEventContextOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err == nil {
		if !bytes.Equal(prior.RequestHash, hash[:]) {
			return events.Context{}, ErrConflict
		}
		var result events.Context
		err = json.Unmarshal(prior.Result, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return events.Context{}, err
	}
	result, err := applyContextChoice(ctx, q, scope, choice)
	if err != nil {
		return events.Context{}, err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return events.Context{}, err
	}
	if err = q.InsertEventContextOperation(
		ctx,
		sqlc.InsertEventContextOperationParams{
			UserID:      scope.UserID,
			OperationID: operation,
			RequestHash: hash[:],
			Result:      payload,
		},
	); err != nil {
		return events.Context{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return events.Context{}, err
	}
	return result, nil
}

func applyContextChoice(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventContextScope,
	choice EventContextChoice,
) (events.Context, error) {
	if _, err := q.LockContextEvent(
		ctx,
		sqlc.LockContextEventParams{ID: choice.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return events.Context{}, err
	}
	user, err := q.LockRegistrationUser(
		ctx,
		sqlc.LockRegistrationUserParams{ID: scope.UserID, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	if err != nil {
		return events.Context{}, err
	}
	if user.ZitadelSubject != scope.Subject {
		return events.Context{}, pgx.ErrNoRows
	}
	if err = q.EnsureUserEventContext(ctx, scope.UserID); err != nil {
		return events.Context{}, err
	}
	current, err := q.LockUserEventContext(ctx, scope.UserID)
	if err != nil {
		return events.Context{}, err
	}
	if current.Version != choice.ExpectedVersion {
		return events.Context{}, ErrConflict
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return events.Context{}, err
	}
	candidates, err := contextCandidates(ctx, q, scope, choice.Module, choice.EventID, now)
	if err != nil {
		return events.Context{}, err
	}
	if len(candidates) != 1 {
		return events.Context{}, events.ErrContextUnavailable
	}
	updated, err := q.UpdateUserEventContext(
		ctx,
		sqlc.UpdateUserEventContextParams{
			UserID:  scope.UserID,
			EventID: pgtype.UUID{Bytes: choice.EventID, Valid: true},
			Version: choice.ExpectedVersion,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Context{}, ErrConflict
	}
	if err != nil {
		return events.Context{}, err
	}
	if choice.RunID != "" {
		if err = q.SetRunEventContext(
			ctx,
			sqlc.SetRunEventContextParams{UserID: scope.UserID, RunID: choice.RunID, EventID: choice.EventID},
		); err != nil {
			return events.Context{}, err
		}
	}
	view := events.ContextView(candidates[0])
	return events.Context{
		Source:           contextExplicit,
		Selected:         &view,
		Choices:          []events.ContextEvent{},
		SelectionVersion: updated.Version,
	}, nil
}

func (s *Store) EventContextOperation(
	ctx context.Context,
	scope EventContextScope,
	operation string,
) (events.Context, error) {
	if err := contextUser(ctx, s.q, scope); err != nil {
		return events.Context{}, err
	}
	prior, err := s.q.GetEventContextOperation(
		ctx,
		sqlc.GetEventContextOperationParams{UserID: scope.UserID, OperationID: operation},
	)
	if err != nil {
		return events.Context{}, err
	}
	var result events.Context
	err = json.Unmarshal(prior.Result, &result)
	return result, err
}

func (s *Store) ListEventContexts(
	ctx context.Context,
	scope EventContextScope,
	module string,
	after uuid.UUID,
	limit int32,
) (events.ContextList, error) {
	if !scope.valid() || !(events.ContextRequest{Module: module}).Valid() || limit < 1 ||
		limit > events.ContextChoiceLimit {
		return events.ContextList{}, events.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return events.ContextList{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = contextUser(ctx, q, scope); err != nil {
		return events.ContextList{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return events.ContextList{}, err
	}
	rows, err := q.ListContextEvents(
		ctx,
		sqlc.ListContextEventsParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			UserID:         scope.UserID,
			ModuleID:       module,
			AtTime:         now,
			AfterID:        after,
			ListOrder:      true,
			PageLimit:      limit + 1,
		},
	)
	if err != nil {
		return events.ContextList{}, err
	}
	result := events.ContextList{Events: []events.ContextEvent{}}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	for _, row := range rows {
		event, e := eventRecord(row.ShuttleEvent, row.RegistrationRules, row.DepositPolicy)
		if e != nil {
			return events.ContextList{}, e
		}
		result.Events = append(result.Events, events.ContextView(event))
	}
	if more {
		result.Next = &result.Events[len(result.Events)-1].ID
	}
	return result, nil
}
