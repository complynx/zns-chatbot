package massage

import (
	"errors"
	"slices"
	"time"
)

// ClientBookingLead is the legacy minimum notice for ordinary participants.
const ClientBookingLead = 15 * time.Minute

var ErrUnavailable = errors.New("massage slot is unavailable")
var ErrBookingTopology = errors.New("massage booking requires nonoverlapping party windows")

type BookingSelection struct {
	WindowID     string    `json:"window_id"`
	SpecialistID string    `json:"specialist_id"`
	ServiceID    string    `json:"service_id"`
	StartsAt     time.Time `json:"starts_at"`
}

// BookingTerms freezes the selected service, displayed price and occupied time.
// It is a proposal, not a reservation or confirmation of payment.
type BookingTerms struct {
	BookingSelection

	WindowName       ScheduleLabel `json:"window_name"`
	SpecialistName   ScheduleLabel `json:"specialist_name"`
	ServiceName      ScheduleLabel `json:"service_name"`
	Timezone         string        `json:"timezone"`
	WindowStartsAt   time.Time     `json:"window_starts_at"`
	WindowEndsAt     time.Time     `json:"window_ends_at"`
	TreatmentEndsAt  time.Time     `json:"treatment_ends_at"`
	OccupancyEndsAt  time.Time     `json:"occupancy_ends_at"`
	RequiresTable    bool          `json:"requires_table"`
	ConfiguredTables int           `json:"configured_tables"`
	Currency         string        `json:"currency"`
	CurrencyScale    int           `json:"currency_scale"`
	PriceMinor       int64         `json:"price_minor"`
}

// NormalizeBookingSelection validates public IDs and canonicalizes the chosen instant.
func NormalizeBookingSelection(selection BookingSelection) (BookingSelection, error) {
	if !identifier.MatchString(selection.WindowID) || !identifier.MatchString(selection.SpecialistID) ||
		!identifier.MatchString(selection.ServiceID) || !validInstant(selection.StartsAt) {
		return BookingSelection{}, ErrInvalid
	}
	selection.StartsAt = selection.StartsAt.UTC()
	return selection, nil
}

// PublicBookingTerms applies the legacy ordinary-client lead and public lengths.
// Specialist exceptions require private authority and are not inferred from IDs.
func PublicBookingTerms(
	document ScheduleDocument,
	selection BookingSelection,
	observed time.Time,
	timezone string,
) (BookingTerms, error) {
	if !validInstant(observed) {
		return BookingTerms{}, ErrInvalid
	}
	return selectedBookingTerms(document, selection, observed.Add(ClientBookingLead), timezone, true)
}

func selectedBookingTerms(
	document ScheduleDocument,
	selection BookingSelection,
	earliest time.Time,
	timezone string,
	public bool,
) (BookingTerms, error) {
	var result BookingTerms
	selection, err := NormalizeBookingSelection(selection)
	if err != nil || !validInstant(earliest) {
		return result, ErrInvalid
	}
	document, err = Normalize(document)
	if err != nil {
		return result, err
	}
	wi := slices.IndexFunc(document.Windows, func(w ScheduleWindow) bool { return w.ID == selection.WindowID })
	si := slices.IndexFunc(
		document.Specialists,
		func(s ScheduleSpecialist) bool { return s.ID == selection.SpecialistID },
	)
	vi := slices.IndexFunc(document.Services, func(s ScheduleService) bool { return s.ID == selection.ServiceID })
	if wi < 0 || si < 0 || vi < 0 {
		return result, ErrInvalid
	}
	window, specialist, service := document.Windows[wi], document.Specialists[si], document.Services[vi]
	if err = validateBookingWindow(document, window); err != nil {
		return result, err
	}
	if public && !publicBookingBlocks(service.Blocks) {
		return result, ErrUnavailable
	}
	page, err := CandidateStarts(document, ScheduleCandidateQuery{
		WindowID: selection.WindowID, SpecialistID: selection.SpecialistID, ServiceID: selection.ServiceID,
		From: selection.StartsAt, Until: selection.StartsAt.Add(time.Microsecond), Limit: 1,
	}, earliest, timezone)
	if err != nil {
		return result, err
	}
	if len(page.Candidates) != 1 || !page.Candidates[0].StartsAt.Equal(selection.StartsAt) {
		return result, ErrUnavailable
	}
	candidate := page.Candidates[0]
	zone := candidate.StartsAt.Location()
	selection.StartsAt = candidate.StartsAt
	result = BookingTerms{
		BookingSelection: selection,
		WindowName:       window.Name,
		SpecialistName:   specialist.Name,
		ServiceName:      service.Name,
		Timezone:         timezone,
		WindowStartsAt:   window.StartsAt.In(zone),
		WindowEndsAt:     window.EndsAt.In(zone),
		TreatmentEndsAt:  candidate.TreatmentEndsAt,
		OccupancyEndsAt:  candidate.OccupancyEndsAt,
		RequiresTable:    specialist.RequiresTable,
		ConfiguredTables: window.Tables,
		Currency:         document.Currency,
		CurrencyScale:    document.CurrencyScale,
		PriceMinor:       service.PriceMinor,
	}
	return result, nil
}

