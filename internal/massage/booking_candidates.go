package massage

import (
	"slices"
	"time"
)

// PublicBookingCandidateStarts applies owner notice and public duration rules.
// It does not inspect reservations; callers must add live occupancy before
// advertising availability. ObservedAt is the real observation, not the lead cutoff.
func PublicBookingCandidateStarts(document ScheduleDocument, query ScheduleCandidateQuery,
	observed time.Time, timezone string,
) (ScheduleCandidatePage, error) {
	var result ScheduleCandidatePage
	if !validInstant(observed) {
		return result, ErrInvalid
	}
	document, err := Normalize(document)
	if err != nil {
		return result, err
	}
	wi := slices.IndexFunc(document.Windows, func(w ScheduleWindow) bool { return w.ID == query.WindowID })
	si := slices.IndexFunc(document.Services, func(s ScheduleService) bool { return s.ID == query.ServiceID })
	if wi < 0 || si < 0 {
		return result, ErrInvalid
	}
	if err = validateBookingWindow(document, document.Windows[wi]); err != nil {
		return result, err
	}
	if !publicBookingBlocks(document.Services[si].Blocks) {
		return result, ErrUnavailable
	}
	result, err = CandidateStarts(document, query, observed.Add(ClientBookingLead), timezone)
	if err != nil {
		return result, err
	}
	result.ObservedAt = observed.UTC()
	return result, nil
}
