package massage

import (
	"slices"
	"time"
)

const (
	DefaultCandidateLimit = 20
	MaxCandidateLimit     = 100
	MaxCandidateRange     = 24 * time.Hour
)

type ScheduleCandidateQuery struct {
	WindowID     string     `json:"window_id"             pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	SpecialistID string     `json:"specialist_id"         pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	ServiceID    string     `json:"service_id"            pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	From         time.Time  `json:"from"`
	Until        time.Time  `json:"until"`
	AfterStart   *time.Time `json:"after_start,omitempty"`
	Limit        int        `json:"limit,omitempty"                                         minimum:"1" maximum:"100"`
}

type ScheduleCandidate struct {
	StartsAt        time.Time `json:"starts_at"`
	TreatmentEndsAt time.Time `json:"treatment_ends_at"`
	OccupancyEndsAt time.Time `json:"occupancy_ends_at"`
}

type ScheduleCandidatePage struct {
	ObservedAt           time.Time           `json:"observed_at"`
	Timezone             string              `json:"timezone"`
	WindowID             string              `json:"window_id"`
	SpecialistID         string              `json:"specialist_id"`
	ServiceID            string              `json:"service_id"`
	Currency             string              `json:"currency"`
	CurrencyScale        int                 `json:"currency_scale"`
	PriceMinor           int64               `json:"price_minor"`
	TreatmentSeconds     int                 `json:"treatment_seconds"`
	OccupancySeconds     int                 `json:"occupancy_seconds"`
	RequiresTable        bool                `json:"requires_table"`
	ConfiguredTables     int                 `json:"configured_tables"`
	ScheduleOnly         bool                `json:"schedule_only"`
	ReservationSupported bool                `json:"reservation_supported"`
	Candidates           []ScheduleCandidate `json:"candidates"`
	HasMore              bool                `json:"has_more"`
	NextAfter            *time.Time          `json:"next_after,omitempty"`
}

func validCandidateQuery(query ScheduleCandidateQuery, observed time.Time) bool {
	if !validSpan(query.From, query.Until) || query.Until.Sub(query.From) > MaxCandidateRange ||
		!validInstant(observed) {
		return false
	}
	if query.AfterStart != nil && !validInstant(*query.AfterStart) {
		return false
	}
	return query.Limit >= 0 && query.Limit <= MaxCandidateLimit
}

// CandidateStarts enumerates one configured specialist/service/window grid.
// It neither inspects bookings nor applies booking-specific lead/count exceptions.
func CandidateStarts(
	d ScheduleDocument,
	query ScheduleCandidateQuery,
	observed time.Time,
	timezone string,
) (ScheduleCandidatePage, error) {
	result := ScheduleCandidatePage{ScheduleOnly: true, Candidates: []ScheduleCandidate{}}
	if !validCandidateQuery(query, observed) {
		return result, ErrInvalid
	}
	if query.Limit == 0 {
		query.Limit = DefaultCandidateLimit
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil || timezone == "Local" || timezone == "" {
		return result, ErrInvalid
	}
	d, err = Normalize(d)
	if err != nil {
		return result, err
	}
	windowIndex := slices.IndexFunc(d.Windows, func(value ScheduleWindow) bool { return value.ID == query.WindowID })
	specialistIndex := slices.IndexFunc(
		d.Specialists,
		func(value ScheduleSpecialist) bool { return value.ID == query.SpecialistID },
	)
	serviceIndex := slices.IndexFunc(
		d.Services,
		func(value ScheduleService) bool { return value.ID == query.ServiceID },
	)
	if windowIndex < 0 || specialistIndex < 0 || serviceIndex < 0 {
		return result, ErrInvalid
	}
	window, specialist, service := d.Windows[windowIndex], d.Specialists[specialistIndex], d.Services[serviceIndex]
	if !slices.Contains(specialist.ServiceIDs, service.ID) {
		return result, ErrInvalid
	}
	if window.StartsAt.In(zone).Year() < 1 || window.EndsAt.In(zone).Year() > 9999 {
		return result, ErrInvalid
	}
	result.ObservedAt, result.Timezone = observed.UTC(), timezone
	result.WindowID, result.SpecialistID, result.ServiceID = window.ID, specialist.ID, service.ID
	result.Currency, result.CurrencyScale, result.PriceMinor = d.Currency, d.CurrencyScale, service.PriceMinor
	result.OccupancySeconds = service.Blocks * d.BlockSeconds
	result.TreatmentSeconds = result.OccupancySeconds - d.BufferSeconds
	result.RequiresTable, result.ConfiguredTables = specialist.RequiresTable, window.Tables
	if specialist.RequiresTable && window.Tables == 0 {
		return result, nil
	}
	step := time.Duration(d.BlockSeconds) * time.Second
	occupancy := time.Duration(result.OccupancySeconds) * time.Second
	treatment := time.Duration(result.TreatmentSeconds) * time.Second
	start := firstCandidateStart(window.StartsAt, query, observed, step)
	for ; start.Before(query.Until) && !start.Add(occupancy).After(window.EndsAt); start = start.Add(step) {
		end := start.Add(occupancy)
		fits := slices.ContainsFunc(specialist.WorkSpans, func(span ScheduleWorkSpan) bool {
			return span.WindowID == window.ID && !start.Before(span.StartsAt) && !end.After(span.EndsAt)
		})
		if !fits {
			continue
		}
		if len(result.Candidates) == query.Limit {
			result.HasMore = true
			last := result.Candidates[len(result.Candidates)-1].StartsAt
			result.NextAfter = &last
			break
		}
		result.Candidates = append(result.Candidates, ScheduleCandidate{StartsAt: start.In(zone),
			TreatmentEndsAt: start.Add(treatment).In(zone), OccupancyEndsAt: end.In(zone)})
	}
	return result, nil
}

func firstCandidateStart(
	anchor time.Time,
	query ScheduleCandidateQuery,
	observed time.Time,
	step time.Duration,
) time.Time {
	first := query.From
	if observed.After(first) {
		first = observed
	}
	if query.AfterStart != nil && query.AfterStart.After(first) {
		first = *query.AfterStart
	}
	start := anchor
	if first.After(start) {
		delta := first.Sub(start)
		start = start.Add(delta - delta%step)
		if start.Before(first) {
			start = start.Add(step)
		}
	}
	if query.AfterStart != nil && start.Equal(*query.AfterStart) {
		start = start.Add(step)
	}
	return start
}