// BookingFitsSchedule permits price/label edits while preserving every occupied
// interval and its table requirement. Historical price terms are never rewritten.
// Callers separately check current window capacity across all committed bookings.
func BookingFitsSchedule(document ScheduleDocument, terms BookingTerms) bool {
	current, err := selectedBookingTerms(document, terms.BookingSelection, terms.StartsAt, terms.Timezone, false)
	return err == nil && current.TreatmentEndsAt.Equal(terms.TreatmentEndsAt) &&
		current.OccupancyEndsAt.Equal(terms.OccupancyEndsAt) && current.RequiresTable == terms.RequiresTable
}

// BookingOccupation describes an existing, noncancelled reservation. Database
// callers derive owner/specialist comparisons; these flags are not user input.
type BookingOccupation struct {
	StartsAt       time.Time
	EndsAt         time.Time
	SameOwner      bool
	SameSpecialist bool
	RequiresTable  bool
}

// BookingSlotAvailable needs current, freshly derived terms, not a historical
// snapshot whose configured table count may have changed.
func BookingSlotAvailable(terms BookingTerms, occupied []BookingOccupation) bool {
	if !validSpan(terms.StartsAt, terms.OccupancyEndsAt) {
		return false
	}
	for _, other := range occupied {
		if !validSpan(other.StartsAt, other.EndsAt) {
			return false
		}
		if other.StartsAt.Before(terms.OccupancyEndsAt) && terms.StartsAt.Before(other.EndsAt) &&
			(other.SameOwner || other.SameSpecialist) {
			return false
		}
	}
	if !terms.RequiresTable {
		return true
	}
	peak, err := PeakTableUse(terms.StartsAt, terms.OccupancyEndsAt, occupied)
	return err == nil && peak < terms.ConfiguredTables
}

type tableBoundary struct {
	at    time.Time
	delta int
}

// PeakTableUse counts simultaneous use, not the number of intersecting bookings.
// Half-open occupancy allows one booking to start exactly when another ends.
func PeakTableUse(start, end time.Time, occupied []BookingOccupation) (int, error) {
	if !validSpan(start, end) {
		return 0, ErrInvalid
	}
	points := make([]tableBoundary, 0, len(occupied))
	for _, other := range occupied {
		if !validSpan(other.StartsAt, other.EndsAt) {
			return 0, ErrInvalid
		}
		if !other.RequiresTable || !other.StartsAt.Before(end) || !start.Before(other.EndsAt) {
			continue
		}
		left, right := other.StartsAt, other.EndsAt
		if left.Before(start) {
			left = start
		}
		if right.After(end) {
			right = end
		}
		points = append(points, tableBoundary{left, 1}, tableBoundary{right, -1})
	}
	slices.SortFunc(points, func(a, b tableBoundary) int {
		if order := a.at.Compare(b.at); order != 0 {
			return order
		}
		return a.delta - b.delta
	})
	current, peak := 0, 0
	for _, point := range points {
		current += point.delta
		peak = max(peak, current)
	}
	return peak, nil
}

func validateBookingWindow(document ScheduleDocument, window ScheduleWindow) error {
	for _, other := range document.Windows {
		if other.ID != window.ID && other.StartsAt.Before(window.EndsAt) && window.StartsAt.Before(other.EndsAt) {
			return ErrBookingTopology
		}
	}
	return nil
}

func publicBookingBlocks(blocks int) bool { return slices.Contains([]int{1, 2, 3, 5}, blocks) }
