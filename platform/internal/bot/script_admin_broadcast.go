package bot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type broadcastSourceKey struct{}
type broadcastSource struct {
	owner  string
	update telegram.Update
	in     incoming
}

func withAdminMessageSource(ctx context.Context, in incoming, u telegram.Update) context.Context {
	return context.WithValue(ctx, broadcastSourceKey{}, broadcastSource{in.owner, u, in})
}

func (b *Bot) scriptBroadcastEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return (agenthost.BroadcastScriptCatalog{
		Allowed: func(ctx context.Context, owner string) (bool, error) {
			var capability struct {
				Allowed bool `json:"allowed"`
			}
			err := b.API.Call(ctx, owner, http.MethodPost, "/v1/admin-messages/capabilities", nil, &capability)
			return capability.Allowed, err
		},
		ActionBinding: agenthost.ScriptToolEntry{
			Prepare: b.prepareBroadcastTool, Execute: b.executeBroadcastTool, ResultLimit: maxOrdinaryScriptResult,
		},
		ReadBinding: agenthost.ScriptToolEntry{
			Prepare: prepareBroadcastRead, Execute: b.executeBroadcastRead, ResultLimit: maxScriptReadBytes,
		},
		ReviewBinding: agenthost.ScriptToolEntry{
			Prepare: b.prepareBroadcastReview, Execute: b.executeBroadcastReview, ResultLimit: maxScriptReadBytes,
		},
	}).Entries(ctx, owner)
}

func (b *Bot) prepareBroadcastTool(
	ctx context.Context,
	owner string,
	updateID int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return record, errors.New("broadcast source unavailable")
	}
	var args struct {
		Command string `json:"command,omitempty"`
		InputID int64  `json:"input_id,omitempty"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return record, err
	}
	digest := sha256.Sum256(append([]byte(call.Name), call.Arguments...))
	request := &agenthost.BroadcastToolRequest{
		Command: args.Command,
		InputID: args.InputID,
		Key:     fmt.Sprintf("script-broadcast-%d-%x", updateID, digest[:8]),
	}
	switch call.Name {
	case scriptBroadcastPreview:
		if _, err := adminmessage.ParseCommand(args.Command); err != nil {
			return record, err
		}
	case scriptBroadcastPending:
	case scriptBroadcastCancel:
		if args.InputID <= 0 {
			return record, errors.New("invalid broadcast input")
		}
	case scriptBroadcastAttach:
		var err error
		request.Attachment, err = b.prepareBroadcastAttachment(ctx, owner, source, args.InputID, request.Key)
		if err != nil {
			return record, err
		}
	default:
		return record, errors.New("tool unavailable")
	}
	record.Broadcast = request
	return record, nil
}

func (b *Bot) executeBroadcastTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	_ *agent.Input,
) (any, error) {
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner || record.Broadcast == nil || record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("broadcast source unavailable")
	}
	request := record.Broadcast
	switch call.Name {
	case scriptBroadcastPending:
		var pending []adminmessage.Input
		err := b.API.Call(
			ctx,
			owner,
			http.MethodPost,
			"/v1/admin-messages/input/pending",
			map[string]int64{broadcastChatKey: source.in.chat},
			&pending,
		)
		return pending, err
	case scriptBroadcastCancel:
		err := b.Host.CancelDerivedAdminMessageInput(ctx, owner, request.InputID, *record.Source)
		return map[string]bool{"ok": err == nil}, err
	}
	prefs, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return nil, err
	}
	messages := &orderMessages{language: prefs.Language}
	preview, requested, err := b.createBroadcastPreview(ctx, source, request, *record.Source, messages)
	if requested {
		return map[string]bool{"input_requested": err == nil}, err
	}
	if err != nil {
		return nil, err
	}
	err = b.sendAdminMessagePage(ctx, source.in, preview.ID, 0, messages)
	return map[string]any{"id": preview.ID, "confirmation_required": true}, err
}

func (b *Bot) createBroadcastPreview(
	ctx context.Context,
	source broadcastSource,
	request *agenthost.BroadcastToolRequest,
	derivation readsource.Derivation,
	messages *orderMessages,
) (adminmessage.Message, bool, error) {
	var preview adminmessage.Message
	if request.Attachment != nil {
		if source.update.Message == nil {
			return preview, false, errors.New("broadcast source unavailable")
		}
		if err := b.registerAdminMessageSource(ctx, source.owner, request.Key, *source.update.Message); err != nil {
			return preview, false, err
		}
		attached, err := b.Host.AttachDerivedAdminMessageInput(ctx, source.owner, *request.Attachment, derivation)
		return attached, false, err
	}
	command, err := adminmessage.ParseCommand(request.Command)
	if err != nil {
		return preview, false, err
	}
	if command.NeedsInput() {
		input, inputErr := b.Host.BeginDerivedAdminMessageInput(
			ctx,
			source.owner,
			request.Key,
			request.Command,
			source.in.chat,
			derivation,
		)
		if inputErr != nil {
			return preview, true, inputErr
		}
		err = b.sendAdminInputPrompt(ctx, source.in, input, messages)
		return preview, true, err
	}
	preview, err = b.Host.PreviewDerivedAdminMessage(ctx, source.owner, request.Key, request.Command, derivation)
	return preview, false, err
}

func (b *Bot) prepareBroadcastAttachment(
	ctx context.Context,
	owner string,
	source broadcastSource,
	inputID int64,
	key string,
) (*adminmessage.Attachment, error) {
	var attachment *adminmessage.Attachment
	if source.update.Message == nil {
		return nil, errors.New("broadcast source unavailable")
	}
	var pending []adminmessage.Input
	if err := b.API.Call(
		ctx,
		owner,
		http.MethodPost,
		"/v1/admin-messages/input/pending",
		map[string]int64{broadcastChatKey: source.in.chat},
		&pending,
	); err != nil {
		return nil, err
	}
	for _, input := range pending {
		if input.ID == inputID {
			attachment = &adminmessage.Attachment{
				InputID:  input.ID,
				ChatID:   source.in.chat,
				PromptID: input.PromptID,
				Key:      key,
			}
		}
	}
	if attachment == nil {
		return nil, errors.New("broadcast input unavailable")
	}
	return attachment, nil
}
