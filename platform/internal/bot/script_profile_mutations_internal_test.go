package bot

import (
	"encoding/json"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestProfileMutationRejectsAuthorityAndInvalidArguments(t *testing.T) {
	t.Parallel()
	b := &Bot{}
	for _, args := range []string{`{}`, `{"field":"role","value":"admin"}`, `{"field":"passport","value":""}`, `{"field":"passport","value":"x","owner":"bob"}`, `{"field":"role","value":"leader","version":1}`, `{"field":"role","value":"leader","key":"injected"}`} {
		_, err := b.prepareScriptProfileMutation(
			t.Context(),
			"alice",
			1,
			scriptclient.ToolCall{Name: scriptProfileSet, Arguments: json.RawMessage(args)},
			agent.Input{Text: "Save my profile"},
		)
		require.Error(t, err)
	}
	for _, args := range []string{`{}`, `{"language":"de"}`, `{"language":"ru","initialize":true}`, `{"language":"en","operation_key":"x"}`} {
		_, err := b.prepareScriptProfileMutation(
			t.Context(),
			"alice",
			1,
			scriptclient.ToolCall{Name: scriptLanguageSet, Arguments: json.RawMessage(args)},
			agent.Input{Text: "Switch language"},
		)
		require.Error(t, err)
	}
}

func TestPrivateProfileScriptRetainsOnlyEffectEvidence(t *testing.T) {
	t.Parallel()
	secret := "private-canary"
	record := agenthost.ScriptRecord{
		Request: agent.ScriptProposal{Code: secret, InputJSON: secret},
		Run:     agent.ScriptRun{Code: secret, Result: json.RawMessage(`"private-canary"`)},
		Calls: []agenthost.ScriptToolRecord{
			{
				Profile: &agenthost.ScriptProfileMutation{
					Field:   "passport",
					Value:   secret,
					Version: 2,
					Key:     "host-key",
				},
				Outcome: agent.ScriptToolResult{
					Name:   scriptProfileSet,
					Result: json.RawMessage(`{"applied":true,"version":3}`),
				},
			},
			{Outcome: agent.ScriptToolResult{Name: "profile.get", Result: json.RawMessage(`"private-canary"`)}},
		},
	}
	payload, err := json.Marshal(record.Calls[0])
	require.NoError(t, err)
	require.NotContains(t, string(payload), secret)
	liveCommand := record.Calls[0].Profile
	agenthost.RedactProfileScript(&record)
	require.Equal(t, secret, liveCommand.Value)
	payload, err = json.Marshal(record)
	require.NoError(t, err)
	require.NotContains(t, string(payload), secret)
	require.Contains(t, string(payload), "host-key")
	require.Contains(t, string(payload), `"applied":true`)
	require.Equal(t, "passport", record.Calls[0].Profile.Field)
}
