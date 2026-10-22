package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

const (
	profilePath              = "/internal/agent/profile/"
	schemaFullName           = "full_name"
	schemaFirstName          = "first_name"
	schemaLastName           = "last_name"
	schemaBotLanguage        = "bot_language"
	profileNameMaxLength     = 256
	profileNamePartMaxLength = 200
)

type profileNameArguments struct {
	ExpectedVersion int64  `json:"expected_version" minimum:"0"`
	FirstName       string `json:"first_name"                   minLength:"1" maxLength:"200"`
	LastName        string `json:"last_name"                    minLength:"1" maxLength:"200"`
}

type profileLanguageArguments struct {
	ExpectedVersion int64  `json:"expected_version" minimum:"0"`
	BotLanguage     string `json:"bot_language"                 enum:"auto,en,ru"`
}

type profileRequestNameArguments struct {
	ExpectedVersion int64 `json:"expected_version" minimum:"0"`
}

type profileCompleteNameArguments struct {
	ExpectedVersion int64     `json:"expected_version" minimum:"0"`
	ExpectationID   uuid.UUID `json:"expectation_id"`
	FirstName       string    `json:"first_name"                   minLength:"1" maxLength:"200"`
	LastName        string    `json:"last_name"                    minLength:"1" maxLength:"200"`
}

type profileCancelNameArguments struct {
	ExpectedVersion int64     `json:"expected_version" minimum:"0"`
	ExpectationID   uuid.UUID `json:"expectation_id"`
}

