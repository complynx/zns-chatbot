package api

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestAvatarResolutionManifestRequiresExactApprovalWithoutEventOrRecipient(t *testing.T) {
	t.Parallel()
	methods := avatarDeliveryAdministrativeMethods()
	require.Len(t, methods, 4)
	raw, err := json.Marshal(methods[0])
	require.NoError(t, err)
	var method struct {
		ID           string      `json:"id"`
		Path         string      `json:"path"`
		StatusPath   string      `json:"status_path"`
		Effect       string      `json:"effect"`
		Idempotency  bool        `json:"idempotency"`
		InputSchema  huma.Schema `json:"input_schema"`
		Confirmation struct {
			Mode        string `json:"mode"`
			PreparePath string `json:"prepare_path"`
		} `json:"confirmation"`
	}
	require.NoError(t, json.Unmarshal(raw, &method))
	require.Equal(t, "resolve_avatar_delivery", method.ID)
	require.Equal(t, avatarResolutionPath, method.Path)
	require.Equal(t, avatarResolutionPath+"/status", method.StatusPath)
	require.Equal(t, "write", method.Effect)
	require.True(t, method.Idempotency)
	require.Equal(t, assignmentConfirmationMode, method.Confirmation.Mode)
	require.Equal(t, avatarResolutionPath+"/prepare", method.Confirmation.PreparePath)
	method.InputSchema.PrecomputeMessages()
	registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
	input := map[string]any{
		"delivery_id":      "00000000-0000-0000-0000-000000000001",
		"expected_version": float64(1),
		"action":           "retry",
		"reason":           "checked chat",
	}
	validate := func() []error {
		result := &huma.ValidateResult{}
		huma.Validate(registry, &method.InputSchema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
		return result.Errors
	}
	require.Empty(t, validate())
	for _, key := range []string{"event_id", "user_id", "telegram_user_id", "global_admin"} {
		input[key] = "caller-supplied"
		require.NotEmpty(t, validate())
		delete(input, key)
	}
	input["action"] = "force"
	require.NotEmpty(t, validate())
	input["action"] = "dismiss"
	require.Empty(t, validate())
	delete(input, "expected_version")
	require.NotEmpty(t, validate())
}
