package delivery

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery/dbgen"
)

// Register joins the owner's enqueue transaction. Lane locking serializes
// sequence visibility through commit, including concurrent domain enqueues.
// Callers must roll back on error and lock multiple lanes in sorted chat order.
func Register(
	ctx context.Context,
	tx pgx.Tx,
	botID int64,
	ref Reference,
	destination Destination,
	class Class,
) (Entry, error) {
	if botID <= 0 || !ref.valid() || !validDestination(destination) || (class != Interactive && class != Background) {
		return Entry{}, ErrQueueReference
	}
	q := dbgen.New(tx)
	if err := q.EnsureDeliveryLane(
		ctx,
		dbgen.EnsureDeliveryLaneParams{BotID: botID, Chat: destination.Chat},
	); err != nil {
		return Entry{}, core.DatabaseOperationError(err)
	}
	if _, err := q.LockDeliveryLane(
		ctx,
		dbgen.LockDeliveryLaneParams{BotID: botID, Chat: destination.Chat},
	); err != nil {
		return Entry{}, core.DatabaseOperationError(err)
	}
	row, err := readQueue(ctx, q, botID, ref)
	if err == nil {
		return registeredEntry(row, destination, class)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, err
	}
	sequence, err := q.AllocateDeliverySequence(
		ctx,
		dbgen.AllocateDeliverySequenceParams{BotID: botID, Chat: destination.Chat},
	)
	if err != nil {
		return Entry{}, core.DatabaseOperationError(err)
	}
	err = q.InsertDeliveryEntry(ctx, dbgen.InsertDeliveryEntryParams{
		BotID: botID, OwnerKind: string(ref.Owner), OwnerKey: ref.Key, EffectKey: ref.Effect,
		Chat: destination.Chat, ThreadID: destination.Thread, LaneSequence: sequence, TrafficClass: string(class),
	})
	if err != nil {
		return Entry{}, core.DatabaseOperationError(err)
	}
	row, err = readQueue(ctx, q, botID, ref)
	if err != nil {
		return Entry{}, err
	}
	return registeredEntry(row, destination, class)
}

func registeredEntry(row dbgen.ReadDeliveryEntryRow, destination Destination, class Class) (Entry, error) {
	entry := queueEntry(row)
	if entry.Destination != destination || entry.Class != class {
		return Entry{}, ErrQueueBinding
	}
	return entry, nil
}

// Begin runs after the owner has locked and validated its attempt and authority.
// Lock order is owner, lane, entry, bot/chat pacing, fairness cursor. It never
// calls a domain or provider. Commit the owner's sending state in this same tx.
func Begin(ctx context.Context, tx pgx.Tx, settings Settings, ref Reference) (Admission, error) {
	if err := settings.Validate(); err != nil {
		return Admission{}, err
	}
	row, err := lockQueue(ctx, tx, settings.BotID, ref)
	if err != nil {
		return Admission{}, err
	}
	q := dbgen.New(tx)
	gate, err := queueAdmission(ctx, q, row)
	if err != nil || !gate.Ready {
		return gate, err
	}
	gate, err = Reserve(ctx, tx, settings, Destination{Chat: row.Chat, Thread: row.ThreadID})
	if err != nil {
		return Admission{}, err
	}
	state := Deferred
	if gate.Ready {
		state = Sending
	}
	if err = projectQueue(ctx, q, row, state, gate.NotBefore); err != nil || !gate.Ready {
		return gate, err
	}
	grant, err := q.AdvanceDeliveryFairness(ctx, settings.BotID)
	if err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	// This lane was already locked before pacing and the cursor; no new lane
	// lock is acquired after the cursor.
	err = q.MarkDeliveryLaneServed(
		ctx,
		dbgen.MarkDeliveryLaneServedParams{BotID: settings.BotID, Chat: row.Chat, Grants: grant},
	)
	return gate, core.DatabaseOperationError(err)
}

