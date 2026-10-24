package db

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massageInstantPurpose = "staff_self"
const massageInstantKind = "instant"

// MassageInstantArguments reserves the actor's own specialist time. The start
// and binding generation belong to the server's stored proposal, never the caller.
type MassageInstantArguments struct {
	EventID                 uuid.UUID                `json:"event_id"`
	ExpectedEventVersion    int64                    `json:"expected_event_version"    minimum:"1"`
	ExpectedScheduleVersion int64                    `json:"expected_schedule_version" minimum:"1"`
	Selection               massage.InstantSelection `json:"selection"`
}

type MassageInstantPreparation struct {
	ID        uuid.UUID               `json:"proposal_id"`
	Arguments MassageInstantArguments `json:"arguments"`
	Snapshot  MassageBookingSnapshot  `json:"snapshot"`
	ExpiresAt time.Time               `json:"expires_at"`
}

func validMassageInstant(a MassageInstantArguments) bool {
	return a.EventID != uuid.Nil && a.ExpectedEventVersion > 0 && a.ExpectedScheduleVersion > 0 &&
		massageSpecialistID.MatchString(a.Selection.WindowID) &&
		massageSpecialistID.MatchString(a.Selection.SpecialistID) &&
		massageSpecialistID.MatchString(a.Selection.ServiceID)
}

// currentMassageInstantSnapshot runs under the event write lock and actor lock.
// A nil frozen value selects time once; a retained proposal must never move it.
func currentMassageInstantSnapshot(ctx context.Context, q *sqlc.Queries,
	scope ProfileScope, event sqlc.ShuttleEvent, args MassageInstantArguments,
	frozen *MassageBookingSnapshot,
) (MassageBookingSnapshot, error) {
	var result MassageBookingSnapshot
	if !slices.Contains(event.Modules, "massage") {
		return result, massage.ErrUnavailable
	}
	binding, err := q.ReadMassageSpecialistBinding(ctx, sqlc.ReadMassageSpecialistBindingParams{
		EventID: event.ID, SpecialistID: args.Selection.SpecialistID,
	})
	if err != nil {
		return result, err
	}
	if !binding.Active || binding.UserID != scope.UserID {
		return result, pgx.ErrNoRows
	}
	schedule, maximum, err := massageScheduleConfiguration(
		ctx, q, event, args.ExpectedEventVersion, args.ExpectedScheduleVersion,
	)
	if err != nil {
		return result, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	var terms massage.BookingTerms
	if frozen == nil {
		terms, err = massage.CurrentInstantTerms(schedule.Document, args.Selection, now.Time, event.Timezone)
	} else {
		selection := frozen.Terms.BookingSelection
		if frozen.Purpose != massageInstantPurpose || frozen.StaffBindingVersion != binding.Version ||
			selection.WindowID != args.Selection.WindowID || selection.SpecialistID != args.Selection.SpecialistID ||
			selection.ServiceID != args.Selection.ServiceID {
			return result, ErrConflict
		}
		terms, err = massage.FrozenInstantTerms(schedule.Document, selection, now.Time, event.Timezone)
	}
	if err != nil {
		return result, err
	}
	rows, err := q.ReadMassageBookingOccupations(ctx, sqlc.ReadMassageBookingOccupationsParams{
		EventID: event.ID, FromAt: massageTimestamp(terms.StartsAt), UntilAt: massageTimestamp(terms.OccupancyEndsAt),
	})
	if err != nil {
		return result, err
	}
	if !massage.BookingSlotAvailable(terms, massageOccupations(rows, scope.UserID, terms.SpecialistID)) {
		return result, massage.ErrUnavailable
	}
	return MassageBookingSnapshot{
		Purpose: massageInstantPurpose, StaffBindingVersion: binding.Version,
		EventName: event.Name, EventVersion: event.Version, ScheduleVersion: schedule.Version,
		MaxBookingsPerWindow: maximum, Terms: terms,
	}, nil
}

func revalidateMassageInstant(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, args MassageInstantArguments, prepared sqlc.ShuttleMassagePreparation,
) (MassageBookingSnapshot, []byte, error) {
	var frozen MassageBookingSnapshot
	if err := json.Unmarshal(prepared.Snapshot, &frozen); err != nil {
		return frozen, nil, err
	}
	snapshot, err := currentMassageInstantSnapshot(ctx, q, scope, event, args, &frozen)
	if err != nil {
		return snapshot, nil, err
	}
	raw, err := json.Marshal(snapshot)
	if err == nil {
		err = matchMassageSnapshot(prepared.Snapshot, raw)
	}
	return snapshot, raw, err
}
