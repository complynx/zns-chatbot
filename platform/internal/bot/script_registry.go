package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const maxOrdinaryScriptResult = 2048

func (b *Bot) prepareScriptMemoryTool(
	ctx context.Context,
	owner string,
	updateID int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	return b.prepareMemoryTool(ctx, owner, updateID, call, record)
}

func (b *Bot) executeScriptMemoryTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	return b.executeMemoryTool(ctx, owner, call, record)
}
