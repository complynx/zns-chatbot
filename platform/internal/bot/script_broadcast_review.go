package bot

import (
	"context"
	"errors"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const scriptBroadcastShow = "broadcasts.show"

// The receipt binds the selected campaign and native destination before display.
// Interrupted transport remains uncertain; script recovery must not resend it.

func (b *Bot) prepareBroadcastReview(ctx context.Context, owner string, _ int64,
	call scriptclient.ToolCall, _ agent.Input) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	var args agenthost.BroadcastReviewArguments
	if err := decodeBroadcastReview(call, &args); err != nil {
		return record, err
	}
	if args.ID <= 0 || args.Offset < 0 || len(args.Cursor) > 2048 {
		return record, errors.New("invalid broadcast review arguments")
	}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return record, errors.New("broadcast source unavailable")
	}
	record.BroadcastReview = &agenthost.BroadcastReviewRequest{Owner: owner, Chat: source.in.chat, Arguments: args}
	return record, nil
}

func decodeBroadcastReview(call scriptclient.ToolCall, args *agenthost.BroadcastReviewArguments) error {
	if call.Name == scriptBroadcastShow {
		var show struct {
			ID     int64 `json:"id"`
			Offset int64 `json:"offset,omitempty"`
		}
		err := decodeScriptArguments(call.Arguments, &show)
		args.ID, args.Offset = show.ID, show.Offset
		return err
	}
	return decodeScriptArguments(call.Arguments, args)
}

func (b *Bot) executeBroadcastReview(ctx context.Context, owner string, call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord, _ *agent.Input) (any, error) {
	request := record.BroadcastReview
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner || request.Owner != owner || request.Chat != source.in.chat {
		return nil, errors.New("broadcast source unavailable")
	}
	args := request.Arguments
	if call.Name == scriptBroadcastShow {
		prefs, err := b.API.Preferences(ctx, owner)
		if err != nil {
			return nil, err
		}
		err = b.sendAdminMessagePage(ctx, source.in, args.ID, args.Offset, &orderMessages{language: prefs.Language})
		return map[string]any{"id": args.ID, "displayed": err == nil, "manual_send_required": true}, err
	}
	cursor, err := core.DecodeReadCursor(
		args.Cursor,
		owner,
		fmt.Sprintf("broadcasts.review:%d:%d", args.ID, args.Offset),
	)
	if err != nil {
		return nil, appclient.ReadError(err)
	}
	page, err := b.loadAdminMessagePage(ctx, owner, args.ID, args.Offset)
	if err != nil {
		return nil, appclient.ReadError(err)
	}
	chunk, err := core.JSONReadChunk(page, cursor)
	return chunk, appclient.ReadError(err)
}
