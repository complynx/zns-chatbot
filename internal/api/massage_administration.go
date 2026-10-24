package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) administrativeMassageBookings(ctx context.Context,
	in *agentReadRequest[massageBookingListArguments],
) (*agentReadResponse[db.MassageAdministrationPage], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	page, err := s.store.AdministrativeMassageBookings(ctx, scope, a.EventID, a.After, a.Limit)
	if err != nil {
		return nil, massageBookingError(err)
	}
	page, err = boundedAdministrativeMassagePage(page)
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, page), nil
}

func boundedAdministrativeMassagePage(page db.MassageAdministrationPage) (db.MassageAdministrationPage, error) {
	for count := len(page.Bookings); count > 0; count-- {
		result := page
		result.Bookings = page.Bookings[:count]
		if count < len(page.Bookings) {
			next := result.Bookings[count-1].Booking.ID
			result.NextAfter = &next
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return page, err
		}
		if len(raw) <= massagePageResultLimit {
			return result, nil
		}
	}
	if len(page.Bookings) == 0 {
		return page, nil
	}
	return page, fmt.Errorf("stored administrative massage booking exceeds response bound")
}

func massageAdministrationReadMethod() map[string]any {
	properties := massageEventProperties()
	properties["after"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	properties[schemaLimit] = map[string]any{
		manifestType:  schemaInteger,
		schemaMinimum: 1,
		schemaMaximum: agentTaskPageLimit,
	}
	return administrativeRead(
		"massage_bookings",
		"Read retained massage bookings and cancellations for one deployment event, across owners. "+
			"Requires global zinusya.admin; event authority, an assigned pass and public specialist "+
			"IDs do not grant this read. Returns owner UUIDs with booking terms, no private "+
			"legal/passport fields or Telegram credentials. Follow next_after as after until absent; "+
			"pages may contain fewer than limit due to the byte bound. "+
			"Includes event timezone and observation time; "+
			"preserve each booking's historical terms/timezone snapshot. "+
			"Historical access needs no active pass or enabled module. "+
			"This reads history, not a staff booking permission or current reservation availability.",
		properties,
		[]string{schemaEventID},
		[]string{schemaEventID, schemaTimezone, schemaObservedAt, "bookings"},
	)
}
