package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const paymentPath = "/internal/agent/payments/"

type paymentConfirmation struct {
	Mode          string                   `json:"mode"                    enum:"none,exact_parameters"`
	Preconditions *assignmentPreconditions `json:"preconditions,omitempty"`
}
type paymentWriteBody[T any] struct {
	ProtocolVersion int                  `json:"protocol_version"       minimum:"1" maximum:"1"`
	OperationID     string               `json:"operation_id"                                   minLength:"1" maxLength:"128"`
	RunID           string               `json:"run_id,omitempty"`
	Arguments       T                    `json:"arguments"`
	IdempotencyKey  string               `json:"idempotency_key"                                minLength:"1" maxLength:"128"`
	Confirmation    *paymentConfirmation `json:"confirmation,omitempty"`
}
type paymentWriteRequest[T any] struct {
	AuthInput

	Body paymentWriteBody[T]
}
type paymentWriteResponse[T any] struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *T                      `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}
type paymentPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     payments.ReviewChange   `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}
type paymentBalanceArguments struct {
	EventID uuid.UUID `json:"event_id"`
	PassID  uuid.UUID `json:"pass_id"`
}
type paymentEvidenceArguments struct {
	EventID    uuid.UUID `json:"event_id"`
	EvidenceID uuid.UUID `json:"evidence_id"`
}
type paymentListArguments struct {
	EventID uuid.UUID `json:"event_id"`
	After   uuid.UUID `json:"after,omitempty"`
	Limit   int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}
type paymentListResult struct {
	Evidence []payments.Evidence `json:"evidence"`
	Next     *uuid.UUID          `json:"next,omitempty"`
}

func (s *Server) registerPayments(api huma.API) {
	s.registerPaymentExpectations(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-discover",
			Method:       http.MethodPost,
			Path:         paymentPath + "discover",
			MaxBodyBytes: internalBodyLimit,
		},
		s.discoverPayments,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-submit",
			Method:       http.MethodPost,
			Path:         paymentPath + "submit",
			MaxBodyBytes: internalBodyLimit,
		},
		s.submitPayment,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-submit-status",
			Method:       http.MethodPost,
			Path:         paymentPath + "submit/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentSubmissionStatus,
	)
	s.registerPaymentReads(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-prepare",
			Method:       http.MethodPost,
			Path:         paymentPath + "review/prepare",
			MaxBodyBytes: internalBodyLimit,
		},
		s.preparePaymentReview,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-review",
			Method:       http.MethodPost,
			Path:         paymentPath + "review",
			MaxBodyBytes: internalBodyLimit,
		},
		s.reviewPayment,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-review-status",
			Method:       http.MethodPost,
			Path:         paymentPath + "review/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentReviewStatus,
	)
	s.registerPaymentWorkspaces(api)
	s.registerPaymentFiles(api)
}
func (s *Server) paymentScope(ctx context.Context, auth AuthInput) (db.PaymentScope, error) {
	p, err := s.authorize(ctx, auth.Authorization, false)
	if err != nil {
		return db.PaymentScope{}, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return db.PaymentScope{}, err
	}
	_, admin := p.Roles["zinusya.admin"][s.options.OrganizationID]
	return db.PaymentScope{EventScope: s.eventScope(), UserID: u.ID, Subject: p.Subject, GlobalAdmin: admin}, nil
}
func (s *Server) discoverPayments(ctx context.Context, in *AuthInput) (*agentDiscoveryResponse, error) {
	scope, err := s.paymentScope(ctx, *in)
	if err != nil {
		return nil, err
	}
	audit, err := s.store.HasPaymentAuthority(ctx, scope)
	if err != nil {
		return nil, paymentError(err)
	}
	return discoveryResult(paymentMethods(audit))
}

func (s *Server) submitPayment(
	ctx context.Context,
	in *paymentWriteRequest[payments.Submission],
) (*paymentWriteResponse[payments.Evidence], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID ||
		body.Confirmation != nil &&
			(body.Confirmation.Mode != confirmationNone || body.Confirmation.Preconditions != nil) {
		return nil, huma.Error400BadRequest("matching operation key and no-confirmation mode required")
	}
	result, err := s.store.SubmitPaymentEvidence(ctx, scope, body.OperationID, body.Arguments)
	return paymentWriteResult(body.OperationID, result, err)
}

func (s *Server) preparePaymentReview(
	ctx context.Context,
	in *agentReadRequest[payments.ReviewChange],
) (*paymentPreparationResponse, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PreparePaymentReview(ctx, scope, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, paymentError(err)
	}
	out := &paymentPreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prep.Arguments
	out.Body.Preconditions.ProposalID = prep.ID
	out.Body.Description, out.Body.ExpiresAt = prep.Description, prep.ExpiresAt
	return out, nil
}

