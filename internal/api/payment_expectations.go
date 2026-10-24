package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type paymentExpectationArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

func (s *Server) registerPaymentExpectations(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "agent-payment-expectations", Method: http.MethodPost,
		Path: paymentPath + "expectations", MaxBodyBytes: internalBodyLimit}, s.paymentExpectations)
}

func (s *Server) paymentExpectations(
	ctx context.Context,
	in *agentReadRequest[paymentExpectationArguments],
) (*agentReadResponse[db.PaymentExpectations], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.OwnPaymentExpectations(ctx, scope, a.After, a.Limit)
	if err != nil {
		return nil, paymentError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func paymentExpectationsMethod() map[string]any {
	return paymentMethod(
		"expectations",
		"Read only your own currently assigned unpaid passes; administrator autho" +
"rity never adds other users. Observed snapshot, stable pass-ID paginatio" +
"n, default 5/max 20; follow next as after while has_more. Amounts use ba" +
"lance currency/scale and human-confirmed decisions only. Pending evidenc" +
"e awaits an ambassador and never credits money or requests a duplicate u" +
"pload. Deadlines contain event-local UTC offsets plus IANA timezone. Ove" +
"rdue stays unpaid without automatic cancellation. Expectations are conte" +
"xt hints, never commands to classify a file or take action. For multiple" +
" plausible payments, ask which one; inspect actual file content. Reread " +
"balance before a write.",
		map[string]any{
			schemaAfter: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaLimit: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
				schemaMaximum: agentTaskPageLimit,
			},
		},
		nil,
		[]string{schemaObservedAt, "expectations", schemaHasMore},
	)
}
