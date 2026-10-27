package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const (
	agentJSONIntegerMax           = 1<<53 - 1
	agentRegistrationDedupSeconds = 86400
	agentCurrencyScaleMax         = 4
	agentTierNumberMax            = 100
)

type agentRegistrationArguments struct {
	EventID uuid.UUID `json:"event_id"`
}
type agentJoinArguments struct {
	EventID uuid.UUID `json:"event_id"`
	Role    string    `json:"role" enum:"leader,follower"`
}
type agentJoinRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string             `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string             `json:"run_id,omitempty"`
		Arguments       agentJoinArguments `json:"arguments"`
		IdempotencyKey  string             `json:"idempotency_key" minLength:"1" maxLength:"128"`
		Confirmation    *struct {
			Mode string `json:"mode" enum:"none"`
		} `json:"confirmation,omitempty"`
	}
}
type agentRegistrationStatusRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int    `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string `json:"operation_id" minLength:"1" maxLength:"128"`
	}
}
type agentRegistrationError struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Classification string `json:"classification"`
}

type agentRegistrationStatusResponse struct {
	Body struct {
		Status      string                      `json:"status"`
		OperationID string                      `json:"operation_id"`
		Result      *registrations.Registration `json:"result,omitempty"`
		Error       *agentRegistrationError     `json:"error,omitempty"`
	}
}

func (s *Server) registerAgentRegistrations(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "agent-registration-list", Method: http.MethodPost,
		Path: "/internal/agent/registrations/list", MaxBodyBytes: internalBodyLimit}, s.agentAccountEvents)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-registration-me",
			Method:       http.MethodPost,
			Path:         "/internal/agent/registrations/me",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentRegistration,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-registration-join",
			Method:       http.MethodPost,
			Path:         "/internal/agent/registrations/join",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentJoin,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-registration-status",
			Method:       http.MethodPost,
			Path:         "/internal/agent/registrations/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentRegistrationStatus,
	)
}

func (s *Server) agentRegistration(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[registrations.Registration], error) {
	result, err := s.readRegistration(ctx, &eventRequest{AuthInput: in.AuthInput, ID: in.Body.Arguments.EventID})
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result.Body), nil
}

func (s *Server) agentJoin(
	ctx context.Context,
	in *agentJoinRequest,
) (*agentRegistrationStatusResponse, error) {
	if in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("idempotency key must match operation ID")
	}
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	user, err := s.account(ctx, p)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.ChangeRegistration(
		ctx,
		s.eventScope(),
		p.Subject,
		in.Body.OperationID,
		db.RegistrationChange{Kind: "register", EventID: a.EventID, UserID: user.ID, Role: a.Role},
	)
	return agentRegistrationWriteResult(in.Body.OperationID, result, err)
}

