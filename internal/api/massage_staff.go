package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

const massageStaffPath = "/internal/agent/massage_staff/"

type massageStaffEventsArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

func (s *Server) registerMassageStaff(api huma.API) {
	s.registerMassageInstant(api)
	s.registerMassageSpecialistPreferences(api)
	registerAgentPost(
		api,
		"agent-massage-staff-windows",
		massageStaffPath+"windows",
		internalBodyLimit,
		s.massageStaffWindows,
	)

	registerAgentPost(
		api,
		"agent-massage-staff-events",
		massageStaffPath+"events",
		internalBodyLimit,
		s.massageStaffEvents,
	)
	registerAgentPost(
		api,
		"agent-massage-staff-schedule",
		massageStaffPath+"schedule",
		internalBodyLimit,
		s.massageStaffSchedule,
	)
	registerAgentPost(
		api,
		"agent-massage-staff-timetable",
		massageStaffPath+"timetable",
		internalBodyLimit,
		s.massageStaffTimetable,
	)
	registerAgentPost(
		api,
		"agent-massage-staff-client",
		massageStaffPath+"client",
		internalBodyLimit,
		s.massageStaffClient,
	)
}

func (s *Server) massageStaffEvents(
	ctx context.Context,
	in *agentReadRequest[massageStaffEventsArguments],
) (*agentReadResponse[events.ContextList], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.MassageStaffEvents(ctx, scope, a.After, int(a.Limit))
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageStaffSchedule(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.MassageStaffScheduleView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MassageStaffSchedule(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageStaffTimetable(
	ctx context.Context,
	in *agentReadRequest[db.MassageStaffQuery],
) (*agentReadResponse[db.MassageStaffPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MassageStaffBookings(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageStaffClient(
	ctx context.Context,
	in *agentReadRequest[massageBookingDetailArguments],
) (*agentReadResponse[db.MassageStaffBooking], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.MassageStaffClient(ctx, scope, a.EventID, a.BookingID)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageStaffWindows(ctx context.Context,
	in *agentReadRequest[db.MassageStaffWindowsQuery],
) (*agentReadResponse[db.MassageStaffWindowsPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MassageStaffWindows(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
