package massage

import (
	"slices"
	"time"
)

// SpecialistBookingGrace is the legacy allowance for starting in the current slot.
const SpecialistBookingGrace = 5 * time.Minute

const specialistPartyTolerance = 2 * time.Hour

// InstantSelection chooses configured terms; the server supplies the start time.
type InstantSelection struct {
	WindowID     string `json:"window_id"`
	SpecialistID string `json:"specialist_id"`
	ServiceID    string `json:"service_id"`
}

// CurrentInstantTerms chooses the current grid slot, advancing once when it is
// older than the grace period. It never searches for a later available slot.
// Private staff authority and occupied capacity are checked by the store.
func CurrentInstantTerms(
	document ScheduleDocument,
	selection InstantSelection,
	observed time.Time,
	timezone string,
) (BookingTerms, error) {
	if !validInstant(observed) {
		return BookingTerms{}, ErrInvalid
	}
	document, err := Normalize(document)
	if err != nil {
		return BookingTerms{}, err
	}
	index := slices.IndexFunc(document.Windows, func(window ScheduleWindow) bool {
		return window.ID == selection.WindowID
	})
	if index < 0 {
		return BookingTerms{}, ErrInvalid
	}
	window := document.Windows[index]
	if !observed.After(window.StartsAt.Add(-specialistPartyTolerance)) ||
		!observed.Before(window.EndsAt.Add(specialistPartyTolerance)) {
		return BookingTerms{}, ErrUnavailable
	}
	anchor := window.StartsAt
	block := time.Duration(document.BlockSeconds) * time.Second
	elapsed := observed.Sub(anchor)
	slot := int64(elapsed / block)
	if elapsed < 0 && elapsed%block != 0 {
		slot-- // Integer division truncates toward zero; the legacy grid uses floor.
	}
	start := anchor.Add(time.Duration(slot) * block)
	if start.Before(observed.Add(-SpecialistBookingGrace)) {
		start = start.Add(block)
	}
	return FrozenInstantTerms(document, BookingSelection{
		WindowID: selection.WindowID, SpecialistID: selection.SpecialistID,
		ServiceID: selection.ServiceID, StartsAt: start,
	}, observed, timezone)
}

// FrozenInstantTerms rechecks an already selected instant without moving it.
// Callers must bind the selection to the actor's exact stored staff proposal.
func FrozenInstantTerms(
	document ScheduleDocument,
	selection BookingSelection,
	observed time.Time,
	timezone string,
) (BookingTerms, error) {
	if !validInstant(observed) {
		return BookingTerms{}, ErrInvalid
	}
	return selectedBookingTerms(document, selection, observed.Add(-SpecialistBookingGrace), timezone, false)
}
