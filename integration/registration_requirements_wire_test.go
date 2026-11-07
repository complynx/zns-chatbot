package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/events"
)

func TestRequiredDetailsFlagsCanBeOmittedThroughEventHTTPRoutes(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"legal_name", "passport_number"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			h := setup(t, newPeers(t))
			_, operator := registrationUser(t, h, 95301, true)
			event := registrationEvent(t, h, operator)
			encoded, err := json.Marshal(event.Document)
			require.NoError(t, err)
			var document map[string]any
			require.NoError(t, json.Unmarshal(encoded, &document))
			document["registration"].(map[string]any)["details_requirements"] = map[string]any{flag: true}
			code, raw := adminRequest(
				t,
				h,
				operator,
				http.MethodPut,
				"/internal/admin/events/"+event.ID.String(),
				map[string]any{
					"operation_id": uuid.NewString(), "expected_version": event.Version, "document": document,
				},
			)
			require.Equal(t, http.StatusOK, code, string(raw))
			var updated events.Event
			require.NoError(t, json.Unmarshal(raw, &updated))
			require.NotNil(t, updated.Document.Registration.DetailsRequirements)
			require.True(t, updated.Document.Registration.DetailsRequirements.LegalName)
			require.Equal(
				t,
				flag == "passport_number",
				updated.Document.Registration.DetailsRequirements.PassportNumber,
			)
			code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI+"/prepare", map[string]any{
				"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": map[string]any{
					"id": event.ID, "expected_version": updated.Version, "document": document,
				},
			})
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), `"status":"prepared"`)
		})
	}
}

func TestRequiredDetailsContextResponsesMatchAdvertisedSchema(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 95311, true)
	_, owner := registrationUser(t, h, 95312, false)
	event := requireEventDetails(t, h, registrationEvent(t, h, operator))
	code, raw := adminRequest(t, h, owner, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID     string      `json:"id"`
				Output huma.Schema `json:"output_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	schemas := map[string]*huma.Schema{}
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_context" {
			continue
		}
		for _, method := range module.Methods {
			schema := method.Output
			schema.PrecomputeMessages()
			schemas[method.ID] = &schema
		}
	}
	for _, method := range []string{"list", "resolve"} {
		args := map[string]any{"module": "registrations"}
		if method == "resolve" {
			args["event_id"] = event.ID
		}
		code, raw = adminRequest(t, h, owner, http.MethodPost, "/internal/agent/context/"+method, map[string]any{
			"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": args,
		})
		require.Equal(t, http.StatusOK, code, string(raw))
		var response struct {
			Status string `json:"status"`
			Result any    `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &response))
		require.Equal(t, "succeeded", response.Status)
		require.Contains(t, string(raw), `"details_requirements":{"legal_name":true,"passport_number":true}`)
		schema := schemas[method]
		require.NotNil(t, schema)
		validation := &huma.ValidateResult{}
		huma.Validate(
			huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer),
			schema,
			huma.NewPathBuffer(nil, 0),
			huma.ModeReadFromServer,
			response.Result,
			validation,
		)
		require.Empty(t, validation.Errors)
	}
}
