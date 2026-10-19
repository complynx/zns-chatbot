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
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const assignmentConfirmationMode = "exact_parameters"

type assignmentPreconditions struct {
	ProposalID uuid.UUID `json:"proposal_id"`
}
type assignmentPreparationArguments struct {
	db.PassAssignment

	ResumeProposalID *uuid.UUID `json:"resume_proposal_id,omitempty"`
}

type agentAssignmentRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                            `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string                         `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string                         `json:"run_id,omitempty"`
		Arguments       assignmentPreparationArguments `json:"arguments"`
		IdempotencyKey  string                         `json:"idempotency_key" minLength:"1" maxLength:"128"`
		Confirmation    struct {
			Mode          string                  `json:"mode" enum:"exact_parameters"`
			Preconditions assignmentPreconditions `json:"preconditions"`
		} `json:"confirmation"`
	}
}
type agentAssignmentPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.PassAssignment       `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}
type agentEventArguments struct {
	ID uuid.UUID `json:"id"`
}
type agentEventListArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}
type agentRegistrationListArguments struct {
	EventID uuid.UUID `json:"event_id"`
	After   uuid.UUID `json:"after,omitempty"`
	Limit   int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}
type agentEventListResult struct {
	Events []events.Event `json:"events"`
	Next   *uuid.UUID     `json:"next,omitempty"`
}
type agentRegistrationListResult struct {
	Registrations []registrations.Registration `json:"registrations"`
	Next          *uuid.UUID                   `json:"next,omitempty"`
}

func (s *Server) registerAgentAssignments(api huma.API) {
	s.registerAssignmentProposalReads(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-prepare-pass",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/assign/prepare",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentPrepareAssignment,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-assign-pass",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/assign",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentAssignPass,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-assignment-status",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/assign/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentAssignmentStatus,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-events",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/events",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentEvents,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-event",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/event",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentEvent,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-registrations",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/registrations",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentRegistrations,
	)
}

func (s *Server) agentPrepareAssignment(
	ctx context.Context,
	in *agentReadRequest[assignmentPreparationArguments],
) (*agentAssignmentPreparationResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	var prepared db.AssignmentPreparation
	if in.Body.Arguments.ResumeProposalID != nil {
		prepared, err = s.store.AdoptAssignment(
			ctx,
			s.eventScope(),
			p.Subject,
			in.Body.OperationID,
			in.Body.Arguments.PassAssignment,
			*in.Body.Arguments.ResumeProposalID,
		)
	} else {
		prepared, err = s.store.PrepareAssignment(
			ctx,
			s.eventScope(),
			p.Subject,
			in.Body.OperationID,
			in.Body.Arguments.PassAssignment,
		)
	}
	if err != nil {
		return nil, registrationError(err)
	}
	out := &agentAssignmentPreparationResponse{}
	out.Body.Status = agentPrepared
	out.Body.OperationID = in.Body.OperationID
	out.Body.Arguments = prepared.Arguments
	out.Body.Preconditions.ProposalID = prepared.ID
	out.Body.Description = prepared.Description
	out.Body.ExpiresAt = prepared.ExpiresAt
	return out, nil
}

func (s *Server) agentAssignPass(
	ctx context.Context,
	in *agentAssignmentRequest,
) (*agentRegistrationStatusResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	if in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("idempotency key must match operation ID")
	}
	if in.Body.Arguments.ResumeProposalID != nil &&
		*in.Body.Arguments.ResumeProposalID != in.Body.Confirmation.Preconditions.ProposalID {
		return nil, huma.Error400BadRequest("resume proposal must match exact confirmation")
	}
	result, err := s.store.ConfirmAssignment(
		ctx,
		s.eventScope(),
		p.Subject,
		in.Body.OperationID,
		in.Body.Arguments.PassAssignment,
		in.Body.Confirmation.Preconditions.ProposalID,
	)
	return agentRegistrationWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) agentAssignmentStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*agentRegistrationStatusResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.AssignmentOperation(ctx, s.eventScope(), p.Subject, in.Body.OperationID)
	out := &agentRegistrationStatusResponse{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, registrationError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}

func (s *Server) agentEvents(
	ctx context.Context,
	in *agentReadRequest[agentEventListArguments],
) (*agentReadResponse[agentEventListResult], error) {
	limit := in.Body.Arguments.Limit
	if limit == 0 {
		limit = agentTaskPageLimit
	}
	result, err := s.listEvents(
		ctx,
		&eventListRequest{AuthInput: in.AuthInput, After: in.Body.Arguments.After, Limit: limit},
	)
	if err != nil {
		return nil, err
	}
	return agentReadResult(
		in.Body.OperationID,
		agentEventListResult{Events: result.Body.Events, Next: result.Body.Next},
	), nil
}

func (s *Server) agentEvent(
	ctx context.Context,
	in *agentReadRequest[agentEventArguments],
) (*agentReadResponse[events.Event], error) {
	result, err := s.readEvent(ctx, &eventRequest{AuthInput: in.AuthInput, ID: in.Body.Arguments.ID})
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result.Body), nil
}

func (s *Server) agentRegistrations(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationListArguments],
) (*agentReadResponse[agentRegistrationListResult], error) {
	a := in.Body.Arguments
	if a.Limit == 0 {
		a.Limit = agentTaskPageLimit
	}
	result, err := s.listRegistrations(
		ctx,
		&registrationListRequest{AuthInput: in.AuthInput, ID: a.EventID, After: a.After, Limit: a.Limit},
	)
	if err != nil {
		return nil, err
	}
	return agentReadResult(
		in.Body.OperationID,
		agentRegistrationListResult{Registrations: result.Body.Registrations, Next: result.Body.Next},
	), nil
}

func assignmentMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	pageFields := map[string]any{
		schemaAfter: uuidField,
		schemaLimit: map[string]any{manifestType: schemaInteger, schemaMinimum: 1, schemaMaximum: agentTaskPageLimit},
	}
	queueFields := map[string]any{
		schemaEventID: uuidField,
		schemaAfter:   uuidField,
		schemaLimit:   pageFields[schemaLimit],
	}
	assignFields := map[string]any{
		schemaEventID:            uuidField,
		schemaRegistrationID:     uuidField,
		schemaExpectedVersion:    map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
		"resume_proposal_id":     uuidField,
		"expected_event_version": map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
	}
	return append(assignmentProposalMethods(), []map[string]any{
		administrativeRead(
			schemaEvents,
			"List this deployment's event configuration in UUID order. limit defaults to 20, maximum 20; follow next as after. Event times are UTC with an IANA display timezone. Example: {\"limit\":20}.",
			pageFields,
			nil,
			[]string{schemaEvents},
		),
		administrativeRead(
			"event",
			"Read event configuration and version by id; registration rules include capacity, exact price_minor and currency_scale, closing instant and payment window. Example: {\"id\":\"00000000-0000-4000-8000-000000000001\"}.",
			map[string]any{"id": uuidField},
			[]string{"id"},
			[]string{"id", schemaVersion, "document"},
		),
		administrativeRead(
			"registrations",
			"List registrations and optional assigned passes for event_id in UUID order (not waitlist priority). Default limit 20, maximum 20; follow next as after. Use returned IDs and versions for assignment. Example: {\"event_id\":\"00000000-0000-4000-8000-000000000001\",\"limit\":20}.",
			queueFields,
			[]string{schemaEventID},
			[]string{"registrations"},
		),
		{
			"id":                "assign",
			manifestDescription: botDescription + " Assign an unpaid pass to a waitlisted registration only after an administrator request and exact-parameter approval. Required: event_id, registration_id, expected_version and expected_event_version from reads. Preparation shows recipient, exact price and local payment policy; reserves no capacity. Confirmation expires after at most five minutes. Execution rechecks versions, capacity, account and module availability. Price/deadline changes require new preparation and approval. No payment is recorded. Example: {\"event_id\":\"00000000-0000-4000-8000-000000000001\",\"registration_id\":\"00000000-0000-4000-8000-000000000002\",\"expected_version\":1,\"expected_event_version\":1}. Reconcile the same operation after uncertainty; missing status is unknown, never blindly replace it. To continue your own pending preparation from another interface, supply its original exact arguments and resume_proposal_id. Adoption preserves terms and expiry and still requires fresh exact approval. Read completed or expired proposals instead of adopting them.",
			manifestPermissions: []string{},
			manifestPath:        "/internal/agent/administration/assign",
			manifestEffect:      effectWrite,
			manifestTimeout:     toolTimeoutMillis,
			manifestMaxResult:   toolResultLimit,
			manifestInputSchema: map[string]any{
				manifestType:               schemaObject,
				schemaAdditionalProperties: false,
				schemaRequired: []string{
					schemaEventID,
					schemaRegistrationID,
					schemaExpectedVersion,
					"expected_event_version",
				},
				schemaProperties: assignFields,
			},
			manifestOutputSchema: registrationSchema(),
			manifestIdempotency:  true,
			manifestStatusPath:   "/internal/agent/administration/assign/status",
			manifestDedupWindow:  agentRegistrationDedupSeconds,
			manifestConfirmation: map[string]any{
				confirmationMode:    assignmentConfirmationMode,
				manifestPreparePath: "/internal/agent/administration/assign/prepare",
			},
			manifestMIMETypes:  []string{},
			manifestDisclosure: false,
		},
	}...)
}
