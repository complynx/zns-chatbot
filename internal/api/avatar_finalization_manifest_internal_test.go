package api

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestAvatarFinalizationManifestSeparatesReceiptAndDeliveryWithoutCallerIdentity(t *testing.T) {
	t.Parallel()
	module := avatarModule()
	methods := module[manifestMethods].([]any)
	found := false
	for _, m := range methods {
		method := m.(map[string]any)
		if method["id"] != "finalize_draft" {
			continue
		}
		found = true
		require.Equal(t, effectWrite, method[manifestEffect])
		require.Equal(t, true, method[manifestIdempotency])
		require.Equal(t, avatarPath+"finalization_operation", method[manifestStatusPath])
		raw, err := json.Marshal(method[manifestInputSchema])
		require.NoError(t, err)
		var schema huma.Schema
		require.NoError(t, json.Unmarshal(raw, &schema))
		schema.PrecomputeMessages()
		registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
		input := map[string]any{"draft_id": "00000000-0000-0000-0000-000000000001", "expected_version": float64(1)}
		validate := func() []error {
			result := &huma.ValidateResult{}
			huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
			return result.Errors
		}
		require.Empty(t, validate())
		for _, key := range []string{"user_id", "event_id", "telegram_user_id", "run_id", "transform"} {
			input[key] = "caller-supplied"
			require.NotEmpty(t, validate())
			delete(input, key)
		}
		receipt := method[manifestOutputSchema].(map[string]any)[schemaProperties].(map[string]any)
		require.Contains(t, receipt, schemaDraftID)
		require.NotContains(t, receipt, schemaTaskID)
		require.NotContains(t, receipt, schemaSourceFileID)
	}
	require.True(t, found)
	legacy := avatarReceiptSchema()[schemaProperties].(map[string]any)
	require.Contains(t, legacy, schemaTaskID)
	require.Contains(t, legacy, schemaSourceFileID)
	require.NotContains(t, legacy, schemaDraftID)
}
