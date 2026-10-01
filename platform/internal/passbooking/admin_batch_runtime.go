package passbooking

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const commandBatchUncouple = "admin_uncouple"

// RuntimeBatch carries explicit Telegram recipients. The service grounds identity,
// versions and operation keys once, before executing any recipient.
type RuntimeBatch struct {
	Key        string          `json:"key"`
	Event      string          `json:"event"`
	Action     string          `json:"action"`
	Recipients []int64         `json:"recipients"`
	Options    AdminAssignment `json:"options"`
}

type RuntimeBatchItem struct {
	TelegramID int64             `json:"telegram_id"`
	Assignment AdminAssignment   `json:"assignment"`
	Outcome    AdminBatchOutcome `json:"outcome"`
}

type runtimeBatchPlan struct {
	Action string             `json:"action"`
	Event  string             `json:"event"`
	Items  []RuntimeBatchItem `json:"items"`
}

func authorizeBatchCancel(ctx context.Context, tx pgx.Tx, actor, event string) error {
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).Scan(&owner)
	if !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationContextError(ctx, err)
	}
	err = tx.QueryRow(ctx, `SELECT owner FROM core.pass_payment_admins WHERE owner=$1 AND event_id=$2 FOR SHARE`, actor, event).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	return core.DatabaseOperationContextError(ctx, err)
}

func validateRuntimeBatch(c RuntimeBatch) error {
	const maxBatchKey = 200
	if !boundedText(c.Key, maxBatchKey, false) || !boundedText(c.Event, maxBatchKey, false) || len(c.Recipients) == 0 ||
		len(c.Recipients) > MaxAdminBatchRecipients {
		return invalid()
	}
	if c.Action != commandAdminAssign && c.Action != commandAdminCancel && c.Action != commandBatchUncouple {
		return invalid()
	}
	if c.Action == commandBatchUncouple && len(c.Recipients) != 1 {
		return invalid()
	}
	seen := map[int64]bool{}
	for _, id := range c.Recipients {
		if id <= 0 || seen[id] {
			return invalid()
		}
		seen[id] = true
	}
	option := c.Options
	if option.Event != "" || option.Key != "" || option.Target != "" || option.Version != 0 ||
		option.TargetVersion != 0 ||
		(option.Create != nil && option.Create.ProfileVersion != 0) {
		return invalid()
	}
	option.Event, option.Key, option.Target = c.Event, c.Key, "target"
	if err := validateAdminAssignment(option); err != nil {
		return err
	}
	if c.Action != commandAdminAssign &&
		(option.TotalPrice != nil || option.Kind != nil || option.Comment != nil || option.SkipBalance != nil || option.AppendTier != nil || option.Create != nil) {
		return invalid()
	}
	return nil
}

// RunBatch persists immutable grounded input and terminal outcomes. An interrupted
// item replays its exact domain key; each recipient remains its own transaction.
func (s Service) RunBatch(ctx context.Context, actor string, c RuntimeBatch) ([]RuntimeBatchItem, error) {
	if err := validateRuntimeBatch(c); err != nil {
		return nil, err
	}
	if err := s.prepareRuntimeBatch(ctx, actor, c); err != nil {
		return nil, err
	}
	var result []RuntimeBatchItem
	for index := range c.Recipients {
		items, err := s.runRuntimeBatchItem(ctx, actor, c, index)
		if err != nil {
			return result, err
		}
		result = items
	}
	return result, nil
}

