package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

type massageBindingListArguments struct {
	EventID uuid.UUID `json:"event_id"`
	After   string    `json:"after,omitempty" maxLength:"32"`
	Limit   int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type massageBindingArguments struct {
	EventID      uuid.UUID `json:"event_id"`
	SpecialistID string    `json:"specialist_id" minLength:"1" maxLength:"32"`
}

func (s *Server) registerMassageBindings(api huma.API) {
	s.registerMassageBindingWrites(api)
	registerAgentPost(
		api,
		"agent-massage-binding-events",
		"/internal/agent/administration/massage_specialist_events",
		internalBodyLimit,
		s.massageBindingEvents,
	)
	registerAgentPost(
		api,
		"agent-massage-bindings",
		"/internal/agent/administration/massage_specialist_bindings",
		internalBodyLimit,
		s.massageBindings,
	)
	registerAgentPost(
		api,
		"agent-massage-binding",
		"/internal/agent/administration/massage_specialist_binding",
		internalBodyLimit,
		s.massageBinding,
	)
	registerAgentPost(
		api,
		"agent-massage-binding-target",
		"/internal/agent/administration/massage_specialist_target",
		internalBodyLimit,
		s.massageBindingTarget,
	)
}

func (s *Server) massageBindingEvents(
	ctx context.Context,
	in *agentReadRequest[massageStaffEventsArguments],
) (*agentReadResponse[events.ContextList], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.MassageBindingEvents(ctx, scope, nil, a.After, a.Limit)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageBindings(
	ctx context.Context,
	in *agentReadRequest[massageBindingListArguments],
) (*agentReadResponse[db.MassageBindingPage], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.MassageBindingConfiguration(ctx, scope, nil, a.EventID, a.After, a.Limit)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageBinding(
	ctx context.Context,
	in *agentReadRequest[massageBindingArguments],
) (*agentReadResponse[db.MassageSpecialistBinding], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.MassageBinding(ctx, scope, nil, a.EventID, a.SpecialistID)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) massageBindingTarget(
	ctx context.Context,
	in *agentReadRequest[db.MassageBindingTargetQuery],
) (*agentReadResponse[db.MassageBindingTarget], error) {
	scope, err := s.massageAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MassageBindingTargetLookup(ctx, scope, nil, in.Body.Arguments)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
