package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptBroadcastAudience = "broadcasts.audience"
const scriptBroadcastProfile = "broadcasts.profile"

type broadcastReadArguments struct {
	UserID string `json:"user_id,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

func prepareBroadcastRead(
	_ context.Context,
	_ string,
	_ int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
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
	_ agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	var arguments broadcastReadArguments
	if err := decodeScriptArguments(call.Arguments, &arguments); err != nil {
		return nil, err
	}
	if call.Name == scriptBroadcastAudience {
		var result core.ReadPage[adminmessage.AudienceUser]
		err := b.API.Call(ctx, owner, http.MethodPost, "/v1/admin-messages/audience", arguments, &result)
		return result, appclient.ReadError(err)
	}
	var result core.ReadChunk
	err := b.API.Call(ctx, owner, http.MethodPost, "/v1/admin-messages/profile", arguments, &result)
	return result, appclient.ReadError(err)
}
