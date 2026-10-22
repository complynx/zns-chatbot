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
)

type authorityListArguments struct {
	EventID uuid.UUID `json:"event_id"`
	After   uuid.UUID `json:"after,omitempty"`
	Limit   int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}
type authorityListResult struct {
	Authorities []db.EventAuthority `json:"authorities"`
	Next        *uuid.UUID          `json:"next,omitempty"`
}

// A named type keeps this exact approval schema distinct from optional no-approval requests.
type authorityConfirmation struct {
	Mode          string                  `json:"mode"          enum:"exact_parameters"`
	Preconditions assignmentPreconditions `json:"preconditions"`
}

type agentAuthorityRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                   `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string                `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string                `json:"run_id,omitempty"`
		Arguments       db.AuthorityChange    `json:"arguments"`
		IdempotencyKey  string                `json:"idempotency_key" minLength:"1" maxLength:"128"`
		Confirmation    authorityConfirmation `json:"confirmation"`
	}
}
type authorityPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.AuthorityChange      `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}
type authorityWriteResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *db.EventAuthority      `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}

func (s *Server) registerAuthorities(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-own-authority",
			Method:       http.MethodPost,
			Path:         "/internal/agent/authorities/me",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentOwnAuthority,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-authorities",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/authorities",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentAuthorities,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-prepare-authority",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/authority/prepare",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentPrepareAuthority,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-change-authority",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/authority",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentChangeAuthority,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-authority-status",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/authority/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentAuthorityStatus,
	)
}

func (s *Server) agentOwnAuthority(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.EventAuthority], error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return nil, err
	}
	result, err := s.store.Authority(ctx, s.eventScope(), in.Body.Arguments.EventID, u.ID)
	if err != nil {
		return nil, authorityError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentAuthorities(
	ctx context.Context,
	in *agentReadRequest[authorityListArguments],
) (*agentReadResponse[authorityListResult], error) {
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	if a.Limit == 0 {
		a.Limit = agentTaskPageLimit
	}
	rows, err := s.store.Authorities(ctx, s.eventScope(), a.EventID, a.After, a.Limit)
	if err != nil {
		return nil, authorityError(err)
	}
	result := authorityListResult{Authorities: rows}
	if len(rows) == int(a.Limit) {
		result.Next = &rows[len(rows)-1].UserID
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentPrepareAuthority(
	ctx context.Context,
	in *agentReadRequest[db.AuthorityChange],
) (*authorityPreparationResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	prep, err := s.store.PrepareAuthority(ctx, s.eventScope(), p.Subject, in.Body.OperationID, in.Body.Arguments)
	if err != nil {
		return nil, authorityError(err)
	}
	out := &authorityPreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prep.Arguments
	out.Body.Preconditions.ProposalID = prep.ID
	out.Body.Description, out.Body.ExpiresAt = prep.Description, prep.ExpiresAt
	return out, nil
}
func (s *Server) agentChangeAuthority(ctx context.Context, in *agentAuthorityRequest) (*authorityWriteResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	if in.Body.IdempotencyKey != in.Body.OperationID || in.Body.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching idempotency key and proposal required")
	}
	result, err := s.store.ChangeAuthority(
		ctx,
		s.eventScope(),
		p.Subject,
		in.Body.OperationID,
		in.Body.Arguments,
		in.Body.Confirmation.Preconditions.ProposalID,
	)
	out := &authorityWriteResponse{}
	out.Body.OperationID = in.Body.OperationID
	switch {
	case err == nil:
		out.Body.Status = agentSucceeded
		out.Body.Result = &result
	case errors.Is(err, db.ErrConflict):
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Classification: agentTerminalRejection,
			Code:           "conflict",
			Message:        "authority version, proposal or operation changed",
		}
	case errors.Is(err, pgx.ErrNoRows):
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Classification: agentTerminalRejection,
			Code:           agentNotFound,
			Message:        "event, account or proposal not found",
		}
	case errors.Is(err, db.ErrInvalidAuthority):
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Classification: agentTerminalRejection,
			Code:           "invalid",
			Message:        "invalid event authority",
		}
	default:
		return nil, authorityError(err)
	}
	return out, nil
}

