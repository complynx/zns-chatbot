package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const MaxMassageReminderLeadSeconds = math.MaxInt32
const defaultMassageReminderLongSeconds = 3600
const defaultMassageReminderShortSeconds = 600

const (
	MassageReminderLong  = "owner_long"
	MassageReminderShort = "owner_short"
)

// MassageReminderLeads uses nonnegative 32-bit seconds; zero retains each legacy
// default. The storage bound also keeps duration arithmetic within int64.
func (d Settings) MassageReminderLeads() (int32, int32, error) {
	long, short := d.MassageNotifyClientPriorLongSeconds, d.MassageNotifyClientPriorSeconds
	if long < 0 || short < 0 || long > MaxMassageReminderLeadSeconds || short > MaxMassageReminderLeadSeconds {
		return 0, 0, fmt.Errorf("invalid massage reminder lead")
	}
	if long == 0 {
		long = defaultMassageReminderLongSeconds
	}
	if short == 0 {
		short = defaultMassageReminderShortSeconds
	}
	return int32(long), int32(short), nil
}

type MassageReminder struct {
	ID            uuid.UUID       `json:"id"`
	Booking       MassageBooking  `json:"booking"`
	RecipientID   uuid.UUID       `json:"recipient_id"`
	Kind          string          `json:"kind"`
	LeadSeconds   int32           `json:"lead_seconds"`
	DueAt         time.Time       `json:"due_at"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	State         string          `json:"state"`
	Version       int64           `json:"version"`
	AttemptID     uuid.UUID       `json:"attempt_id"`
	Payload       json.RawMessage `json:"-"`
	Receipt       json.RawMessage `json:"receipt"`
	CreatedAt     time.Time       `json:"created_at"`
}

func readMassageReminder(
	ctx context.Context,
	q *sqlc.Queries,
	scope EventScope,
	id uuid.UUID,
) (MassageReminder, error) {
	row, err := q.ReadMassageReminder(
		ctx,
		sqlc.ReadMassageReminderParams{ID: id, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return MassageReminder{}, err
	}
	booking, err := massageBookingRecord(row.ShuttleMassageBooking)
	if err != nil {
		return MassageReminder{}, err
	}
	n := row.ShuttleMassageReminder
	return MassageReminder{
		ID:            n.ID,
		Booking:       booking,
		RecipientID:   row.ShuttleMassageBooking.UserID,
		Kind:          n.Kind,
		LeadSeconds:   n.LeadSeconds,
		DueAt:         n.DueAt.Time.UTC(),
		NextAttemptAt: n.NextAttemptAt.Time.UTC(),
		State:         n.State,
		Version:       n.Version,
		AttemptID: uuid.UUID(
			n.AttemptID.Bytes,
		),
		Payload:   n.Payload,
		Receipt:   n.Receipt,
		CreatedAt: n.CreatedAt.Time.UTC(),
	}, nil
}

// MassageReminder reads deployment-scoped delivery state for trusted workers or
// an authorized administrator. It does not establish a participant permission.
func (s *Store) MassageReminder(ctx context.Context, scope EventScope, id uuid.UUID) (MassageReminder, error) {
	return readMassageReminder(ctx, s.q, scope, id)
}

func readMassageReminderLeads(ctx context.Context, q *sqlc.Queries) (int32, int32, error) {
	settings, err := q.ReadSettingsForMassage(ctx)
	if err != nil {
		return 0, 0, err
	}
	var document Settings
	if err = json.Unmarshal(settings.Document, &document); err != nil {
		return 0, 0, err
	}
	return document.MassageReminderLeads()
}

// insertMassageReminders shares the booking transaction and its settings lock.
func insertMassageReminders(ctx context.Context, q *sqlc.Queries, booking uuid.UUID) error {
	long, short, err := readMassageReminderLeads(ctx, q)
	if err != nil {
		return err
	}
	for _, reminder := range []struct {
		kind    string
		seconds int32
	}{{MassageReminderLong, long}, {MassageReminderShort, short}} {
		if err = q.InsertMassageReminder(
			ctx,
			sqlc.InsertMassageReminderParams{
				ID:          uuid.New(),
				BookingID:   booking,
				Kind:        reminder.kind,
				LeadSeconds: reminder.seconds,
			},
		); err != nil {
			return err
		}
	}
	return nil
}

// Multirow mutations lock reminder IDs in the same order as settings rescheduling.
func blockBookingMassageReminders(ctx context.Context, q *sqlc.Queries, booking uuid.UUID) error {
	if _, err := q.LockSchedulableMassageReminders(ctx, booking); err != nil {
		return err
	}
	return q.BlockBookingMassageReminders(ctx, booking)
}

func rescheduleMassageReminders(ctx context.Context, q *sqlc.Queries, before, after Settings) error {
	oldLong, oldShort, err := before.MassageReminderLeads()
	if err != nil {
		return err
	}
	long, short, err := after.MassageReminderLeads()
	if err != nil {
		return err
	}
	if oldLong == long && oldShort == short {
		return nil
	}
	// The successful settings CAS already holds the settings update lock. Do not
	// lock events/owners/bookings here: booking and reservation acquire those first.
	if _, err = q.LockSchedulableMassageReminders(ctx, uuid.Nil); err != nil {
		return err
	}
	return q.RescheduleMassageReminders(
		ctx,
		sqlc.RescheduleMassageRemindersParams{LongSeconds: long, ShortSeconds: short},
	)
}
