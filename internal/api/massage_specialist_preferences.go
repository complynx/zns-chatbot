package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massagePreferenceWritePath = massageStaffPath + "set_preferences"
const massagePreferenceNotifyBookings = "notify_bookings"
const massagePreferenceNotifyNext = "notify_next"

func (s *Server) registerMassageSpecialistPreferences(api huma.API) {
	registerAgentPost(
		api,
		"agent-massage-staff-preferences",
		massageStaffPath+"preferences",
		internalBodyLimit,
		s.massageSpecialistPreferences,
	)
	registerAgentPost(
		api,
		"agent-massage-staff-set-preferences",
		massagePreferenceWritePath,
		internalBodyLimit,
		s.setMassageSpecialistPreferences,
	)
	for _, suffix := range []string{routeStatus, routeReconcile} {
		registerAgentPost(
			api,
			"agent-massage-staff-preferences-"+suffix,
			massagePreferenceWritePath+"/"+suffix,
			internalBodyLimit,
			s.massageSpecialistPreferenceStatus,
		)
	}
}

func (s *Server) massageSpecialistPreferences(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.MassageSpecialistPreferences], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MassageSpecialistPreferences(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) setMassageSpecialistPreferences(
	ctx context.Context,
	in *profileWriteRequest[db.MassageSpecialistPreferenceChange],
) (*paymentWriteResponse[db.MassageSpecialistPreferences], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("matching nonzero operation UUID and idempotency key required")
	}
	result, err := s.store.SetMassageSpecialistPreferences(ctx, scope, operation, in.Body.Arguments)
	return massagePreferenceResult(in.Body.OperationID, result, err)
}

func (s *Server) massageSpecialistPreferenceStatus(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[db.MassageSpecialistPreferences], error) {
	scope, operation, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMassageSpecialistPreferenceOperation(ctx, scope, operation)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.MassageSpecialistPreferences]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return massagePreferenceResult(in.Body.OperationID, result, err)
}

func massagePreferenceResult(
	operation string,
	result db.MassageSpecialistPreferences,
	err error,
) (*paymentWriteResponse[db.MassageSpecialistPreferences], error) {
	out := &paymentWriteResponse[db.MassageSpecialistPreferences]{}
	out.Body.OperationID = operation
	if err != nil {
		if errors.Is(err, db.ErrConflict) || errors.Is(err, massage.ErrInvalid) || errors.Is(err, pgx.ErrNoRows) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "massage_preference_rejected",
				Message:        "Specialist access, selected event, preference version or operation rejected",
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, massageBookingError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func massageSpecialistPreferenceMethods() []map[string]any {
	get := paymentMethod(
		"preferences",
		"Read your global specialist booking/cancellation and upcoming-session notification "+
			"preferences across ALL your events in this bot account. "+
			"Select one enabled event with a current own private specialist binding for authorization; "+
			"a pass or administrator role is not a substitute. Both flags default to true/version0. "+
			"This is not an event-specific preference.",
		massageEventProperties(),
		[]string{schemaEventID},
		[]string{"user_id", massagePreferenceNotifyBookings, massagePreferenceNotifyNext, schemaVersion},
	)
	get[manifestPath] = massageStaffPath + "preferences"
	properties := massageEventProperties()
	properties[schemaExpectedVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 0}
	properties[massagePreferenceNotifyBookings] = map[string]any{manifestType: schemaBoolean}
	properties[massagePreferenceNotifyNext] = map[string]any{manifestType: schemaBoolean}
	set := paymentMethod(
		"set_preferences",
		"Set your own global specialist booking/cancellation and upcoming-session notification "+
			"preferences across ALL your events in this bot account, only on explicit user intent. "+
			"Select an enabled event with a current own binding and read preferences first. "+
			"Send expected_version and at least one explicit boolean: notify_bookings or notify_next, "+
			"including false. An omitted flag stays unchanged. "+
			"A same-value write keeps the version but retains its receipt. "+
			"Same operation ID returns its original result without changing newer preferences. "+
			"Missing status is not proof of non-execution: reconcile/retry the same operation before "+
			"creating another. Each flag has an independent delivery generation; "+
			"changing one does not invalidate notifications governed by the other.",
		properties,
		[]string{schemaEventID, schemaExpectedVersion},
		[]string{"user_id", massagePreferenceNotifyBookings, massagePreferenceNotifyNext, schemaVersion},
	)
	inputSchema := ordersObjectSchema(properties, []string{schemaEventID, schemaExpectedVersion})
	inputSchema["anyOf"] = []any{
		map[string]any{
			manifestType: schemaObject,
			schemaProperties: map[string]any{
				massagePreferenceNotifyBookings: properties[massagePreferenceNotifyBookings],
			},
			schemaRequired: []string{massagePreferenceNotifyBookings},
		},
		map[string]any{
			manifestType:     schemaObject,
			schemaProperties: map[string]any{massagePreferenceNotifyNext: properties[massagePreferenceNotifyNext]},
			schemaRequired:   []string{massagePreferenceNotifyNext},
		},
	}
	set[manifestInputSchema] = inputSchema
	set[manifestPath] = massagePreferenceWritePath
	set[manifestEffect] = effectWrite
	set[manifestIdempotency] = true
	set[manifestStatusPath] = massagePreferenceWritePath + "/status"
	set[manifestDedupWindow] = agentRegistrationDedupSeconds
	set[manifestConfirmation] = map[string]any{confirmationMode: confirmationNone}
	return []map[string]any{get, set}
}