func queueAdmission(ctx context.Context, q *dbgen.Queries, row dbgen.ReadDeliveryEntryRow) (Admission, error) {
	if Kind(row.State) != Deferred {
		return Admission{Reason: "delivery_not_pending"}, nil
	}
	head, err := q.IsDeliveryHead(
		ctx,
		dbgen.IsDeliveryHeadParams{BotID: row.BotID, Chat: row.Chat, LaneSequence: row.LaneSequence},
	)
	if err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	if !head.Valid || !head.Bool {
		return Admission{Reason: "delivery_lane_blocked"}, nil
	}
	clock, err := q.DeliveryClock(ctx)
	if err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	if row.NotBefore.InfinityModifier == pgtype.Infinity || row.NotBefore.Time.After(clock.Time) {
		return Admission{Reason: "delivery_cooldown", NotBefore: row.NotBefore.Time}, nil
	}
	return Admission{Ready: true}, nil
}

// Finish schedules a wire outcome and projects it under the same owner tx.
// The owner must fence its attempt before calling and save the returned outcome
// and deadline before commit. Calling Schedule then Project reverses lock order.
func Finish(
	ctx context.Context,
	tx pgx.Tx,
	settings Settings,
	ref Reference,
	outcome Outcome,
) (Outcome, time.Time, error) {
	if err := settings.Validate(); err != nil {
		return Outcome{}, time.Time{}, err
	}
	if !outcome.Valid() {
		return Outcome{}, time.Time{}, ErrQueueState
	}
	row, err := lockQueue(ctx, tx, settings.BotID, ref)
	if err != nil {
		return Outcome{}, time.Time{}, err
	}
	if !queueTransition(Kind(row.State), outcome.Kind) {
		return Outcome{}, time.Time{}, ErrQueueState
	}
	return finishLockedQueue(ctx, tx, settings, row, outcome)
}

// FinishUncertainSuccess resolves a known receipt while its uncertain retry is pending.
// The owner must first lock its pending attempt, prove the receipt generation is
// its last uncertain attempt, and reject a live lease. Save the returned outcome
// and deadline in the same transaction. This does not admit another send.
func FinishUncertainSuccess(
	ctx context.Context,
	tx pgx.Tx,
	settings Settings,
	ref Reference,
	outcome Outcome,
) (Outcome, time.Time, error) {
	if err := settings.Validate(); err != nil {
		return Outcome{}, time.Time{}, err
	}
	if outcome.Kind != Succeeded || !outcome.Valid() {
		return Outcome{}, time.Time{}, ErrQueueState
	}
	row, err := lockQueue(ctx, tx, settings.BotID, ref)
	if err != nil {
		return Outcome{}, time.Time{}, err
	}
	if Kind(row.State) != Deferred {
		return Outcome{}, time.Time{}, ErrQueueState
	}
	return finishLockedQueue(ctx, tx, settings, row, outcome)
}

func finishLockedQueue(
	ctx context.Context,
	tx pgx.Tx,
	settings Settings,
	row dbgen.ReadDeliveryEntryRow,
	outcome Outcome,
) (Outcome, time.Time, error) {
	result, deadline, err := Schedule(ctx, tx, settings, Destination{Chat: row.Chat, Thread: row.ThreadID}, outcome)
	if err != nil {
		return Outcome{}, time.Time{}, err
	}
	err = projectQueue(ctx, dbgen.New(tx), row, result.Kind, deadline)
	return result, deadline, err
}

// Project mirrors non-wire recovery, cancellation or owner-policy rejection in the owner's transaction.
// It never creates a reference or acquires pacing locks. Wire completions use
// Finish; attempt fencing and allowed recovery decisions remain owner policy.
func Project(ctx context.Context, tx pgx.Tx, botID int64, ref Reference, state Kind, notBefore time.Time) error {
	switch state {
	case Deferred, Cancelled, Uncertain, Parked, Paused, Rejected:
	case Sending, Succeeded:
		return ErrQueueState
	default:
		return ErrQueueState
	}
	row, err := lockQueue(ctx, tx, botID, ref)
	if err != nil {
		return err
	}
	if !queueTransition(Kind(row.State), state) {
		return ErrQueueState
	}
	return projectQueue(ctx, dbgen.New(tx), row, state, notBefore)
}

