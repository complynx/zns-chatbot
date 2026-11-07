package api

import (
	"context"

	"github.com/complynx/zns-chatbot/internal/db"
)

const (
	massageSchemaCandidates           = "candidates"
	massageSchemaScheduleOnly         = "schedule_only"
	massageSchemaReservationSupported = "reservation_supported"
)

func (s *Server) ownMassageAvailability(
	ctx context.Context,
	in *agentReadRequest[db.MassageAvailabilityArguments],
) (*agentReadResponse[db.MassageAvailabilityView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMassageAvailability(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, massageBookingError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func massageAvailabilityMethod() map[string]any {
	props := massageEventProperties()
	props[schemaExpectedEventVersion] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	props["expected_schedule_version"] = map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	props["query"] = massageCandidateQuerySchema()
	method := paymentMethod(
		"availability",
		"Observe live public massage starts with your current assigned pass, including unpaid. "+
			"Resolve event context for module massage, read schedule versions, then supply a "+
			"specialist/service/window and an absolute range of at most 24 hours; "+
			"pages have at most 100 candidates. Times include event-zone offsets, including repeated "+
			"DST times. Public lengths are 1, 2, 3 or 5 blocks, with 15-minute notice. "+
			"Each candidate explicitly reports available or a non-identifying occupied/party_limit "+
			"reason; configured_tables is not remaining capacity. "+
			"This reads actual reservations and owner party count, but creates no preparation or hold. "+
			"Availability can change immediately; book/prepare and exact approved apply remain "+
			"authoritative. Follow next_after as query.after_start with unchanged range/versions, "+
			"including when every returned candidate is unavailable. "+
			"Never use schedule-only candidate_starts as live availability.",
		props,
		[]string{schemaEventID, schemaExpectedEventVersion, "expected_schedule_version", "query"},
		[]string{
			schemaEventID,
			ordersSchemaEventVersion,
			schemaVersion,
			schemaObservedAt,
			massageSchemaCandidates,
			massageSchemaScheduleOnly,
			massageSchemaReservationSupported,
		},
	)
	method[manifestPath] = massageBookingPath + "availability"
	return method
}
