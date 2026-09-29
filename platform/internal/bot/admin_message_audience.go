package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptBroadcastAudience = "broadcasts.audience"
const scriptBroadcastProfile = "broadcasts.profile"

type broadcastReadArguments struct {
	UserID string `json:"user_id,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

func (b *Bot) broadcastReadEntries() []scriptToolEntry {
	tools := []scriptclient.Tool{
		{
			Name:        scriptBroadcastAudience,
			Description: "Administrator-only keyset pages of same-deployment user IDs, display-name excerpts and available profile field names. Follow next_cursor until more=false. Use broadcasts.profile to inspect complete values and filter with JavaScript; preview explicit recipient IDs afterward. No SQL.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"cursor":{"type":"string","maxLength":2048}},"additionalProperties":false}`,
			),
		},
		{
			Name:        scriptBroadcastProfile,
			Description: "Read an authorized broadcast profile as complete JSON chunks. Concatenate json fields until more=false, then JSON.parse. Missing fields stay absent, null stays null. Stale means restart; do not use incomplete chunks. Current global administrator access required.",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"user_id":{"type":"string","maxLength":20},"cursor":{"type":"string","maxLength":2048}},"required":["user_id"],"additionalProperties":false}`,
			),
		},
	}
	entries := make([]scriptToolEntry, 0, len(tools))
	for _, tool := range tools {
		entries = append(
			entries,
			scriptToolEntry{
				descriptor:  tool,
				prepare:     prepareBroadcastRead,
				execute:     b.executeBroadcastRead,
				resultLimit: maxScriptReadBytes,
			},
		)
	}
	return entries
}

func prepareBroadcastRead(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var arguments broadcastReadArguments
	if err := decodeScriptArguments(call.Arguments, &arguments); err != nil {
		return record, err
	}
	const maxID = 20
	const maxCursor = 2048
	if call.Name == scriptBroadcastAudience && arguments.UserID != "" {
		return record, errors.New("invalid broadcast read arguments")
	}
	if len(arguments.UserID) > maxID || len(arguments.Cursor) > maxCursor ||
		call.Name == scriptBroadcastProfile && arguments.UserID == "" {
		return record, errors.New("invalid broadcast read arguments")
	}
	return record, nil
}

func (b *Bot) executeBroadcastRead(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	_ scriptToolRecord,
	_ *agent.Input,
) (any, error) {
	var arguments broadcastReadArguments
	if err := decodeScriptArguments(call.Arguments, &arguments); err != nil {
		return nil, err
	}
	if call.Name == scriptBroadcastAudience {
		var result core.ReadPage[adminmessage.AudienceUser]
		err := b.API.call(ctx, owner, http.MethodPost, "/v1/admin-messages/audience", arguments, &result)
		return result, scriptDomainAPIError(err)
	}
	var result core.ReadChunk
	err := b.API.call(ctx, owner, http.MethodPost, "/v1/admin-messages/profile", arguments, &result)
	return result, scriptDomainAPIError(err)
}