func queueTransition(previous, next Kind) bool {
	if terminal(previous) {
		return previous == next
	}
	return previous != Deferred || (next != Succeeded && next != Uncertain)
}

// Candidates returns advisory heads only. Commit/close this read transaction
// before owner work, and select again after each grant so fairness stays current.
// Three interactive grants alternate with one background grant when both exist.
func Candidates(ctx context.Context, tx pgx.Tx, botID int64, maximum int32) ([]Entry, error) {
	const maxCandidates = 100
	if botID <= 0 || maximum <= 0 || maximum > maxCandidates {
		return nil, ErrQueueReference
	}
	rows, err := dbgen.New(tx).DeliveryCandidates(ctx, dbgen.DeliveryCandidatesParams{BotID: botID, Maximum: maximum})
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, queueEntry(dbgen.ReadDeliveryEntryRow(row)))
	}
	return entries, nil
}

func readQueue(ctx context.Context, q *dbgen.Queries, botID int64, ref Reference) (dbgen.ReadDeliveryEntryRow, error) {
	row, err := q.ReadDeliveryEntry(ctx, dbgen.ReadDeliveryEntryParams{
		BotID: botID, OwnerKind: string(ref.Owner), OwnerKey: ref.Key, EffectKey: ref.Effect,
	})
	return row, core.DatabaseOperationError(err)
}

func lockQueue(ctx context.Context, tx pgx.Tx, botID int64, ref Reference) (dbgen.ReadDeliveryEntryRow, error) {
	if botID <= 0 || !ref.valid() {
		return dbgen.ReadDeliveryEntryRow{}, ErrQueueReference
	}
	q := dbgen.New(tx)
	row, err := readQueue(ctx, q, botID, ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrQueueReference
	}
	if err != nil {
		return row, err
	}
	if _, err = q.LockDeliveryLane(ctx, dbgen.LockDeliveryLaneParams{BotID: botID, Chat: row.Chat}); err != nil {
		return row, core.DatabaseOperationError(err)
	}
	locked, err := q.LockDeliveryEntry(ctx, dbgen.LockDeliveryEntryParams{
		BotID: botID, OwnerKind: string(ref.Owner), OwnerKey: ref.Key, EffectKey: ref.Effect,
	})
	return dbgen.ReadDeliveryEntryRow(locked), core.DatabaseOperationError(err)
}

func projectQueue(
	ctx context.Context,
	q *dbgen.Queries,
	row dbgen.ReadDeliveryEntryRow,
	state Kind,
	deadline time.Time,
) error {
	value := pgtype.Timestamptz{Time: deadline, Valid: true}
	if deadline.IsZero() {
		value.InfinityModifier = pgtype.NegativeInfinity
	}
	count, err := q.ProjectDeliveryEntry(ctx, dbgen.ProjectDeliveryEntryParams{
		BotID: row.BotID, OwnerKind: row.OwnerKind, OwnerKey: row.OwnerKey, EffectKey: row.EffectKey,
		State: string(state), NotBefore: value,
	})
	if err == nil && count != 1 {
		return ErrQueueReference
	}
	return core.DatabaseOperationError(err)
}

func queueEntry(row dbgen.ReadDeliveryEntryRow) Entry {
	return Entry{
		Reference:   Reference{Owner: Owner(row.OwnerKind), Key: row.OwnerKey, Effect: row.EffectKey},
		Destination: Destination{Chat: row.Chat, Thread: row.ThreadID}, Sequence: row.LaneSequence,
		Class: Class(row.TrafficClass), State: Kind(row.State), NotBefore: row.NotBefore.Time,
	}
}
