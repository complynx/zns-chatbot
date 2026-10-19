package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderEvidencePath = "/internal/agent/order-payments/"

type orderEvidenceListArguments struct {
	EventID uuid.UUID `json:"event_id"`
	OrderID uuid.UUID `json:"order_id,omitempty"`
	After   uuid.UUID `json:"after,omitempty"`
	Limit   int32     `json:"limit,omitempty"    minimum:"1" maximum:"20"`
}

type orderEvidenceOperationArguments struct {
	OperationID uuid.UUID                     `json:"operation_id"`
	Kind        db.OrderEvidenceOperationKind `json:"kind"         enum:"submit,review"`
}

func orderEvidenceRoute(method string) huma.Operation {
	return huma.Operation{
		OperationID:  "agent-order-payments-" + method,
		Method:       http.MethodPost,
		Path:         orderEvidencePath + method,
		MaxBodyBytes: internalBodyLimit,
	}
}

func (s *Server) registerOrderEvidence(api huma.API) {
	huma.Register(api, orderEvidenceRoute("discover"), s.discoverOrderEvidence)
	huma.Register(api, orderEvidenceRoute("evidence"), s.agentOwnOrderEvidence)
	huma.Register(api, orderEvidenceRoute("evidence_for_review"), s.agentOrderEvidenceForReview)
	huma.Register(api, orderEvidenceRoute("list"), s.agentOwnOrderEvidenceList)
	huma.Register(api, orderEvidenceRoute("queue"), s.agentOrderEvidenceQueue)
	huma.Register(api, orderEvidenceRoute("operation"), s.agentOrderEvidenceOperation)
	s.registerOrderEvidenceFiles(api)
	s.registerOrderEvidenceWrites(api)
}

func orderEvidenceError(err error) error {
	if errors.Is(err, orders.ErrSelection) {
		return huma.Error400BadRequest("invalid order evidence request")
	}
	return paymentError(err)
}

// Bound the full success envelope, including its operation ID and result.
func orderEvidenceReadResult[T any](operation string, result T) (*fileEnvelope, error) {
	return boundedOrderEvidenceJSON(agentReadResult(operation, result).Body)
}

// Raw JSON prevents schema-link injection from enlarging the checked envelope.
func boundedOrderEvidenceJSON(value any) (*fileEnvelope, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > agentAdminResultLimit {
		return nil, huma.Error503ServiceUnavailable("order evidence response exceeds supported bound")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: raw}, nil
}

func (s *Server) discoverOrderEvidence(ctx context.Context, in *AuthInput) (*agentDiscoveryResponse, error) {
	scope, err := s.paymentScope(ctx, *in)
	if err != nil {
		return nil, err
	}
	audit, err := s.store.HasOrdersEvidenceAuthority(ctx, scope)
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	return discoveryResult(orderEvidenceMethods(audit))
}

func (s *Server) agentOwnOrderEvidence(
	ctx context.Context,
	in *agentReadRequest[paymentEvidenceArguments],
) (*fileEnvelope, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnOrderEvidence(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.EvidenceID)
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	return orderEvidenceReadResult(in.Body.OperationID, result)
}

func (s *Server) agentOrderEvidenceForReview(
	ctx context.Context,
	in *agentReadRequest[paymentEvidenceArguments],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OrderEvidenceForReview(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.EvidenceID)
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	return orderEvidenceReadResult(in.Body.OperationID, result)
}

func (s *Server) agentOwnOrderEvidenceList(
	ctx context.Context,
	in *agentReadRequest[orderEvidenceListArguments],
) (*fileEnvelope, error) {
	return s.agentOrderEvidenceList(ctx, in, false)
}

func (s *Server) agentOrderEvidenceQueue(
	ctx context.Context,
	in *agentReadRequest[orderEvidenceListArguments],
) (*fileEnvelope, error) {
	return s.agentOrderEvidenceList(ctx, in, true)
}

func (s *Server) agentOrderEvidenceList(
	ctx context.Context,
	in *agentReadRequest[orderEvidenceListArguments],
	audit bool,
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = 20
	}
	page, err := s.store.OrderEvidenceList(ctx, scope, args.EventID, args.OrderID, args.After, args.Limit, audit)
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	// Shrink by actual envelope bytes. Cursor always names the last included row.
	for {
		out, boundErr := orderEvidenceReadResult(in.Body.OperationID, page)
		if boundErr == nil {
			return out, nil
		}
		if len(page.Evidence) <= 1 {
			return nil, boundErr
		}
		page.Evidence = page.Evidence[:len(page.Evidence)-1]
		last := page.Evidence[len(page.Evidence)-1].ID
		page.Next = &last
	}
}

func (s *Server) agentOrderEvidenceOperation(
	ctx context.Context,
	in *agentReadRequest[orderEvidenceOperationArguments],
) (*fileEnvelope, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	result, err := s.store.OrderEvidenceOperationByID(ctx, scope, args.OperationID, args.Kind)
	out := &paymentWriteResponse[db.OrderEvidenceOperation]{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return boundedOrderEvidenceJSON(out.Body)
	}
	if err != nil {
		return nil, orderEvidenceError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return boundedOrderEvidenceJSON(out.Body)
}
