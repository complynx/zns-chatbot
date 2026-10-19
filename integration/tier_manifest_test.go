package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestTierAssignmentReceiptMatchesAdvertisedAgentOutput(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	configureTierPair(t, f, admin, &args, tierPolicy("paired", 2))
	prepared, err := f.H.store.PreparePairAssignment(context.Background(), admin, "manifest-tier-pair", args)
	require.NoError(t, err)
	_, err = f.H.store.ConfirmPairAssignment(context.Background(), admin, "manifest-tier-pair", args, prepared.ID)
	require.NoError(t, err)
	token := pairUserToken(f, admin)
	code, raw := adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var discovery struct {
		Methods []struct {
			ID     string          `json:"id"`
			Output json.RawMessage `json:"output_schema"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	var schema huma.Schema
	for _, method := range discovery.Methods {
		if method.ID == "assign" {
			require.NoError(t, json.Unmarshal(method.Output, &schema))
		}
	}
	require.NotEmpty(t, schema.Properties)
	schema.PrecomputeMessages()
	code, raw = adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/assign/status", map[string]any{
		"protocol_version": 1, "operation_id": "manifest-tier-pair",
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt struct {
		Status string `json:"status"`
		Result any    `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "succeeded", receipt.Status)
	validation := &huma.ValidateResult{}
	huma.Validate(huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer), &schema,
		huma.NewPathBuffer(nil, 0), huma.ModeReadFromServer, receipt.Result, validation)
	require.Empty(t, validation.Errors, "A committed tier receipt must pass the output contract consumed by Loom")
}
