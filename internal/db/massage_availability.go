package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

type MassageAvailabilityArguments struct {
	EventID                 uuid.UUID                      `json:"event_id"`
	ExpectedEventVersion    int64                          `json:"expected_event_version"    minimum:"1"`
	ExpectedScheduleVersion int64                          `json:"expected_schedule_version" minimum:"1"`
	Query                   massage.ScheduleCandidateQuery `json:"query"`
}

type MassageAvailabilityCandidate struct {
	massage.ScheduleCandidate

	Available         bool   `json:"available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

// MassageAvailabilityView is an observation, not a proposal, reservation or consent.
type MassageAvailabilityView struct {
	EventID              uuid.UUID                      `json:"event_id"`
	EventVersion         int64                          `json:"event_version"`
	Version              int64                          `json:"version"`
	ObservedAt           time.Time                      `json:"observed_at"`
	Timezone             string                         `json:"timezone"`
	WindowID             string                         `json:"window_id"`
	SpecialistID         string                         `json:"specialist_id"`
	ServiceID            string                         `json:"service_id"`
	Currency             string                         `json:"currency"`
	CurrencyScale        int                            `json:"currency_scale"`
	PriceMinor           int64                          `json:"price_minor"`
	TreatmentSeconds     int                            `json:"treatment_seconds"`
	OccupancySeconds     int                            `json:"occupancy_seconds"`
	RequiresTable        bool                           `json:"requires_table"`
	ConfiguredTables     int                            `json:"configured_tables"`
	MaxBookingsPerWindow int                            `json:"max_bookings_per_window"`
	CurrentBookings      int64                          `json:"current_bookings"`
	ScheduleOnly         bool                           `json:"schedule_only"`
	ReservationSupported bool                           `json:"reservation_supported"`
	Candidates           []MassageAvailabilityCandidate `json:"candidates"`
	HasMore              bool                           `json:"has_more"`
	NextAfter            *time.Time                     `json:"next_after,omitempty"`
}

func validMassageAvailability(args MassageAvailabilityArguments) bool {
	return args.EventID != uuid.Nil && args.ExpectedEventVersion > 0 && args.ExpectedScheduleVersion > 0
}

// OwnMassageAvailability reads entitlement, configuration and light occupancy
// rows in one consistent owner-authorized snapshot. No booking/proposal is created.
func (s *Store) OwnMassageAvailability(
	ctx context.Context,
	scope ProfileScope,
	args MassageAvailabilityArguments,
) (MassageAvailabilityView, error) {
	var result MassageAvailabilityView
	if !validMassageAvailability(args) {
		return result, massage.ErrInvalid
	}
	tx, q, err := s.massageOwnerReadTx(ctx, scope, args.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	event, err := q.GetEvent(
		ctx,
		sqlc.GetEventParams{ID: args.EventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	)
	if err != nil {
		return result, err
	}
	return readMassageAvailability(ctx, q, scope, event, args)
}

// OwnMiniAppMassageAvailability validates the session inside the read snapshot,
// so logout and revocation cannot be bypassed by a stale preflight.
func (s *Store) OwnMiniAppMassageAvailability(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, args MassageAvailabilityArguments,
) (MassageAvailabilityView, error) {
	var result MassageAvailabilityView
	if !validMassageAvailability(args) {
		return result, massage.ErrInvalid
	}
	tx, q, event, scope, err := s.miniAppMassageReadTx(ctx, deployment, session, args.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return readMassageAvailability(ctx, q, scope, event, args)
}

func readMassageAvailability(ctx context.Context, q *sqlc.Queries, scope ProfileScope,
	event sqlc.ShuttleEvent, args MassageAvailabilityArguments,
) (MassageAvailabilityView, error) {
	var result MassageAvailabilityView
	config, maximum, err := massageBookingConfiguration(
		ctx,
		q,
		scope,
		event,
		args.ExpectedEventVersion,
		args.ExpectedScheduleVersion,
	)
	if err != nil {
		return result, err
	}
	observed, err := q.RegistrationNow(ctx)
	if err != nil {
		return result, err
	}
	page, err := massage.PublicBookingCandidateStarts(config.Document, args.Query, observed.Time, event.Timezone)
	if err != nil {
		return result, err
	}
	count, err := q.CountOwnPartyMassages(
		ctx,
		sqlc.CountOwnPartyMassagesParams{EventID: event.ID, UserID: scope.UserID, WindowID: page.WindowID},
	)
	if err != nil {
		return result, err
	}
	occupied, err := massageAvailabilityOccupations(ctx, q, scope, event.ID, page)
	if err != nil {
		return result, err
	}
	result = MassageAvailabilityView{
		EventID:              event.ID,
		EventVersion:         event.Version,
		Version:              config.Version,
		ObservedAt:           page.ObservedAt,
		Timezone:             page.Timezone,
		WindowID:             page.WindowID,
		SpecialistID:         page.SpecialistID,
		ServiceID:            page.ServiceID,
		Currency:             page.Currency,
		CurrencyScale:        page.CurrencyScale,
		PriceMinor:           page.PriceMinor,
		TreatmentSeconds:     page.TreatmentSeconds,
		OccupancySeconds:     page.OccupancySeconds,
		RequiresTable:        page.RequiresTable,
		ConfiguredTables:     page.ConfiguredTables,
		MaxBookingsPerWindow: maximum,
		CurrentBookings:      count,
		ReservationSupported: true,
		Candidates:           []MassageAvailabilityCandidate{},
		HasMore:              page.HasMore,
		NextAfter:            page.NextAfter,
	}
	for _, candidate := range page.Candidates {
		selection := massage.BookingSelection{StartsAt: candidate.StartsAt}
		terms := massage.BookingTerms{
			BookingSelection: selection,
			OccupancyEndsAt:  candidate.OccupancyEndsAt,
			RequiresTable:    page.RequiresTable,
			ConfiguredTables: page.ConfiguredTables,
		}
		slot := MassageAvailabilityCandidate{ScheduleCandidate: candidate, Available: true}
		if count >= int64(maximum) {
			slot.Available = false
			slot.UnavailableReason = "party_limit"
		} else if !massage.BookingSlotAvailable(terms, occupied) {
			slot.Available = false
			slot.UnavailableReason = "occupied"
		}
		result.Candidates = append(result.Candidates, slot)
	}
	return result, nil
}

func massageAvailabilityOccupations(
	ctx context.Context,
	q *sqlc.Queries,
	scope ProfileScope,
	eventID uuid.UUID,
	page massage.ScheduleCandidatePage,
) ([]massage.BookingOccupation, error) {
	if len(page.Candidates) == 0 {
		return nil, nil
	}
	first, last := page.Candidates[0], page.Candidates[len(page.Candidates)-1]
	rows, err := q.ReadMassageBookingOccupations(ctx, sqlc.ReadMassageBookingOccupationsParams{
		EventID: eventID, FromAt: massageTimestamp(first.StartsAt), UntilAt: massageTimestamp(last.OccupancyEndsAt),
	})
	if err != nil {
		return nil, err
	}
	return massageOccupations(rows, scope.UserID, page.SpecialistID), nil
}
