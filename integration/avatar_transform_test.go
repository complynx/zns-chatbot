package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
)

func TestAvatarTransformKeepsIdentityAndReplaysCanonicalEditedRequest(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, avatarProcessorFunc(renderer.Render))
	operation := uuid.New()
	arguments := map[string]any{"source_file_id": f.source}
	code, raw := avatarRequest(t, f, f.owner, "generate", operation, arguments)
	require.Equal(t, http.StatusOK, code, string(raw))
	original := generatedAvatar(t, raw)
	require.Nil(t, original.Transform)
	require.Equal(t, f.rendered.SHA256, original.SHA256)
	require.Equal(t, avatar.ProcessorVersion, original.ProcessorVersion)
	arguments["transform"] = avatar.IdentityTransform()
	code, raw = avatarRequest(t, f, f.owner, "generate", operation, arguments)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, original, generatedAvatar(t, raw), "Explicit identity preserves the original generation receipt")
	edit := avatar.Transform{ZoomPPM: avatar.FramePPM, AngleMilli: avatar.HalfTurnMilli}
	editedOperation := uuid.New()
	arguments["transform"] = edit
	code, raw = avatarRequest(t, f, f.owner, "generate", editedOperation, arguments)
	require.Equal(t, http.StatusOK, code, string(raw))
	edited := generatedAvatar(t, raw)
	edit.AngleMilli = -avatar.HalfTurnMilli
	require.Equal(t, &edit, edited.Transform)
	require.Equal(t, avatar.EditedProcessorVersion, edited.ProcessorVersion)
	require.NotEqual(t, original.SHA256, edited.SHA256)
	arguments["transform"] = edit
	code, raw = avatarRequest(t, f, f.owner, "generate", editedOperation, arguments)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, edited, generatedAvatar(t, raw), "Equivalent half-turns share exact operation identity")
	edit.OffsetXPPM++
	arguments["transform"] = edit
	code, raw = avatarRequest(t, f, f.owner, "generate", editedOperation, arguments)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	var count int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatars").Scan(&count))
	require.Equal(t, 2, count)
	view := avatarView(t, f, original)
	require.Equal(t, original, view.Avatar, "Later edits cannot change the old receipt")
}

func TestAvatarTransformRejectsWrongProcessorEchoBeforeCommit(t *testing.T) {
	t.Parallel()
	f := setupAvatar(
		t,
		avatarProcessorFunc(func(ctx context.Context, raw []byte, _ avatar.Transform) (avatar.Rendered, error) {
			return renderer.Render(ctx, raw, avatar.IdentityTransform())
		}),
	)
	code, raw := avatarRequest(
		t,
		f,
		f.owner,
		"generate",
		uuid.New(),
		map[string]any{"source_file_id": f.source, "transform": avatar.Transform{ZoomPPM: 2 * avatar.FramePPM}},
	)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, string(raw), `"status":"rejected"`)
	var count int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_avatars").Scan(&count))
	require.Zero(t, count)
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_outbox WHERE payload->>'kind'='avatar_document'").
			Scan(&count),
	)
	require.Zero(t, count)
}

func TestAvatarTransformLimitsAndManifestMatchToolContract(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	code, _ := avatarRequest(
		t,
		f,
		f.owner,
		"generate",
		uuid.New(),
		map[string]any{"source_file_id": f.source, "transform": avatar.Transform{}},
	)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	code, raw := adminRequest(t, f.h, f.owner, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code)
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID     string         `json:"id"`
				Input  map[string]any `json:"input_schema"`
				Output map[string]any `json:"output_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	found := false
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_avatars" {
			continue
		}
		for _, method := range module.Methods {
			if method.ID != "generate" {
				continue
			}
			found = true
			input := method.Input["properties"].(map[string]any)
			transform := input["transform"].(map[string]any)
			fields := transform["properties"].(map[string]any)
			zoom := fields["zoom_ppm"].(map[string]any)
			require.InDelta(t, avatar.MinZoomPPM, zoom["minimum"], 0)
			require.InDelta(t, avatar.MaxZoomPPM, zoom["maximum"], 0)
			require.Contains(t, method.Output["properties"].(map[string]any), "transform")
		}
	}
	require.True(t, found)
}