type profileWriteRequest[T any] struct {
	AuthInput

	Body struct {
		ProtocolVersion int    `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string `json:"operation_id" format:"uuid"`
		RunID           string `json:"run_id,omitempty"`
		Arguments       T      `json:"arguments"`
		IdempotencyKey  string `json:"idempotency_key" format:"uuid"`
		Confirmation    *struct {
			Mode string `json:"mode" enum:"none"`
		} `json:"confirmation,omitempty"`
	}
}

type profileResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *db.UserProfile         `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}

type profileOperationRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int    `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string `json:"run_id,omitempty"`
		Arguments       *struct {
			OperationID uuid.UUID `json:"operation_id"`
		} `json:"arguments,omitempty"`
	}
}

func (s *Server) registerProfile(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "agent-profile-get", Method: http.MethodPost,
		Path: profilePath + "get", MaxBodyBytes: internalBodyLimit}, s.agentProfile)
	huma.Register(api, huma.Operation{OperationID: "agent-profile-operation", Method: http.MethodPost,
		Path: profilePath + "operation", MaxBodyBytes: internalBodyLimit}, s.agentProfileOperation)
	registerProfileMutation(s, api, "set_name", func(a profileNameArguments) db.ProfileMutation {
		return db.ProfileMutation{ExpectedVersion: a.ExpectedVersion, FirstName: a.FirstName, LastName: a.LastName}
	})
	registerProfileMutation(s, api, "set_language", func(a profileLanguageArguments) db.ProfileMutation {
		return db.ProfileMutation{ExpectedVersion: a.ExpectedVersion, BotLanguage: a.BotLanguage}
	})
	registerProfileMutation(s, api, "request_name", func(a profileRequestNameArguments) db.ProfileMutation {
		return db.ProfileMutation{ExpectedVersion: a.ExpectedVersion}
	})
	registerProfileMutation(s, api, "complete_name", func(a profileCompleteNameArguments) db.ProfileMutation {
		return db.ProfileMutation{
			ExpectedVersion: a.ExpectedVersion,
			ExpectationID:   a.ExpectationID,
			FirstName:       a.FirstName,
			LastName:        a.LastName,
		}
	})
	registerProfileMutation(s, api, "cancel_name", func(a profileCancelNameArguments) db.ProfileMutation {
		return db.ProfileMutation{ExpectedVersion: a.ExpectedVersion, ExpectationID: a.ExpectationID}
	})
}

// Each tool accepts only its own arguments; all mutations share the owner and receipt checks.
func registerProfileMutation[T any](s *Server, api huma.API, kind string, mutation func(T) db.ProfileMutation) {
	huma.Register(api, huma.Operation{OperationID: "agent-profile-" + kind, Method: http.MethodPost,
		Path: profilePath + kind, MaxBodyBytes: internalBodyLimit},
		func(ctx context.Context, in *profileWriteRequest[T]) (*profileResponse, error) {
			scope, err := s.profileScope(ctx, in.AuthInput)
			if err != nil {
				return nil, err
			}
			op, err := uuid.Parse(in.Body.OperationID)
			if err != nil || op == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
				return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
			}
			change := mutation(in.Body.Arguments)
			change.OperationID, change.Kind = op, kind
			result, err := s.store.MutateProfile(ctx, scope, change)
			return profileWriteResult(in.Body.OperationID, result, err)
		})
}

func (s *Server) profileScope(ctx context.Context, auth AuthInput) (db.ProfileScope, error) {
	p, err := s.authorize(ctx, auth.Authorization, false)
	if err != nil {
		return db.ProfileScope{}, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return db.ProfileScope{}, err
	}
	return db.ProfileScope{BotID: s.options.BotID, Organization: s.options.OrganizationID,
		Subject: p.Subject, UserID: u.ID}, nil
}

func (s *Server) agentProfile(
	ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[db.UserProfile], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.GetProfile(ctx, scope)
	if err != nil {
		return nil, profileError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentProfileOperation(ctx context.Context, in *profileOperationRequest) (*profileResponse, error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ProfileOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &profileResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return profileWriteResult(in.Body.OperationID, result, err)
}

// Owner tools share the status-path envelope and explicit operation lookup protocol.
func (s *Server) ownerOperation(ctx context.Context, in *profileOperationRequest) (db.ProfileScope, uuid.UUID, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return scope, uuid.Nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if in.Body.Arguments != nil {
		op, err = in.Body.Arguments.OperationID, nil
	}
	if err != nil || op == uuid.Nil {
		return scope, uuid.Nil, huma.Error400BadRequest("operation ID must be a nonzero UUID")
	}
	return scope, op, nil
}

func profileWriteResult(operation string, result db.UserProfile, err error) (*profileResponse, error) {
	out := &profileResponse{}
	out.Body.OperationID = operation
	if err != nil {
		if errors.Is(err, db.ErrInvalidProfile) || errors.Is(err, db.ErrConflict) || errors.Is(err, pgx.ErrNoRows) {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{
				Code:           "profile_rejected",
				Message:        err.Error(),
				Classification: agentTerminalRejection,
			}
			return out, nil
		}
		return nil, profileError(err)
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func profileError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return huma.Error404NotFound("profile unavailable")
	}
	return huma.Error503ServiceUnavailable("profile storage unavailable")
}

func profileModule() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	version := map[string]any{manifestType: schemaInteger, schemaMinimum: 0}
	name := map[string]any{manifestType: schemaString, schemaMinLength: 1, schemaMaxLength: profileNamePartMaxLength}
	language := map[string]any{manifestType: schemaString, schemaEnum: []string{"auto", "en", "ru"}}
	input := func(fields map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
			schemaProperties: fields, schemaRequired: required}
	}
	method := func(id, description string, args map[string]any, write bool) map[string]any {
		m := map[string]any{"id": id, manifestDescription: botDescription + " " + description,
			manifestPermissions: []string{}, manifestPath: profilePath + id, manifestEffect: effectRead,
			manifestTimeout: toolTimeoutMillis, manifestMaxResult: toolResultLimit,
			manifestInputSchema: args, manifestOutputSchema: profileSchema(), manifestIdempotency: write,
			manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
			manifestMIMETypes:    []string{}, manifestDisclosure: false}
		if write {
			m[manifestEffect], m[manifestStatusPath], m[manifestDedupWindow] = effectWrite, profilePath+"operation", agentRegistrationDedupSeconds
		}
		return m
	}
	get := method(
		"get",
		"Read your independent full name, bot language, profile version and pending name request. Telegram metadata never overwrites these fields. Name is not legal identity. Expired name requests do not affect registration or payment state.",
		input(map[string]any{}),
		false,
	)
	setName := method(
		"set_name",
		"Save the user's explicitly supplied or confirmed real first_name and last_name separately; components may contain spaces. Do not guess a split by word position: ask when ambiguous. Read get first and send expected_version. This finishes any pending name request and schedules name-only IAM synchronization; it does not prove IAM has updated or change legal registration details. Never copy Telegram hints without the user's confirmation.",
		input(
			map[string]any{schemaExpectedVersion: version, schemaFirstName: name, schemaLastName: name},
			schemaExpectedVersion,
			schemaFirstName,
			schemaLastName,
		),
		true,
	)
	setLanguage := method(
		"set_language",
		"Save the user's explicit interface language choice: en, ru, or auto for Telegram fallback. Agent replies still follow meaningful current user text; neutral input uses conversation then interface language. This preserves a pending name request.",
		input(
			map[string]any{schemaExpectedVersion: version, schemaBotLanguage: language},
			schemaExpectedVersion,
			schemaBotLanguage,
		),
		true,
	)
	request := method(
		"request_name",
		"Record a bounded request for the user's full name, then ask conversationally. Existing active request and deadline are preserved. No message is captured automatically; unrelated conversation is allowed.",
		input(map[string]any{schemaExpectedVersion: version}, schemaExpectedVersion),
		true,
	)
	complete := method(
		"complete_name",
		"After explicit user input or confirmation, save separate real first_name and last_name and finish the matching unexpired name request. Components may contain spaces; clarify an ambiguous split. Requires get's version and expectation ID. Schedules name-only IAM synchronization, not legal registration details. A local receipt does not prove remote IAM completion. Stale or expired requests conflict; reread and follow current user intent.",
		input(
			map[string]any{
				schemaExpectedVersion: version,
				"expectation_id":      uuidField,
				schemaFirstName:       name,
				schemaLastName:        name,
			},
			schemaExpectedVersion,
			"expectation_id",
			schemaFirstName,
			schemaLastName,
		),
		true,
	)
	cancel := method(
		"cancel_name",
		"Cancel only the matching pending name request after user intent; no business object changes. Requires current profile version and expectation ID.",
		input(
			map[string]any{schemaExpectedVersion: version, "expectation_id": uuidField},
			schemaExpectedVersion,
			"expectation_id",
		),
		true,
	)
	operation := method(
		"operation",
		"Reconcile the original immutable result of a profile write using arguments.operation_id. The status endpoint also accepts protocol_version and the original operation_id in the envelope without arguments. Missing status is unknown; retain the same operation ID and arguments after uncertainty.",
		input(map[string]any{"operation_id": uuidField}, "operation_id"),
		false,
	)
	return map[string]any{
		"id":                "zinusya_profile",
		manifestDescription: "Owner profile and bounded durable full-name request",
		manifestPermissions: []string{},
		manifestDiscovery:   discoveryStatic,
		manifestMethods:     []any{get, setName, setLanguage, request, complete, cancel, operation},
	}
}

func profileSchema() map[string]any {
	instant := map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime}
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{schemaUserID, schemaVersion, schemaFullName, schemaBotLanguage, schemaObservedAt},
		schemaProperties: map[string]any{
			schemaUserID:      uuidField,
			schemaVersion:     map[string]any{manifestType: schemaInteger, schemaMinimum: 0},
			schemaFullName:    map[string]any{manifestType: schemaString, schemaMaxLength: profileNameMaxLength},
			"first_name":      map[string]any{manifestType: schemaString, schemaMaxLength: profileNamePartMaxLength},
			"last_name":       map[string]any{manifestType: schemaString, schemaMaxLength: profileNamePartMaxLength},
			"name_source":     map[string]any{manifestType: schemaString, schemaEnum: []string{"user", "iam"}},
			schemaBotLanguage: map[string]any{manifestType: schemaString, schemaEnum: []string{"auto", "en", "ru"}},
			schemaObservedAt:  instant,
			"name_expectation": map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
				schemaRequired:   []string{"id", schemaCreatedAt, schemaExpiresAt},
				schemaProperties: map[string]any{"id": uuidField, schemaCreatedAt: instant, schemaExpiresAt: instant}},
		}}
}
