package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const (
	agentStateLength      = 64
	agentTaskPageLimit    = 20
	agentAdminResultLimit = 65536
)

func registerAgentPost[I, O any](
	api huma.API, id, path string, bodyLimit int64, handler func(context.Context, *I) (*O, error),
) {
	huma.Register(
		api,
		huma.Operation{OperationID: id, Method: http.MethodPost, Path: path, MaxBodyBytes: bodyLimit},
		handler,
	)
}

func requireExactOperation(operation, idempotency, proposal uuid.UUID) error {
	if operation == uuid.Nil || idempotency != operation || proposal == uuid.Nil {
		return huma.Error400BadRequest("matching operation, idempotency key and proposal required")
	}
	return nil
}

func (s *Server) adminScope(ctx context.Context, in AuthInput) (db.AdminScope, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return db.AdminScope{}, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return db.AdminScope{}, err
	}
	return db.AdminScope{
		BotID:        s.options.BotID,
		Organization: s.options.OrganizationID,
		Subject:      p.Subject,
		UserID:       u.ID,
		GlobalAdmin:  true,
	}, nil
}

// Agent reads use Loom's envelope but share the operator API's authorization and scoping.
type agentReadRequest[T any] struct {
	AuthInput

	Body struct {
		ProtocolVersion int    `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     string `json:"operation_id" minLength:"1" maxLength:"128"`
		RunID           string `json:"run_id,omitempty"`
		Arguments       T      `json:"arguments"`
	}
}

type agentReadResponse[T any] struct {
	Body struct {
		Status      string `json:"status"`
		OperationID string `json:"operation_id"`
		Result      T      `json:"result"`
	}
}

type agentTaskListArguments struct {
	State string    `json:"state,omitempty" maxLength:"64"`
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty"                minimum:"1" maximum:"20"`
}

type agentTaskArguments struct {
	ID uuid.UUID `json:"id"`
}

type agentTaskListResult struct {
	Tasks []db.TaskSummary `json:"tasks"`
	Next  *uuid.UUID       `json:"next,omitempty"`
}

func (s *Server) registerAgentAdministration(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "agent-admin-discover", Method: http.MethodPost,
		Path: "/internal/agent/administration/discover", MaxBodyBytes: internalBodyLimit}, s.discoverAdministration)
	huma.Register(api, huma.Operation{OperationID: "agent-admin-settings", Method: http.MethodPost,
		Path: "/internal/agent/administration/settings", MaxBodyBytes: internalBodyLimit}, s.agentSettings)
	huma.Register(api, huma.Operation{OperationID: "agent-admin-tasks", Method: http.MethodPost,
		Path: "/internal/agent/administration/tasks", MaxBodyBytes: internalBodyLimit}, s.agentTasks)
	huma.Register(api, huma.Operation{OperationID: "agent-admin-task", Method: http.MethodPost,
		Path: "/internal/agent/administration/task", MaxBodyBytes: internalBodyLimit}, s.agentTask)
}

type agentDiscoveryResponse struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func (s *Server) discoverAdministration(ctx context.Context, in *AuthInput) (*agentDiscoveryResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, false)
	if err != nil {
		return nil, err
	}
	methods := []map[string]any{}
	if _, ok := p.Roles["zinusya.admin"][s.options.OrganizationID]; ok {
		methods = administrativeMethods()
	}
	return discoveryResult(methods)
}

func discoveryResult(methods []map[string]any) (*agentDiscoveryResponse, error) {
	// Loom rejects extra fields in discovery; raw JSON avoids Huma's schema-link transformer.
	body, err := json.Marshal(struct {
		Methods []map[string]any `json:"methods"`
	}{methods})
	if err != nil {
		return nil, huma.Error500InternalServerError("discovery unavailable")
	}
	return &agentDiscoveryResponse{ContentType: contentTypeJSON, Body: body}, nil
}

func (s *Server) agentSettings(
	ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[db.VersionedSettings], error) {
	result, err := s.getSettings(ctx, &in.AuthInput)
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result.Body), nil
}

