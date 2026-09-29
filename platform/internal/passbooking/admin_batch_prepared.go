package passbooking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// RuntimeBatchState contains the immutable grounded input and its committed
// outcomes. Source is host metadata, never part of the public batch request.
type RuntimeBatchState struct {
	Source json.RawMessage
	plan   runtimeBatchPlan
	actor  string
	key    string
}

func (b *RuntimeBatchState) Items() []RuntimeBatchItem { return b.plan.Items }

func (b *RuntimeBatchState) Pending(index int) bool {
	return b.plan.Items[index].Outcome.Status == AdminBatchNotAttempted
}

func (b *RuntimeBatchState) Actors() []string {
	actors := []string{b.actor}
	for _, item := range b.plan.Items {
		if item.Assignment.Target != "" {
			actors = append(actors, item.Assignment.Target)
		}
	}
	return actors
}

// ReadRuntimeBatch reads the lock prelude inputs. LockRuntimeBatchInTx checks
// the immutable source again after taking the row lock. A missing batch returns
// pgx.ErrNoRows.
func (s Service) ReadRuntimeBatch(ctx context.Context, actor string, c RuntimeBatch) (*RuntimeBatchState, error) {
	if err := validateRuntimeBatch(c); err != nil {
		return nil, err
	}
	b := &RuntimeBatchState{actor: actor, key: hash([]byte(c.Key))}
	var request string
	err := s.DB.QueryRow(ctx, `SELECT request_hash,plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2`, actor, b.key).
		Scan(&request, &b.plan, &b.Source)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if request != hash(raw) {
		return nil, conflict("idempotency_conflict")
	}
	return b, nil
}

// PrepareRuntimeBatchInTx authorizes the target and returns an existing batch
// before any caller source check. The caller holds the event/actor lock union.
func (s Service) PrepareRuntimeBatchInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c RuntimeBatch,
) (*RuntimeBatchState, bool, error) {
	if err := validateRuntimeBatch(c); err != nil {
		return nil, false, err
	}
	if _, err := readEvent(ctx, tx, c.Event); err != nil {
		return nil, false, err
	}
	if _, err := authorize(ctx, tx, actor, c.Action, c.Event); err != nil {
		return nil, false, err
	}
	b := &RuntimeBatchState{actor: actor, key: hash([]byte(c.Key))}
	var previous string
	err := tx.QueryRow(ctx, `SELECT request_hash,plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2`, actor, b.key).
		Scan(&previous, &b.plan, &b.Source)
	raw, encodeErr := json.Marshal(c)
	if encodeErr != nil {
		return nil, false, encodeErr
	}
	if err == nil {
		if previous != hash(raw) {
			return nil, false, conflict("idempotency_conflict")
		}
		return b, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	b.plan, err = groundRuntimeBatch(ctx, tx, actor, c)
	return b, false, err
}

func (b *RuntimeBatchState) Persist(ctx context.Context, tx pgx.Tx, c RuntimeBatch, source json.RawMessage) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_admin_batches(actor,key_hash,request_hash,plan,source_derivation) VALUES($1,$2,$3,$4,$5)`,
		b.actor,
		b.key,
		hash(raw),
		b.plan,
		source,
	)
	return err
}

func (b *RuntimeBatchState) LockRuntimeBatchInTx(ctx context.Context, tx pgx.Tx) error {
	var source json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2 FOR UPDATE`, b.actor, b.key).
		Scan(&b.plan, &source); err != nil {
		return err
	}
	if !bytes.Equal(source, b.Source) {
		return conflict("idempotency_conflict")
	}
	_, err := authorize(ctx, tx, b.actor, b.plan.Action, b.plan.Event)
	return err
}

// PreparedRuntimeBatchItem keeps the domain receipt ahead of the source fence.
// The enclosing transaction owns the batch row; effects execute in a savepoint.
type PreparedRuntimeBatchItem struct {
	batch      *RuntimeBatchState
	index      int
	assignment *PreparedAssignment
	command    *PreparedCommand
}

func (s Service) PrepareRuntimeBatchItem(
	ctx context.Context,
	tx pgx.Tx,
	b *RuntimeBatchState,
	index int,
) (*PreparedRuntimeBatchItem, error) {
	if index < 0 || index >= len(b.plan.Items) {
		return nil, invalid()
	}
	p := &PreparedRuntimeBatchItem{batch: b, index: index}
	item := &b.plan.Items[index]
	if item.Outcome.Status != AdminBatchNotAttempted {
		return p, nil
	}
	var err error
	if b.plan.Action == commandAdminAssign {
		p.assignment, err = s.PrepareAssignmentInTx(ctx, tx, b.actor, item.Assignment)
	} else {
		a := item.Assignment
		p.command, err = s.PrepareInTx(
			ctx,
			tx,
			b.actor,
			Command{
				Name:          b.plan.Action,
				Event:         a.Event,
				Key:           a.Key,
				Version:       a.Version,
				Target:        a.Target,
				TargetVersion: a.TargetVersion,
			},
		)
	}
	return p, err
}

func (p *PreparedRuntimeBatchItem) Replayed() bool {
	item := &p.batch.plan.Items[p.index]
	if item.Outcome.Status != AdminBatchNotAttempted {
		return true
	}
	if p.assignment != nil {
		result, found := p.assignment.Replay()
		if found {
			item.Outcome.Assignment = &result
			item.Outcome.Status = AdminBatchSucceeded
		}
		return found
	}
	_, found := p.command.Replay()
	if found {
		item.Outcome.Status = AdminBatchSucceeded
	}
	return found
}

func (p *PreparedRuntimeBatchItem) Apply(ctx context.Context) error {
	if p.assignment != nil {
		result, err := p.assignment.Apply(ctx)
		if err == nil {
			p.batch.plan.Items[p.index].Outcome.Assignment = &result
		}
		return err
	}
	_, err := p.command.Apply(ctx)
	return err
}

// SaveOutcome records only domain rejections; infrastructure errors leave the
// pending marker intact so the exact operation can resume.
func (b *RuntimeBatchState) SaveOutcome(ctx context.Context, tx pgx.Tx, index int, result error) error {
	if err := adminBatchResult(&b.plan.Items[index].Outcome, result); err != nil {
		return err
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE core.pass_admin_batches SET plan=$3 WHERE actor=$1 AND key_hash=$2`,
		b.actor,
		b.key,
		b.plan,
	)
	return err
}
