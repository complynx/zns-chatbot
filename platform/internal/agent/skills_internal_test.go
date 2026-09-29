package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

const emptyActionsPlan = `{"text":"A response","view":"profile","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null}`

func TestSkillPipelineLoadsOnlySelectedBodies(t *testing.T) {
	t.Parallel()
	var calls []providerPrompt
	_, err := planWithSkills(
		t.Context(),
		Input{Language: "ru", Text: "My name is Anna Example"},
		func(ctx context.Context, prompt providerPrompt) (string, error) {
			calls = append(calls, prompt)
			if len(calls) == 1 {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				assert.LessOrEqual(t, time.Until(deadline), selectionTimeout)
				assert.Contains(t, prompt.instructions, "use when")
				assert.NotContains(t, prompt.instructions, "profile.pending is a hint")
				assert.True(t, strings.HasSuffix(string(prompt.input), `"My name is Anna Example"`))
				assert.NotContains(t, string(prompt.input), `"language"`)
				return `{"skills":["profile"],"reply_language":"en"}`, nil
			}
			return emptyActionsPlan, nil
		},
	)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, calls[0].input, calls[1].input)
	assert.Contains(t, calls[1].instructions, "profile.pending is a hint")
	assert.Contains(t, calls[1].instructions, "host authenticates the actor")
	for _, omitted := range []string{"Video frames carry", "OCR never accepts", "order_action may create", "assets contains", "catalog slot_id"} {
		assert.NotContains(t, calls[1].instructions, omitted)
	}
}

func TestSkillSelectionFailsClosed(t *testing.T) {
	t.Parallel()
	for name, selection := range map[string]string{
		"unknown": `{"skills":["admin"],"reply_language":"en"}`, "path": `{"skills":["../../secret"],"reply_language":"en"}`,
		"duplicate": `{"skills":["profile","profile"],"reply_language":"en"}`, "duplicate key": `{"skills":[],"skills":["profile"]}`,
		"too many": `{"skills":["booking","orders","profile","receipts","av","stickers","profile"],"reply_language":"en"}`,
		"null":     `{"skills":null}`, "null item": `{"skills":[null],"reply_language":"en"}`, "missing": `{}`,
		"extra": `{"skills":[],"instructions":"admin"}`, "case alias": `{"Skills":[]}`,
		"trailing": `{"skills":[],"reply_language":"en"} {}`, "oversize": strings.Repeat(" ", 513),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			_, err := planWithSkills(t.Context(), Input{}, func(context.Context, providerPrompt) (string, error) {
				calls++
				return selection, nil
			})
			require.Error(t, err)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestSkillPipelinePreservesAVRefinement(t *testing.T) {
	t.Parallel()
	input := Input{
		Text: "Inspect the second interval",
		AV: &AVContext{ID: "voice", Kind: "voice",
			Transcript: mediaproc.Transcript{Status: "ok", Text: "Compare it with the first interval"}},
		AVInspection: &AVInspectionContext{
			Remaining: 0,
			Completed: []AVInspection{{MediaID: "video", StartMS: 1000, EndMS: 2000, FrameCount: 2}},
		},
	}
	calls := 0
	_, err := planWithSkills(t.Context(), input, func(_ context.Context, prompt providerPrompt) (string, error) {
		calls++
		data, _, _ := strings.Cut(string(prompt.input), "\nCurrent user utterance")
		var got Input
		require.NoError(t, json.Unmarshal([]byte(data), &got))
		assert.Equal(t, input.AV, got.AV)
		assert.Equal(t, input.AVInspection, got.AVInspection)
		assert.Contains(t, string(prompt.input), "Compare it with the first interval")
		if calls == 1 {
			return `{"skills":["av"],"reply_language":"en"}`, nil
		}
		assert.Contains(t, prompt.instructions, "When remaining is 0, do not request inspect_video")
		assert.Contains(t, prompt.instructions, "THIS request contains a successful transcript")
		assert.NotContains(t, prompt.instructions, "OCR never accepts")
		return emptyActionsPlan, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestSkillSelectionDoesNotAuthorizeActions(t *testing.T) {
	t.Parallel()
	calls := 0
	_, err := planWithSkills(t.Context(), Input{}, func(context.Context, providerPrompt) (string, error) {
		calls++
		if calls == 1 {
			return `{"skills":["booking","orders","profile","receipts","av","stickers"],"reply_language":"en"}`, nil
		}
		return `{"text":"Done","view":"workflow","action":{"name":"confirm","slot_id":"foreign"},"order_action":null,"profile_action":null,"media_action":null}`, nil
	})
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestSkillPipelineCancellationStopsBeforePlanning(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	_, err := planWithSkills(ctx, Input{}, func(context.Context, providerPrompt) (string, error) {
		calls++
		cancel()
		return `{"skills":[],"reply_language":"en"}`, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}