func (s *Server) agentTasks(
	ctx context.Context,
	in *agentReadRequest[agentTaskListArguments],
) (*agentReadResponse[agentTaskListResult], error) {
	a := in.Body.Arguments
	if a.Limit == 0 {
		a.Limit = agentTaskPageLimit
	}
	result, err := s.adminListTasks(
		ctx,
		&adminTaskListRequest{AuthInput: in.AuthInput, State: a.State, After: a.After, Limit: a.Limit},
	)
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, agentTaskListResult{result.Body.Tasks, result.Body.Next}), nil
}

func (s *Server) agentTask(
	ctx context.Context,
	in *agentReadRequest[agentTaskArguments],
) (*agentReadResponse[db.TaskDiagnostics], error) {
	result, err := s.adminTask(ctx, &taskRequest{AuthInput: in.AuthInput, ID: in.Body.Arguments.ID})
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result.Body), nil
}

func agentReadResult[T any](operation string, result T) *agentReadResponse[T] {
	out := &agentReadResponse[T]{}
	out.Body.Status, out.Body.OperationID, out.Body.Result = agentSucceeded, operation, result
	return out
}

func administrativeMethods() []map[string]any {
	methods := append(
		append(
			append(append(assignmentMethods(), authorityMethods()...), notificationMethods()...),
			userRevocationMethods()...),
		[]map[string]any{
			taskClosureMethod(),
			settingsChangeMethod(),
			eventChangeMethod(),
			ordersCatalogAdminReadMethod(),
			ordersCatalogWriteMethod(),
			mealCatalogAdminReadMethod(),
			mealCatalogWriteMethod(),
			massageScheduleAdminReadMethod(),
			massageAdministrationReadMethod(),
			massageScheduleWriteMethod(),
			archiveValidationMethod(),
			storedArchiveValidationMethod(),
			archiveStorageMethod(),
			historyApplyMethod(),
			administrativeRead(
				"settings",
				"Read versioned settings; no secrets are returned. Worker concurrency app"+
					"lies after restart. Local question timeout is reserved; Loom controls cu"+
					"rrent prompt expiry.",
				map[string]any{},
				nil,
				[]string{schemaVersion, "document"},
			),
			administrativeRead(
				"tasks",
				"List task metadata for this bot and organization in stable UUID order. Follow next with after.",
				map[string]any{
					schemaState: map[string]any{manifestType: schemaString, schemaMaxLength: agentStateLength},
					schemaAfter: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
					schemaLimit: map[string]any{
						"type":        schemaInteger,
						schemaMinimum: 1,
						schemaMaximum: agentTaskPageLimit,
					},
				},
				nil,
				[]string{"tasks"},
			),
			administrativeRead(
				"task",
				"Inspect task state, queue, deliveries and operator decisions. Message content and credentials are excluded.",
				map[string]any{"id": map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}},
				[]string{"id"},
				[]string{"task", "deliveries", "queue_jobs", "resolutions"},
			),
		}...)
	methods = append(methods, avatarDeliveryAdministrativeMethods()...)
	return append(methods, massageBindingReadMethods()...)
}

func administrativeRead(
	id, description string,
	properties map[string]any,
	required, resultFields []string,
) map[string]any {
	input := map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false, schemaProperties: properties}
	if len(required) > 0 {
		input[schemaRequired] = required
	}
	return map[string]any{
		"id":                 id,
		"description":        description,
		"permissions":        []string{},
		manifestPath:         "/internal/agent/administration/" + id,
		manifestEffect:       effectRead,
		manifestTimeout:      toolTimeoutMillis,
		manifestMaxResult:    agentAdminResultLimit,
		manifestInputSchema:  input,
		manifestOutputSchema: map[string]any{manifestType: schemaObject, schemaRequired: resultFields},
		manifestIdempotency:  false,
		manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
		manifestMIMETypes:    []string{},
		manifestDisclosure:   false,
	}
}
