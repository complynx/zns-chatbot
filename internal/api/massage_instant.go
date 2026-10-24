package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const massageInstantPath = massageStaffPath + "instant"
const massageInstantSelection = "selection"

type massageInstantPreparationArguments struct {
	db.MassageInstantArguments

	ExistingProposalID massageProposalID `json:"existing_proposal_id,omitzero"`
}

func (s *Server) registerMassageInstant(api huma.API) {
	registerAgentPost(api, "agent-prepare-massage-instant", massageInstantPath+"/prepare",
		internalBodyLimit, s.prepareMassageInstant)
	registerAgentPost(api, "agent-massage-instant", massageInstantPath,
		internalBodyLimit, s.bookMassageInstant)
	registerAgentPost(api, "agent-massage-instant-status", massageInstantPath+"/status",
		internalBodyLimit, s.massageInstantStatus)
}

func (s *Server) prepareMassageInstant(ctx context.Context,
	in *agentReadRequest[massageInstantPreparationArguments],
) (*massagePreparationResponse[db.MassageInstantArguments], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	operation, err := uuid.Parse(in.Body.OperationID)
	if err != nil || operation == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	var prepared db.MassageInstantPreparation
	if proposal := in.Body.Arguments.ExistingProposalID.UUID; proposal != uuid.Nil {
		prepared, err = s.store.AdoptOwnMassageInstant(
			ctx, scope, operation, proposal, in.Body.Arguments.MassageInstantArguments,
		)
	} else {
		prepared, err = s.store.PrepareOwnMassageInstant(
			ctx, scope, operation, in.Body.Arguments.MassageInstantArguments,
		)
	}
	if err != nil {
		return nil, massageBookingError(err)
	}
	language, err := s.orderPresentationLanguage(ctx, scope)
	if err != nil {
		return nil, massageBookingError(err)
	}
	description, err := massageBookingDescription(prepared.Snapshot, language, false)
	if err != nil {
		return nil, massageBookingError(err)
	}
	out := &massagePreparationResponse[db.MassageInstantArguments]{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = description
	return out, nil
}

func (s *Server) bookMassageInstant(ctx context.Context,
	in *massageWriteRequest[db.MassageInstantArguments],
) (*paymentWriteResponse[db.MassageBooking], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if err = requireExactOperation(
		b.OperationID,
		b.IdempotencyKey,
		b.Confirmation.Preconditions.ProposalID,
	); err != nil {
		return nil, err
	}
	result, err := s.store.BookOwnMassageInstant(ctx, scope, b.OperationID,
		b.Confirmation.Preconditions.ProposalID, b.Arguments, db.MassageSourceAgent)
	return massageBookingResult(b.OperationID.String(), result, err)
}

func (s *Server) massageInstantStatus(ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[db.MassageBooking], error) {
	return s.massageEffectStatus(ctx, in, "instant")
}

func massageInstantMethod() map[string]any {
	properties := massageEventProperties()
	properties["existing_proposal_id"] = map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	properties[schemaExpectedEventVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	properties[massageBindingExpectedScheduleVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	id := map[string]any{manifestType: schemaString, schemaPattern: ordersSchemaIDPattern}
	properties[massageInstantSelection] = ordersObjectSchema(map[string]any{
		massageSchemaWindowID: id, massageSchemaSpecialistID: id, schemaServiceID: id,
	}, []string{massageSchemaWindowID, massageSchemaSpecialistID, schemaServiceID})
	method := paymentMethod(
		"instant",
		"Reserve only your OWN specialist time after exact human approval of the "+
			"server's frozen proposal. Read massage_staff.schedule for own alias IDs "+
			"and configured windows/services. Send current event/schedule versions an"+
			"d window/own-alias/service IDs; never supply a client or start time. The"+
			" server selects the current slot, advances once if over five minutes old"+
			", and freezes it; it never searches later gaps. Current private own bind"+
			"ing, enabled massage module, binding generation, versions, clock and all"+
			" occupied capacity are rechecked on apply. No pass or ordinary per-party"+
			" limit is required; public IDs and admin roles grant no specialist autho"+
			"rity. Preparation holds no capacity. To continue an existing instant cho"+
			"ice, pass existing_proposal_id plus its exact instant arguments; prepara"+
			"tion preserves its original time, proposal and expiry and strips the sel"+
			"ector from normalized arguments. Require fresh approval of the displayed"+
			" self reservation and exact local start/end. Revocation/regrant cannot r"+
			"evive old consent. Recover the same operation/status or massage.operatio"+
			"n after uncertainty; consumed proposal replay never creates another book"+
			"ing. Use massage.get/list for owner history and massage.cancel for expli"+
			"cit owner release. This cannot book for another person, confirm payment "+
			"or create a refund.",
		properties,
		[]string{
			schemaEventID,
			schemaExpectedEventVersion,
			massageBindingExpectedScheduleVersion,
			massageInstantSelection,
		},
		[]string{"id", schemaEventID, schemaVersion, schemaState, massageSchemaSnapshot},
	)
	method[manifestPath], method[manifestStatusPath] = massageInstantPath, massageInstantPath+"/status"
	method[manifestEffect], method[manifestIdempotency] = effectWrite, true
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestConfirmation] = map[string]any{
		confirmationMode: assignmentConfirmationMode, manifestPreparePath: massageInstantPath + "/prepare",
	}
	return method
}