func agentRegistrationWriteResult(
	operation string,
	result registrations.Registration,
	err error,
) (*agentRegistrationStatusResponse, error) {
	if err != nil {
		if errors.Is(err, registrationdetails.ErrRequired) || errors.Is(err, db.ErrConflict) ||
			errors.Is(err, registrations.ErrInvalid) ||
			errors.Is(err, registrations.ErrCapacity) ||
			errors.Is(err, registrations.ErrUnavailable) ||
			errors.Is(err, pgx.ErrNoRows) {
			out := &agentRegistrationStatusResponse{}
			out.Body.Status = agentRejected
			out.Body.OperationID = operation
			out.Body.Error = &agentRegistrationError{
				Code:           "registration_rejected",
				Message:        err.Error(),
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, registrationError(err)
	}
	out := &agentRegistrationStatusResponse{}
	out.Body.Status = agentSucceeded
	out.Body.OperationID = operation
	out.Body.Result = &result
	return out, nil
}

func (s *Server) agentRegistrationStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*agentRegistrationStatusResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	result, err := s.store.RegistrationJoinOperation(ctx, s.eventScope(), p.Subject, in.Body.OperationID)
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

func registrationMethods() []any {
	eventProperty := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	output := registrationSchema()
	read := map[string]any{
		"id": "me",
		manifestDescription: "Read your own registration and assigned pass for event_id. UTC instants; " +
			"display in event timezone. 404 means no accessible registration. " +
			"Example: {\"event_id\":\"00000000-0000-4000-8000-000000000001\"}.",
		manifestPermissions: []string{},
		manifestPath:        "/internal/agent/registrations/me",
		manifestEffect:      effectRead,
		manifestTimeout:     toolTimeoutMillis,
		manifestMaxResult:   toolResultLimit,
		manifestInputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaEventID},
			schemaProperties:           map[string]any{schemaEventID: eventProperty},
		},
		manifestOutputSchema: output,
		manifestIdempotency:  false,
		manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
		manifestMIMETypes:    []string{},
		manifestDisclosure:   false,
	}
	join := map[string]any{
		"id": "join",
		manifestDescription: "Request your solo waitlist registration for event_id and role (leader or " +
			"follower), only after a user request. No pass or payment is implied. " +
			"Sales must be open and registrations enabled. " +
			"Repeating the same role preserves original request time; role changes conflict. " +
			"Example: {\"event_id\":\"00000000-0000-4000-8000-000000000001\",\"role\":\"leader\"}. " +
			"Receipt survives restart; after uncertainty reconcile the same operation ID. " +
			"A missing status is unknown and does not prove no effect; " +
			"never create a replacement operation blindly.",
		manifestPermissions: []string{},
		manifestPath:        "/internal/agent/registrations/join",
		manifestEffect:      effectWrite,
		manifestTimeout:     toolTimeoutMillis,
		manifestMaxResult:   toolResultLimit,
		manifestInputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaEventID, schemaRole},
			schemaProperties: map[string]any{
				schemaEventID: eventProperty,
				schemaRole: map[string]any{
					manifestType: schemaString,
					schemaEnum:   []string{registrations.Leader, registrations.Follower},
				},
			},
		},
		manifestOutputSchema: output,
		manifestIdempotency:  true,
		manifestStatusPath:   "/internal/agent/registrations/status",
		manifestDedupWindow:  agentRegistrationDedupSeconds,
		manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
		manifestMIMETypes:    []string{},
		manifestDisclosure:   false,
	}
	list := accountEventsMethod()
	list[manifestPath] = "/internal/agent/registrations/list"
	return []any{read, list, join, pairInvitationMethod()}
}
func registrationSchema() map[string]any {
	uuidProperty := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	pass := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			"id",
			schemaRegistrationID,
			schemaPriceMinor,
			schemaCurrency,
			schemaCurrencyScale,
			"assigned_at",
			"payment_due_at",
		},
		schemaProperties: map[string]any{
			"id":                 uuidProperty,
			schemaRegistrationID: uuidProperty,
			schemaPriceMinor: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: agentJSONIntegerMax,
			},
			schemaCurrency: map[string]any{manifestType: schemaString, schemaPattern: schemaCurrencyPattern},
			schemaCurrencyScale: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
				schemaMaximum: agentCurrencyScaleMax,
			},
			schemaDeposit: depositPolicySchema(true),
			"tier": map[string]any{
				manifestType: schemaObject, schemaAdditionalProperties: false,
				schemaRequired: []string{"id", "number"},
				schemaProperties: map[string]any{
					"id": map[string]any{manifestType: schemaString, schemaPattern: "^[a-z0-9][a-z0-9_-]{0,63}$"},
					"number": map[string]any{
						manifestType:  schemaInteger,
						schemaMinimum: 1,
						schemaMaximum: agentTierNumberMax,
					},
				},
			},
			"assigned_at":    instant,
			"payment_due_at": instant,
		},
	}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			"id",
			schemaEventID,
			schemaUserID,
			schemaRole,
			schemaState,
			schemaVersion,
			"requested_at",
		},
		schemaProperties: map[string]any{
			"id":          uuidProperty,
			schemaEventID: uuidProperty,
			schemaUserID:  uuidProperty,
			schemaRole: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{registrations.Leader, registrations.Follower},
			},
			schemaState: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{registrations.Waitlisted, registrations.Assigned},
			},
			schemaVersion:  map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			"requested_at": instant,
			"pass":         pass,
		},
	}
}