func (s *Server) agentAuthorityStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*authorityWriteResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	result, err := s.store.AuthorityOperation(ctx, s.eventScope(), p.Subject, in.Body.OperationID)
	out := &authorityWriteResponse{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, authorityError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}
func authorityError(err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("event authority, account or operation not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("authority version, proposal or operation changed")
	case errors.Is(err, db.ErrInvalidAuthority):
		return huma.Error400BadRequest("invalid event authority")
	default:
		return huma.Error503ServiceUnavailable("event authority storage unavailable")
	}
}
func authoritySchema() map[string]any {
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{schemaEventID, schemaUserID, schemaRole, schemaVersion},
		schemaProperties: map[string]any{
			schemaEventID: map[string]any{
				manifestType: schemaString,
				schemaFormat: schemaUUID,
			},
			schemaUserID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaRole: map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{db.AuthorityNone, db.AuthorityAmbassador, db.AuthorityAdministrator},
			},
			schemaVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
		},
	}
}
func authorityModule() map[string]any {
	method := administrativeRead(
		"me",
		"Read your own event authority by event_id. An assigned pass is not an ambassador role. none means no event authority. This grants no general access to other users' files.",
		map[string]any{schemaEventID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}},
		[]string{schemaEventID},
		nil,
	)
	method[manifestPath] = "/internal/agent/authorities/me"
	method[manifestOutputSchema] = authoritySchema()
	return map[string]any{
		"id":                "zinusya_authorities",
		manifestDescription: "Your event-scoped ambassador or administrator role",
		manifestPermissions: []string{},
		manifestDiscovery:   discoveryStatic,
		manifestMethods:     []any{method},
	}
}
func authorityMethods() []map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	read := administrativeRead(
		"authorities",
		"List event-local roles in user UUID order, including revoked roles (none). Only active local accounts are listed. Follow next as after; default/maximum limit 20.",
		map[string]any{
			schemaEventID: uuidField,
			schemaAfter:   uuidField,
			schemaLimit: map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 1,
				schemaMaximum: agentTaskPageLimit,
			},
		},
		[]string{schemaEventID},
		[]string{"authorities"},
	)
	write := map[string]any{
		"id":                "authority",
		manifestDescription: "Grant or revoke a local event authority only on a global administrator request with exact-parameter approval. Required event_id, local user_id, role (none/ambassador/administrator), expected_version (0 before first assignment). Get current versions from authorities. No global IAM role is granted. Preparation reserves nothing and expires in five minutes. An event ambassador cannot grant roles. Reconcile the same operation after uncertainty; missing status is unknown.",
		manifestPermissions: []string{},
		manifestPath:        "/internal/agent/administration/authority",
		manifestEffect:      effectWrite,
		manifestTimeout:     toolTimeoutMillis,
		manifestMaxResult:   toolResultLimit,
		manifestInputSchema: map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaEventID, schemaUserID, schemaRole, schemaExpectedVersion},
			schemaProperties: map[string]any{
				schemaEventID: uuidField,
				schemaUserID:  uuidField,
				schemaRole: map[string]any{
					manifestType: schemaString,
					schemaEnum:   []string{db.AuthorityNone, db.AuthorityAmbassador, db.AuthorityAdministrator},
				},
				schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
			},
		},
		manifestOutputSchema: authoritySchema(),
		manifestIdempotency:  true,
		manifestStatusPath:   "/internal/agent/administration/authority/status",
		manifestDedupWindow:  agentRegistrationDedupSeconds,
		manifestConfirmation: map[string]any{
			confirmationMode:    assignmentConfirmationMode,
			manifestPreparePath: "/internal/agent/administration/authority/prepare",
		},
		manifestMIMETypes:  []string{},
		manifestDisclosure: false,
	}
	return []map[string]any{read, write}
}
