// Package massage contains scheduling rules and transactional massage bookings.
package massage

import (
	"errors"
	"math"
	"time"
)

const (
	SlotDuration      = 20 * time.Minute
	serviceBuffer     = 5 * time.Minute
	partyTolerance    = 2 * time.Hour
	clientNotice      = 15 * time.Minute
	specialistFlyover = 5 * time.Minute
)

var (
	ErrLength   = errors.New("unsupported massage length")
	ErrCurrency = errors.New("unsupported massage currency")
	ErrTables   = errors.New("legacy shared table configuration is unimplemented")
)

type Currency string

const (
	BYN Currency = "BYN"
	RUB Currency = "RUB"
)

// Quote returns service duration and whole currency units. Regular clients see lengths 1, 2, 3, 5;
// specialist instant booking also exposes 4 and 6.
func Quote(length int, currency Currency) (time.Duration, int, error) {
	if length < 1 || length > 6 {
		return 0, 0, ErrLength
	}
	var prices [6]int
	switch currency {
	case BYN:
		prices = [6]int{43, 57, 90, 110, 125, 150}
	case RUB:
		prices = [6]int{1200, 1600, 2500, 3000, 3500, 4000}
	default:
		return 0, 0, ErrCurrency
	}
	return time.Duration(length)*SlotDuration - serviceBuffer, prices[length-1], nil
}

// Party uses instants. The importer must interpret legacy naive datetimes in Europe/Minsk.
type Party struct {
	Start  time.Time
	End    time.Time
	Tables int
}

type Span struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Booking struct {
	SpecialistID int64
	Slot         int
	Length       int
}

type Specialist struct {
	ID int64
	// This deliberately follows the truthiness of Python's misleading table_not_required field.
	LegacyTableFlag bool
	Work            []Span
}

type Slots map[int]map[int64]bool

// CurrentParty preserves config order and strict tolerance boundaries.
func CurrentParty(parties []Party, now time.Time) (Party, bool) {
	for _, party := range parties {
		if now.After(party.Start.Add(-partyTolerance)) && now.Before(party.End.Add(partyTolerance)) {
			return party, true
		}
	}
	return Party{}, false
}

func SlotTime(party Party, slot int) time.Time {
	return party.Start.Add(time.Duration(slot) * SlotDuration)
}

// InstantSlot requires a party selected with CurrentParty and a specialist actor.
func InstantSlot(party Party, now time.Time) int {
	slot := int(math.Floor(float64(now.Sub(party.Start)) / float64(SlotDuration)))
	if SlotTime(party, slot).Before(now.Add(-specialistFlyover)) {
		slot++
	}
	return slot
}

// WorkSlots preserves Python's span-start filtering: it does not clip span ends to party end.
func WorkSlots(party Party, spans []Span) map[int]bool {
	slots := make(map[int]bool)
	for _, span := range spans {
		if span.Start.Before(party.Start.Add(-partyTolerance)) || span.Start.After(party.End) {
			continue
		}
		first := int(math.Ceil(float64(span.Start.Sub(party.Start)) / float64(SlotDuration)))
		last := int(math.Floor(float64(span.End.Sub(party.Start)) / float64(SlotDuration)))
		for slot := first; slot < last; slot++ {
			slots[slot] = true
		}
	}
	return slots
}

// Available expects active bookings scoped to one event and party. It deliberately leaves
// specialist duration limits to the selection UI, as the Python availability function does.
func Available(party Party, specialists []Specialist, bookings []Booking, length int, now time.Time) (Slots, error) {
	if length < 1 || length > 6 {
		return nil, ErrLength
	}
	if now.After(party.End.Add(partyTolerance)) {
		return Slots{}, nil
	}
	tableIDs := make(map[int64]bool)
	for _, specialist := range specialists {
		if specialist.LegacyTableFlag {
			tableIDs[specialist.ID] = true
		}
	}
	if party.Tables != 1 && party.Tables < len(tableIDs) {
		return nil, ErrTables
	}
	occupied, tablesBusy := occupancy(bookings, tableIDs)
	available := make(Slots)
	for _, specialist := range specialists {
		for slot := range WorkSlots(party, specialist.Work) {
			if occupied[slot][specialist.ID] || (party.Tables == 1 && specialist.LegacyTableFlag && tablesBusy[slot]) {
				continue
			}
			if available[slot] == nil {
				available[slot] = make(map[int64]bool)
			}
			available[slot][specialist.ID] = true
		}
	}
	return consecutive(available, length), nil
}

func occupancy(bookings []Booking, tableIDs map[int64]bool) (Slots, map[int]bool) {
	occupied := make(Slots)
	tablesBusy := make(map[int]bool)
	for _, booking := range bookings {
		for slot := booking.Slot; slot < booking.Slot+booking.Length; slot++ {
			if occupied[slot] == nil {
				occupied[slot] = make(map[int64]bool)
			}
			occupied[slot][booking.SpecialistID] = true
			if tableIDs[booking.SpecialistID] {
				tablesBusy[slot] = true
			}
		}
	}
	return occupied, tablesBusy
}

func consecutive(available Slots, length int) Slots {
	result := make(Slots)
	for slot, specialists := range available {
		for id := range specialists {
			fits := true
			for offset := 1; offset < length; offset++ {
				if !available[slot+offset][id] {
					fits = false
					break
				}
			}
			if fits {
				if result[slot] == nil {
					result[slot] = make(map[int64]bool)
				}
				result[slot][id] = true
			}
		}
	}
	return result
}

type Decision string

const (
	Allowed        Decision = "allowed"
	Timeout        Decision = "timeout"
	DailyLimit     Decision = "daily_limit"
	Unavailable    Decision = "unavailable"
	ClientConflict Decision = "client_conflict"
)

type Request struct {
	Slot              int
	Length            int
	SpecialistID      int64
	ActorIsSpecialist bool
	DailyLimit        int
}

// Eligibility enforces the constraints used by the Python selection UI.
// The caller must recheck under transaction locks. ClientBookings must contain
// only this client's active bookings in this event and party; availability must match Length.
func Eligibility(party Party, now time.Time, request Request, available Slots, clientBookings []Booking) Decision {
	deadline := now.Add(clientNotice)
	if request.ActorIsSpecialist {
		deadline = now.Add(-specialistFlyover)
	}
	if SlotTime(party, request.Slot).Before(deadline) {
		return Timeout
	}
	if !request.ActorIsSpecialist && len(clientBookings) >= request.DailyLimit {
		return DailyLimit
	}
	extended := false
	if !request.ActorIsSpecialist {
		for _, booking := range clientBookings {
			if request.Slot >= booking.Slot-request.Length && request.Slot < booking.Slot+booking.Length {
				extended = true
			}
		}
	}
	specialists, exists := available[request.Slot]
	if !exists || !specialists[request.SpecialistID] {
		return Unavailable
	}
	if extended {
		return ClientConflict
	}
	return Allowed
}
