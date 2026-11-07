package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

type agentContextListArguments struct {
	Module string    `json:"module" minLength:"1" maxLength:"128"`
	After  uuid.UUID `json:"after,omitempty"`
	Limit  int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type agentContextChoiceArguments struct {
	Module          string    `json:"module" minLength:"1" maxLength:"128"`
	EventID         uuid.UUID `json:"event_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"0"`
}
type agentContextChoiceRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                         `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string                      `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string                      `json:"run_id,omitempty"`
		Arguments       agentContextChoiceArguments `json:"arguments"`
		IdempotencyKey  string                      `json:"idempotency_key" minLength:"1" maxLength:"128"`
		Confirmation    *struct {
			Mode string `json:"mode" enum:"none"`
		} `json:"confirmation,omitempty"`
	}
}
type agentContextResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *events.Context         `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}

func (s *Server) registerAgentEventContext(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-context-list",
			Method:       http.MethodPost,
			Path:         "/internal/agent/context/list",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentListContexts,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-context-resolve",
			Method:       http.MethodPost,
			Path:         "/internal/agent/context/resolve",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentResolveContext,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-context-choose",
			Method:       http.MethodPost,
			Path:         "/internal/agent/context/choose",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentChooseContext,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-context-status",
			Method:       http.MethodPost,
			Path:         "/internal/agent/context/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentContextStatus,
	)
}
func (s *Server) contextScope(ctx context.Context, auth AuthInput) (db.EventContextScope, error) {
	p, err := s.authorize(ctx, auth.Authorization, false)
	if err != nil {
		return db.EventContextScope{}, err
	}
	user, err := s.account(ctx, p)
	if err != nil {
		return db.EventContextScope{}, err
	}
	return db.EventContextScope{EventScope: s.eventScope(), UserID: user.ID, Subject: p.Subject}, nil
}
func contextRunID(value string) (string, error) {
	if len(value) > events.MaxContextRunIDBytes {
		return "", huma.Error400BadRequest("run ID exceeds 128 bytes")
	}
	return value, nil
}
func (s *Server) agentResolveContext(
	ctx context.Context,
	in *agentReadRequest[events.ContextRequest],
) (*agentReadResponse[events.Context], error) {
	scope, err := s.contextScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	runID, err := contextRunID(in.Body.RunID)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ResolveEventContext(ctx, scope, in.Body.Arguments, runID)
	if err != nil {
		return nil, contextError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
func (s *Server) agentChooseContext(ctx context.Context, in *agentContextChoiceRequest) (*agentContextResponse, error) {
	scope, err := s.contextScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("idempotency key must match operation ID")
	}
	runID, err := contextRunID(in.Body.RunID)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	result, err := s.store.ChooseEventContext(
		ctx,
		scope,
		in.Body.OperationID,
		db.EventContextChoice{
			Module:          args.Module,
			EventID:         args.EventID,
			ExpectedVersion: args.ExpectedVersion,
			RunID:           runID,
		},
	)
	out := &agentContextResponse{}
	out.Body.OperationID = in.Body.OperationID
	if err != nil {
		if errors.Is(err, events.ErrInvalid) || errors.Is(err, events.ErrContextUnavailable) ||
			errors.Is(err, db.ErrConflict) ||
			errors.Is(err, pgx.ErrNoRows) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "event_context_rejected",
				Message:        err.Error(),
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, contextError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}

func (s *Server) agentContextStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*agentContextResponse, error) {
	scope, err := s.contextScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.EventContextOperation(ctx, scope, in.Body.OperationID)
	out := &agentContextResponse{}
	out.Body.OperationID = in.Body.OperationID
	if errors.Is(err, pgx.ErrNoRows) {
		out.Body.Status = agentNotFound
		return out, nil
	}
	if err != nil {
		return nil, contextError(err)
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}
func contextError(err error) error {
	switch {
	case errors.Is(err, events.ErrInvalid):
		return huma.Error400BadRequest("invalid event context")
	case errors.Is(err, events.ErrContextUnavailable), errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("event context unavailable")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("event selection version or operation changed")
	default:
		return huma.Error503ServiceUnavailable("event context storage unavailable")
	}
}

const (
	eventContextResolveDescription = "Resolve an event for module before a user action. " +
		"Priority: explicit event_id, eligible run/user pin, eligible hint_id, ongoing event, nearest " +
		"upcoming start. Multiple ongoing events or equal nearest starts require clarification; " +
		"never choose by UUID. registrations accepts open configured events without a pass; " +
		"other modules require an assigned pass, including unpaid. " +
		"Event must enable module and be current/future. " +
		"Optional require_explicit disables hints/automatic selection; valid saved pins still qualify. " +
		"Output source, optional selected event summary and registration rules, bounded choices " +
		"(max20), more and selection_version; use list to page all qualified events. " +
		"No selection is saved. UTC instants plus IANA timezone; convert for display. " +
		"Example: {\"module\":\"orders\"}. Explicit inaccessible ID returns404, invalid hints/pins are " +
		"ignored. Context grants no action permission."

	eventContextListDescription = "List all current/future events qualified for module in stable UUID order. " +
		"registrations needs open rules; other participant modules need an assigned, even unpaid, " +
		"pass. Default limit20, max20; follow next as after. " +
		"Returns events with bounded metadata, timezone and optional registration rules. " +
		"Use this list when resolve.more is true or to choose another event. " +
		"Example: {\"module\":\"orders\",\"limit\":20}."

	eventContextSelectDescription = "Save a qualified event for this user and current run, after an explicit user " +
		"choice or a resolved unambiguous context for the requested action. Read resolve first; " +
		"send its selection_version as expected_version. Arguments module, event_id, expected_version. " +
		"No pass, booking or payment is created. Stable original receipt; " +
		"stale version/changed retry conflicts. Same run's saved choice survives a different run " +
		"changing the user's default. Business actions must still supply the explicit event ID and " +
		"recheck access. Example: {\"module\":\"orders\"," +
		"\"event_id\":\"00000000-0000-4000-8000-000000000001\",\"expected_version\":0}. " +
		"After uncertainty reconcile the same operation; " +
		"missing status is unknown and never permits blind replacement."
)

func eventContextModule() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	moduleField := map[string]any{
		manifestType:    schemaString,
		schemaMinLength: 1,
		schemaMaxLength: events.MaxModuleIDLength,
	}
	output := eventContextSchema()
	common := func(id, description, effect string, input map[string]any) map[string]any {
		return map[string]any{
			"id":                 id,
			manifestDescription:  description,
			manifestPermissions:  []string{},
			manifestPath:         "/internal/agent/context/" + id,
			manifestEffect:       effect,
			manifestTimeout:      toolTimeoutMillis,
			manifestMaxResult:    agentAdminResultLimit,
			manifestInputSchema:  input,
			manifestOutputSchema: output,
			manifestIdempotency:  false,
			manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
			manifestMIMETypes:    []string{},
			manifestDisclosure:   false,
		}
	}
	resolve := common(
		"resolve",
		eventContextResolveDescription,
		effectRead,
		map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaModule},
			schemaProperties: map[string]any{
				schemaModule:       moduleField,
				schemaEventID:      uuidField,
				"hint_id":          uuidField,
				"require_explicit": map[string]any{manifestType: schemaBoolean},
			},
		},
	)
	list := common(
		"list",
		eventContextListDescription,
		effectRead,
		map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaModule},
			schemaProperties: map[string]any{
				schemaModule: moduleField,
				schemaAfter:  uuidField,
				schemaLimit: map[string]any{
					manifestType:  schemaInteger,
					schemaMinimum: 1,
					schemaMaximum: events.ContextChoiceLimit,
				},
			},
		},
	)
	list[manifestOutputSchema] = map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{schemaEvents},
		schemaProperties: map[string]any{
			schemaEvents: map[string]any{
				manifestType: "array",
				"items":      contextEventSchema(),
				"maxItems":   events.ContextChoiceLimit,
			},
			"next": uuidField,
		},
	}
	choose := common(
		"choose",
		eventContextSelectDescription,
		effectWrite,
		map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             []string{schemaModule, schemaEventID, schemaExpectedVersion},
			schemaProperties: map[string]any{
				schemaModule:          moduleField,
				schemaEventID:         uuidField,
				schemaExpectedVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
			},
		},
	)
	choose[manifestIdempotency] = true
	choose[manifestStatusPath] = "/internal/agent/context/status"
	choose[manifestDedupWindow] = agentRegistrationDedupSeconds
	return map[string]any{
		"id":                "zinusya_context",
		manifestDescription: "User and run event context; suggestions are not permission grants",
		manifestPermissions: []string{},
		manifestDiscovery:   discoveryStatic,
		manifestMethods:     []any{resolve, list, choose},
	}
}

func (s *Server) agentListContexts(
	ctx context.Context,
	in *agentReadRequest[agentContextListArguments],
) (*agentReadResponse[events.ContextList], error) {
	scope, err := s.contextScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	args := in.Body.Arguments
	if args.Limit == 0 {
		args.Limit = events.ContextChoiceLimit
	}
	result, err := s.store.ListEventContexts(ctx, scope, args.Module, args.After, args.Limit)
	if err != nil {
		return nil, contextError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func contextEventSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	rules := map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired: []string{
			"leader_capacity",
			"follower_capacity",
			schemaPriceMinor,
			schemaCurrency,
			schemaCurrencyScale,
			"closes_at",
			"payment_window_seconds",
		},
		schemaProperties: map[string]any{
			"leader_capacity": map[string]any{
				manifestType:  schemaInteger,
				schemaMinimum: 0,
			},
			"follower_capacity": map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
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
			"closes_at":              instant,
			"payment_window_seconds": map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			"pricing":                eventTierPolicySchema(),
			"details_requirements":   eventDetailsRequirementsSchema(),
		},
	}
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{"id", schemaVersion, "name", schemaTimezone, "starts_at", "ends_at"},
		schemaProperties: map[string]any{
			"id":          uuidField,
			schemaVersion: map[string]any{manifestType: schemaInteger, schemaMinimum: 1},
			"name": map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: events.MaxNameLength,
			},
			"timezone": map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: events.MaxModuleIDLength,
			},
			"starts_at":    instant,
			"ends_at":      instant,
			"registration": rules,
			schemaDeposit:  depositPolicySchema(false),
		},
	}
}
func eventContextSchema() map[string]any {
	return map[string]any{
		manifestType:               schemaObject,
		schemaAdditionalProperties: false,
		schemaRequired:             []string{"source", schemaChoices, "more", "selection_version"},
		schemaProperties: map[string]any{
			"source": map[string]any{
				manifestType: schemaString,
				schemaEnum:   []string{"explicit", "pinned", "hint", "current", "nearest", "clarify", confirmationNone},
			},
			"selected": contextEventSchema(),
			schemaChoices: map[string]any{
				manifestType: "array",
				"items":      contextEventSchema(),
				"maxItems":   events.ContextChoiceLimit,
			},
			"more":              map[string]any{manifestType: schemaBoolean},
			"selection_version": map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
		},
	}
}
