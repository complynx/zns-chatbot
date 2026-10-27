package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const profileAPI = "/internal/agent/profile/"

type profileAPIReceipt struct {
	Status      string          `json:"status"`
	OperationID string          `json:"operation_id"`
	Result      *db.UserProfile `json:"result,omitempty"`
}

func profileRequest(t *testing.T, h *harness, token, method string, operation uuid.UUID, args any) (int, []byte) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	if method != "get" && method != "operation" {
		body["idempotency_key"] = operation
		body["confirmation"] = map[string]any{"mode": "none"}
	}
	return adminRequest(t, h, token, http.MethodPost, profileAPI+method, body)
}

func profileOutputSchemas(t *testing.T, h *harness, token string) map[string]*huma.Schema {
	t.Helper()
	code, raw := adminRequest(t, h, token, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID     string          `json:"id"`
				Output json.RawMessage `json:"output_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	schemas := make(map[string]*huma.Schema)
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_profile" {
			continue
		}
		for _, method := range module.Methods {
			var schema huma.Schema
			require.NoError(t, json.Unmarshal(method.Output, &schema))
			schema.PrecomputeMessages()
			schemas[method.ID] = &schema
		}
	}
	require.Len(t, schemas, 7)
	return schemas
}

func validProfileReceipt(t *testing.T, raw []byte, schema *huma.Schema) db.UserProfile {
	t.Helper()
	var receipt profileAPIReceipt
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "succeeded", receipt.Status, string(raw))
	require.NotNil(t, receipt.Result)
	var output struct {
		Result any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &output))
	require.NotNil(t, schema)
	validation := &huma.ValidateResult{}
	huma.Validate(huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer), schema,
		huma.NewPathBuffer(nil, 0), huma.ModeReadFromServer, output.Result, validation)
	require.Empty(t, validation.Errors, "Committed profile results must match Loom's advertised contract")
	return *receipt.Result
}

func TestProfileAgentToolsExposeValidOwnerResultsAndReconcileAfterRestart(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	userID, token := registrationUser(t, h, 18201, false)
	schemas := profileOutputSchemas(t, h, token)
	code, raw := profileRequest(t, h, token, "get", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile := validProfileReceipt(t, raw, schemas["get"])
	require.Equal(t, userID, profile.UserID)
	require.Zero(t, profile.Version)
	code, raw = profileRequest(
		t,
		h,
		token,
		"request_name",
		uuid.New(),
		map[string]any{"expected_version": profile.Version},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["request_name"])
	require.NotNil(t, profile.NameExpectation)
	expectation := profile.NameExpectation
	code, raw = profileRequest(t, h, token, "set_language", uuid.New(), map[string]any{
		"expected_version": profile.Version, "bot_language": "ru",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["set_language"])
	require.Equal(t, expectation, profile.NameExpectation)
	operation := uuid.New()
	complete := map[string]any{
		"expected_version": profile.Version,
		"expectation_id":   expectation.ID,
		"first_name":       "Мария",
		"last_name":        "Петрова",
	}
	code, raw = profileRequest(t, h, token, "complete_name", operation, complete)
	require.Equal(t, http.StatusOK, code, string(raw))
	completed := validProfileReceipt(t, raw, schemas["complete_name"])
	require.Equal(t, "Мария Петрова", completed.FullName)
	require.Equal(t, "Мария", completed.FirstName)
	require.Equal(t, "Петрова", completed.LastName)
	require.Equal(t, "user", completed.NameSource)
	require.Nil(t, completed.NameExpectation)
	h.stop(t)
	h.start(t)
	code, raw = adminRequest(t, h, token, http.MethodPost, profileAPI+"operation", map[string]any{
		"protocol_version": 1, "operation_id": operation,
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, completed, validProfileReceipt(t, raw, schemas["operation"]))
	code, raw = profileRequest(t, h, token, "complete_name", operation, complete)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, completed, validProfileReceipt(t, raw, schemas["complete_name"]))
	code, raw = profileRequest(
		t,
		h,
		token,
		"request_name",
		uuid.New(),
		map[string]any{"expected_version": completed.Version},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["request_name"])
	code, raw = profileRequest(t, h, token, "cancel_name", uuid.New(), map[string]any{
		"expected_version": profile.Version, "expectation_id": profile.NameExpectation.ID,
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["cancel_name"])
	require.Equal(t, completed.FullName, profile.FullName)
	require.Nil(t, profile.NameExpectation)
	code, raw = profileRequest(t, h, token, "set_name", uuid.New(), map[string]any{
		"expected_version": profile.Version, "first_name": "New Explicit", "last_name": "Name",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	profile = validProfileReceipt(t, raw, schemas["set_name"])
	require.Equal(t, "New Explicit Name", profile.FullName)
	code, raw = profileRequest(t, h, token, "operation", uuid.New(), map[string]any{"operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(
		t,
		completed,
		validProfileReceipt(t, raw, schemas["operation"]),
		"Explicit lookup returns the historical owner receipt",
	)
}

func TestProfileAgentRequiresSeparateNameComponents(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	userID, token := registrationUser(t, h, 18204, false)
	for _, args := range []map[string]any{
		{"expected_version": 0, "full_name": "Anna Name"},
		{"expected_version": 0, "first_name": "Anna"},
		{"expected_version": 0, "first_name": "Anna", "last_name": ""},
		{"expected_version": 0, "first_name": "Anna", "last_name": "Name", "full_name": "Anna Name"},
	} {
		code, raw := profileRequest(t, h, token, "set_name", uuid.New(), args)
		require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	}
	p, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, userID))
	require.NoError(t, err)
	require.Zero(t, p.Version)
	require.Empty(t, p.FullName)
}

func TestProfileAgentRejectsStaleWritesAndKeepsReceiptsPrivateEvenFromAdministrators(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	ownerID, owner := registrationUser(t, h, 18202, false)
	_, otherAdmin := registrationUser(t, h, 18203, true)
	operation := uuid.New()
	args := map[string]any{"expected_version": 0, "first_name": "Private Owner", "last_name": "Name"}
	code, raw := profileRequest(t, h, owner, "set_name", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "succeeded")
	code, raw = profileRequest(t, h, otherAdmin, "operation", uuid.New(), map[string]any{"operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "not_found")
	require.NotContains(t, string(raw), "Private Owner Name")
	code, raw = profileRequest(t, h, owner, "set_name", uuid.New(), map[string]any{
		"expected_version": 0, "first_name": "Stale", "last_name": "Name",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	args["first_name"] = "Changed Collision"
	code, raw = profileRequest(t, h, owner, "set_name", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	code, _ = adminRequest(t, h, owner, http.MethodPost, profileAPI+"set_name", map[string]any{
		"protocol_version": 1, "operation_id": uuid.New(), "idempotency_key": uuid.New(), "arguments": args,
	})
	require.Equal(t, http.StatusBadRequest, code)
	_, err := h.store.SetUserRevoked(ctx, ownerID, "profile-api-fixture")
	require.NoError(t, err)
	for _, method := range []string{"get", "operation", "set_name"} {
		requestArgs := map[string]any{}
		if method == "operation" {
			requestArgs["operation_id"] = operation
		}
		if method == "set_name" {
			requestArgs = args
		}
		code, _ = profileRequest(t, h, owner, method, operation, requestArgs)
		require.Equal(t, http.StatusForbidden, code, method)
	}
}
