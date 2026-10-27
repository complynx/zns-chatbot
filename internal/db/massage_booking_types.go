package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/massage"
)

const massageBookingStateBooked = "booked"

type MassageBookingSource string

const (
	MassageSourceAgent    MassageBookingSource = "agent"
	MassageSourceMiniApp  MassageBookingSource = "miniapp"
	MassageSourceTelegram MassageBookingSource = "telegram"
)

type MassageBookingArguments struct {
	EventID                 uuid.UUID                `json:"event_id"`
	ExpectedEventVersion    int64                    `json:"expected_event_version"    minimum:"1"`
	ExpectedScheduleVersion int64                    `json:"expected_schedule_version" minimum:"1"`
	Selection               massage.BookingSelection `json:"selection"`
}

type MassageBookingSnapshot struct {
	Purpose              string               `json:"purpose,omitempty"`
	StaffBindingVersion  int64                `json:"staff_binding_version,omitempty"`
	EventName            string               `json:"event_name"`
	EventVersion         int64                `json:"event_version"`
	ScheduleVersion      int64                `json:"schedule_version"`
	MaxBookingsPerWindow int                  `json:"max_bookings_per_window"`
	Terms                massage.BookingTerms `json:"terms"`
}

type MassageBooking struct {
	ID          uuid.UUID              `json:"id"`
	EventID     uuid.UUID              `json:"event_id"`
	Version     int64                  `json:"version"`
	State       string                 `json:"state"`
	Snapshot    MassageBookingSnapshot `json:"snapshot"`
	Source      MassageBookingSource   `json:"source"`
	BookedAt    time.Time              `json:"booked_at"`
	CancelledAt *time.Time             `json:"cancelled_at,omitempty"`
}

type MassageBookingPreparation struct {
	ID        uuid.UUID               `json:"proposal_id"`
	Arguments MassageBookingArguments `json:"arguments"`
	Snapshot  MassageBookingSnapshot  `json:"snapshot"`
	ExpiresAt time.Time               `json:"expires_at"`
}

type MassageCancellationArguments struct {
	EventID         uuid.UUID `json:"event_id"`
	BookingID       uuid.UUID `json:"booking_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
}

type MassageCancellationPreparation struct {
	ID        uuid.UUID                    `json:"proposal_id"`
	Arguments MassageCancellationArguments `json:"arguments"`
	Booking   MassageBooking               `json:"booking"`
	ExpiresAt time.Time                    `json:"expires_at"`
}

type MassageBookingPage struct {
	Bookings  []MassageBooking `json:"bookings"`
	NextAfter *uuid.UUID       `json:"next_after,omitempty"`
}

func normalizeMassageBooking(a MassageBookingArguments) (MassageBookingArguments, error) {
	if a.EventID == uuid.Nil || a.ExpectedEventVersion < 1 || a.ExpectedScheduleVersion < 1 {
		return a, massage.ErrInvalid
	}
	selection, err := massage.NormalizeBookingSelection(a.Selection)
	a.Selection = selection
	return a, err
}

func validMassageCancellation(a MassageCancellationArguments) bool {
	return a.EventID != uuid.Nil && a.BookingID != uuid.Nil && a.ExpectedVersion > 0
}

func massageSourceValid(source MassageBookingSource) bool {
	return source == MassageSourceAgent || source == MassageSourceMiniApp || source == MassageSourceTelegram
}

// MassageOperationReceipt describes the original operation, not current booking state.
type MassageOperationReceipt struct {
	OperationID uuid.UUID            `json:"operation_id"`
	EventID     uuid.UUID            `json:"event_id"`
	Kind        string               `json:"kind"`
	Source      MassageBookingSource `json:"source"`
	ProposalID  uuid.UUID            `json:"proposal_id"`
	BookingID   uuid.UUID            `json:"booking_id"`
	Result      MassageBooking       `json:"result"`
	RecordedAt  time.Time            `json:"recorded_at"`
}