func (s *Server) reviewPayment(
	ctx context.Context,
	in *paymentWriteRequest[payments.ReviewChange],
) (*paymentWriteResponse[payments.ReviewResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	body := in.Body
	if body.IdempotencyKey != body.OperationID || body.Confirmation == nil ||
		body.Confirmation.Mode != assignmentConfirmationMode ||
		body.Confirmation.Preconditions == nil ||
		body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation key and exact proposal confirmation required")
	}
	result, err := s.store.ReviewPaymentEvidence(
		ctx,
		scope,
		body.OperationID,
		body.Arguments,
		body.Confirmation.Preconditions.ProposalID,
	)
	return paymentWriteResult(body.OperationID, result, err)
}
func paymentWriteResult[T any](operation string, result T, err error) (*paymentWriteResponse[T], error) {
	out := &paymentWriteResponse[T]{}
	out.Body.OperationID = operation
	if err == nil {
		out.Body.Status = agentSucceeded
		out.Body.Result = &result
		return out, nil
	}
	if errors.Is(err, db.ErrConflict) || errors.Is(err, payments.ErrInvalid) ||
		errors.Is(err, payments.ErrUnavailable) ||
		errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "payment_rejected",
			Message:        err.Error(),
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	return nil, paymentError(err)
}
func paymentError(err error) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}
	switch {
	case errors.Is(err, payments.ErrDenied):
		return huma.Error403Forbidden("event payment authority required")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("payment evidence or account not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("payment version, source or operation changed")
	case errors.Is(err, payments.ErrInvalid):
		return huma.Error400BadRequest("invalid payment request, amount or undocumented exception")
	case errors.Is(err, payments.ErrUnavailable):
		return huma.Error403Forbidden("event payment module unavailable")
	default:
		return huma.Error503ServiceUnavailable("payment storage unavailable")
	}
}

func (s *Server) paymentBalance(
	ctx context.Context,
	in *agentReadRequest[paymentBalanceArguments],
) (*agentReadResponse[payments.Balance], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PaymentBalance(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.PassID)
	if err != nil {
		return nil, paymentError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) paymentEvidence(
	ctx context.Context,
	in *agentReadRequest[paymentEvidenceArguments],
) (*agentReadResponse[payments.Evidence], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PaymentEvidence(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.EvidenceID)
	if err != nil {
		return nil, paymentError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) listPaymentEvidence(
	ctx context.Context,
	in *agentReadRequest[paymentListArguments],
	audit bool,
) (*agentReadResponse[paymentListResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.PaymentEvidenceList(ctx, scope, args.EventID, args.After, args.Limit, audit)
	if err != nil {
		return nil, paymentError(err)
	}
	result := paymentListResult{Evidence: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) paymentList(
	ctx context.Context,
	in *agentReadRequest[paymentListArguments],
) (*agentReadResponse[paymentListResult], error) {
	return s.listPaymentEvidence(ctx, in, false)
}

func (s *Server) paymentQueue(
	ctx context.Context,
	in *agentReadRequest[paymentListArguments],
) (*agentReadResponse[paymentListResult], error) {
	return s.listPaymentEvidence(ctx, in, true)
}
func (s *Server) paymentReceipt(ctx context.Context, in *agentRegistrationStatusRequest) (db.PaymentReceipt, error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return db.PaymentReceipt{}, err
	}
	receipt, err := s.store.PaymentOperation(ctx, scope, in.Body.OperationID)
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}
func paymentStatusResult[T any](operation string, result *T, err error) (*paymentWriteResponse[T], error) {
	out := &paymentWriteResponse[T]{}
	out.Body.OperationID = operation
	if errors.Is(err, pgx.ErrNoRows) || err == nil && result == nil {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, paymentError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = result
	return out, nil
}

func (s *Server) paymentSubmissionStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[payments.Evidence], error) {
	receipt, err := s.paymentReceipt(ctx, in)
	return paymentStatusResult(in.Body.OperationID, receipt.Evidence, err)
}

func (s *Server) paymentReviewStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[payments.ReviewResult], error) {
	receipt, err := s.paymentReceipt(ctx, in)
	return paymentStatusResult(in.Body.OperationID, receipt.Review, err)
}

func (s *Server) registerPaymentReads(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-events",
			Method:       http.MethodPost,
			Path:         paymentPath + "events",
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentEvents,
	)

	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-balance",
			Method:       http.MethodPost,
			Path:         paymentPath + "balance",
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentBalance,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-evidence",
			Method:       http.MethodPost,
			Path:         paymentPath + schemaEvidence,
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentEvidence,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-list",
			Method:       http.MethodPost,
			Path:         paymentPath + "list",
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentList,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-queue",
			Method:       http.MethodPost,
			Path:         paymentPath + "queue",
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentQueue,
	)
}

type paymentEventsResult struct {
	Events []db.PaymentEventChoice `json:"events"`
	Next   *uuid.UUID              `json:"next,omitempty"`
}

func (s *Server) paymentEvents(
	ctx context.Context,
	in *agentReadRequest[agentEventListArguments],
) (*agentReadResponse[paymentEventsResult], error) {
	scope, err := s.paymentScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = agentTaskPageLimit
	}
	rows, err := s.store.PaymentEvents(ctx, scope, args.After, args.Limit)
	if err != nil {
		return nil, paymentError(err)
	}
	result := paymentEventsResult{Events: rows}
	if len(rows) == int(args.Limit) {
		result.Next = &rows[len(rows)-1].ID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
