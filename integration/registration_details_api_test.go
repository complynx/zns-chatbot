package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

const detailsAPI = "/internal/agent/registration_details/"

func detailsRequest(t *testing.T, h *harness, token, method string, operation uuid.UUID, args any) (int, []byte) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	if method == "set_legal_name" || method == "set_passport_number" || method == "begin_request" {
		body["idempotency_key"] = operation
		body["confirmation"] = map[string]any{"mode": "none"}
	}
	return adminRequest(t, h, token, http.MethodPost, detailsAPI+method, body)
}

func detailsSchemas(t *testing.T, h *harness, token string) map[string]*huma.Schema {
	t.Helper()
	code, raw := adminRequest(t, h, token, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var manifest struct {
		Revision string `json:"revision"`
		Modules  []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID     string          `json:"id"`
				Output json.RawMessage `json:"output_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.Equal(t, "mvp-43", manifest.Revision)
	schemas := make(map[string]*huma.Schema)
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_registration_details" {
			continue
		}
		for _, method := range module.Methods {
			var schema huma.Schema
			require.NoError(t, json.Unmarshal(method.Output, &schema))
			schema.PrecomputeMessages()
			schemas[method.ID] = &schema
		}
	}
	require.Len(t, schemas, 9)
	return schemas
}

func validDetailsReceipt(t *testing.T, raw []byte, schema *huma.Schema) registrationdetails.Status {
	t.Helper()
	var receipt struct {
		Status string                      `json:"status"`
		Result *registrationdetails.Status `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "succeeded", receipt.Status, string(raw))
	require.NotNil(t, receipt.Result)
	var output struct {
		Result any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &output))
	require.NotNil(t, schema)
	validation := &huma.ValidateResult{}
	huma.Validate(
		huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer),
		schema,
		huma.NewPathBuffer(nil, 0),
		huma.ModeReadFromServer,
		output.Result,
		validation,
	)
	require.Empty(t, validation.Errors)
	require.NotContains(t, string(raw), "SYNTHETIC-PASSPORT")
	require.NotContains(t, string(raw), "Synthetic Legal Name")
	return *receipt.Result
}

func TestRegistrationDetailsAgentAPIUsesRedactedSchemasAndHistoricalReplay(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 72)
	h := setup(t, p)
	_, token := registrationUser(t, h, 95101, false)
	schemas := detailsSchemas(t, h, token)
	code, raw := detailsRequest(t, h, token, "get", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Zero(t, validDetailsReceipt(t, raw, schemas["get"]).Version)
	code, raw = detailsRequest(
		t,
		h,
		token,
		"set_legal_name",
		uuid.New(),
		map[string]any{"expected_version": 0, "legal_name": "Synthetic Legal Name"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	name := validDetailsReceipt(t, raw, schemas["set_legal_name"])
	require.True(t, name.LegalNamePresent)
	passportOperation := uuid.New()
	passportArgs := map[string]any{"expected_version": name.Version, "passport_number": "SYNTHETIC-PASSPORT-95101"}
	code, raw = detailsRequest(t, h, token, "set_passport_number", passportOperation, passportArgs)
	require.Equal(t, http.StatusOK, code, string(raw))
	completed := validDetailsReceipt(t, raw, schemas["set_passport_number"])
	require.Equal(
		t,
		registrationdetails.Status{Version: 2, LegalNamePresent: true, PassportNumberPresent: true},
		completed,
	)
	h.stop(t)
	p.registrationDetails = detailsProtector(t, 72)
	h.start(t)
	code, raw = detailsRequest(
		t,
		h,
		token,
		"set_legal_name",
		uuid.New(),
		map[string]any{"expected_version": completed.Version, "legal_name": "Synthetic Legal Name Updated"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.EqualValues(t, 3, validDetailsReceipt(t, raw, schemas["set_legal_name"]).Version)
	code, raw = detailsRequest(t, h, token, "set_passport_number", passportOperation, passportArgs)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, completed, validDetailsReceipt(t, raw, schemas["set_passport_number"]))
	code, raw = adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		detailsAPI+"operation",
		map[string]any{"protocol_version": 1, "operation_id": passportOperation},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, completed, validDetailsReceipt(t, raw, schemas["operation"]))
}

func TestRegistrationDetailsAgentAPIRejectsForeignStaleAndRevokedOwner(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 73)
	h := setup(t, p)
	ownerID, owner := registrationUser(t, h, 95102, false)
	_, other := registrationUser(t, h, 95103, false)
	operation := uuid.New()
	args := map[string]any{"expected_version": 0, "passport_number": "SYNTHETIC-PASSPORT-95102"}
	code, raw := detailsRequest(t, h, owner, "set_passport_number", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw = detailsRequest(t, h, other, "operation", uuid.New(), map[string]any{"operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "not_found")
	code, raw = detailsRequest(t, h, other, "get", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"passport_number_present":false`)
	code, raw = detailsRequest(t, h, owner, "set_passport_number", uuid.New(), args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	_, err := h.store.SetUserRevoked(t.Context(), ownerID, "details-api-fixture")
	require.NoError(t, err)
	for _, method := range []string{"get", "operation", "set_passport_number"} {
		requestArgs := map[string]any{}
		if method == "operation" {
			requestArgs["operation_id"] = operation
		}
		if method == "set_passport_number" {
			requestArgs = args
		}
		code, _ = detailsRequest(t, h, owner, method, operation, requestArgs)
		require.Equal(t, http.StatusForbidden, code, method)
	}
}

func TestRegistrationDetailsAgentToolsStayAbsentWithoutConfiguredSecret(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, token := registrationUser(t, h, 95104, false)
	code, raw := adminRequest(t, h, token, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "zinusya_registration_details")
	code, _ = detailsRequest(t, h, token, "get", uuid.New(), map[string]any{})
	require.Equal(t, http.StatusNotFound, code)
}
