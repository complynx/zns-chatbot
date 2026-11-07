package api

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestMassageBindingManifestRequiresExactApprovalAndExplicitFalse(t *testing.T) {
	t.Parallel()
	methods := massageBindingWriteMethods()
	require.Len(t, methods, 3)
	raw, err := json.Marshal(methods[0])
	require.NoError(t, err)
	var method struct {
		ID                  string      `json:"id"`
		Effect              string      `json:"effect"`
		Path                string      `json:"path"`
		StatusPath          string      `json:"status_path"`
		InputSchema         huma.Schema `json:"input_schema"`
		IdempotencyRequired bool        `json:"idempotency"`
		Confirmation        struct {
			Mode        string `json:"mode"`
			PreparePath string `json:"prepare_path"`
		} `json:"confirmation"`
	}
	require.NoError(t, json.Unmarshal(raw, &method))
	require.Equal(t, "set_massage_specialist_binding", method.ID)
	require.Equal(t, "write", method.Effect)
	require.Equal(t, massageBindingWritePath, method.Path)
	require.Equal(t, massageBindingWritePath+"/status", method.StatusPath)
	require.True(t, method.IdempotencyRequired)
	require.Equal(t, assignmentConfirmationMode, method.Confirmation.Mode)
	require.Equal(t, massageBindingWritePath+"/prepare", method.Confirmation.PreparePath)
	method.InputSchema.PrecomputeMessages()
	input := map[string]any{
		"event_id":                  "00000000-0000-0000-0000-000000000001",
		"specialist_id":             "alex",
		"user_id":                   "00000000-0000-0000-0000-000000000002",
		"active":                    false,
		"expected_version":          float64(0),
		"expected_event_version":    float64(1),
		"expected_schedule_version": float64(1),
	}
	registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
	validate := func() []error {
		result := &huma.ValidateResult{}
		huma.Validate(registry, &method.InputSchema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
		return result.Errors
	}
	require.Empty(t, validate(), "An explicit false/zero is valid")
	input["existing_proposal_id"] = "00000000-0000-0000-0000-000000000003"
	require.Empty(t, validate())
	delete(input, "active")
	require.NotEmpty(t, validate(), "Omission must not mean revocation")
	input["active"] = false
	delete(input, "expected_version")
	require.NotEmpty(t, validate(), "Absence must be an explicit precondition")
}