func (s Service) prepareRuntimeBatch(ctx context.Context, actor string, c RuntimeBatch) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	batch, found, err := s.PrepareRuntimeBatchInTx(ctx, tx, actor, c)
	if err != nil {
		return err
	}
	if len(batch.Source) > 0 {
		return conflict("derived_batch_requires_coordinator")
	}
	if !found {
		if err = batch.Persist(ctx, tx, c, nil); err != nil {
			return err
		}
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
func groundRuntimeBatch(ctx context.Context, tx pgx.Tx, actor string, c RuntimeBatch) (runtimeBatchPlan, error) {
	plan := runtimeBatchPlan{Action: c.Action, Event: c.Event, Items: make([]RuntimeBatchItem, len(c.Recipients))}
	records, err := readBookings(ctx, tx, c.Event)
	if err != nil {
		return plan, err
	}
	for i, id := range c.Recipients {
		command := c.Options
		command.Event, command.Key, command.Version = c.Event, "batch-"+hash(
			[]byte(c.Key),
		)+fmt.Sprintf(
			"-%d",
			i,
		), bookingVersion(
			records[actor],
		)
		var profileVersion int64
		err = tx.QueryRow(ctx, `SELECT u.id,COALESCE(p.version,0) FROM core.users u LEFT JOIN core.pass_profiles p ON p.owner=u.id WHERE u.telegram_id=$1`, id).
			Scan(&command.Target, &profileVersion)
		outcome := AdminBatchOutcome{Key: command.Key, Status: AdminBatchNotAttempted}
		if errors.Is(err, pgx.ErrNoRows) {
			outcome.Status, outcome.Code = AdminBatchRejected, "pass_recipient_unknown"
		} else if err != nil {
			return plan, core.DatabaseOperationError(err)
		}
		command.TargetVersion = bookingVersion(records[command.Target])
		outcome.Target = command.Target
		if command.Create != nil {
			create := *command.Create
			create.ProfileVersion = profileVersion
			command.Create = &create
		}
		plan.Items[i] = RuntimeBatchItem{TelegramID: id, Assignment: command, Outcome: outcome}
	}
	return plan, nil
}

func (s Service) runRuntimeBatchItem(
	ctx context.Context,
	actor string,
	c RuntimeBatch,
	index int,
) ([]RuntimeBatchItem, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	var plan runtimeBatchPlan
	if _, err = readEvent(ctx, tx, c.Event); err != nil {
		return nil, err
	}
	var source []byte
	err = tx.QueryRow(ctx, `SELECT plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2 FOR UPDATE`, actor, hash([]byte(c.Key))).
		Scan(&plan, &source)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	if len(source) > 0 {
		return nil, conflict("derived_batch_requires_coordinator")
	}
	committedItems := slices.Clone(plan.Items)
	item := &plan.Items[index]
	// Mutations check authorization themselves. A terminal replay also checks it.
	if item.Outcome.Status != AdminBatchNotAttempted {
		if _, err = authorize(ctx, tx, actor, plan.Action, plan.Event); err != nil {
			return nil, err
		}
	}
	if item.Outcome.Status == AdminBatchNotAttempted {
		err = s.executeRuntimeBatchItem(ctx, tx, actor, plan.Action, item)
		if stop := adminBatchResult(&item.Outcome, err); stop != nil {
			return plan.Items, stop
		}
		_, err = tx.Exec(
			ctx,
			`UPDATE core.pass_admin_batches SET plan=$3 WHERE actor=$1 AND key_hash=$2`,
			actor,
			hash([]byte(c.Key)),
			plan,
		)
		if err != nil {
			return plan.Items, core.DatabaseOperationError(err)
		}
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return committedItems, err
	}
	return plan.Items, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) executeRuntimeBatchItem(
	ctx context.Context,
	outer pgx.Tx,
	actor, action string,
	item *RuntimeBatchItem,
) error {
	// A domain rejection may occur after writes. Roll back its savepoint before
	// persisting the terminal rejection, while retaining the batch row lock.
	tx, err := outer.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.mutateRuntimeBatchItem(ctx, tx, actor, action, item); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) mutateRuntimeBatchItem(
	ctx context.Context,
	tx pgx.Tx,
	actor, action string,
	item *RuntimeBatchItem,
) error {
	if action == commandAdminAssign {
		assigned, err := s.adminAssignInTx(ctx, tx, actor, item.Assignment)
		if err == nil {
			item.Outcome.Assignment = &assigned
		}
		return err
	}
	a := item.Assignment
	_, err := s.executeInTx(
		ctx,
		tx,
		actor,
		Command{
			Name:          action,
			Event:         a.Event,
			Key:           a.Key,
			Version:       a.Version,
			Target:        a.Target,
			TargetVersion: a.TargetVersion,
		},
	)
	return err
}
