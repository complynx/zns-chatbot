package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const (
	scriptMassageBook      = "massage.book"
	scriptMassageCancel    = "massage.cancel"
	scriptMassageInstant   = "massage.practitioner.instant"
	scriptMassageConfigure = "massage.practitioner.configure"
)

func (b *Bot) scriptMassageEntries(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return (agenthost.MassageScriptCatalog{Client: b.API, Binding: agenthost.ScriptToolEntry{
		Prepare: b.prepareMassageTool, Execute: b.executeMassageTool, ResultLimit: maxScriptReadBytes,
	}}).Entries(ctx, owner)
}
func (b *Bot) prepareMassageTool(ctx context.Context, owner string, update int64,
	call scriptclient.ToolCall, _ agent.Input) (agenthost.ScriptToolRecord, error) {
	record := agenthost.ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return record, errors.New("tool unavailable")
	}
	request, err := b.bindMassageTool(ctx, owner, call)
	if err != nil {
		return record, err
	}
	request.Update = update
	record.Massage = &request
	return record, nil
}

func (b *Bot) executeMassageTool(ctx context.Context, owner string, _ scriptclient.ToolCall,
	record agenthost.ScriptToolRecord, _ *agent.Input) (any, error) {
	request := record.Massage
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if request == nil || !ok || source.owner != owner {
		return nil, errors.New("tool unavailable")
	}
	if record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("missing admitted source")
	}
	var result any
	var err error
	switch {
	case request.Command != nil:
		result, err = b.Host.ExecuteDerivedMassage(ctx, owner, *request.Command, *record.Source)
	case request.Preferences != nil:
		result, err = b.Host.SetDerivedMassagePreferences(
			ctx,
			owner,
			request.Event,
			*request.Preferences,
			*record.Source,
		)
	default:
		return nil, errors.New("tool unavailable")
	}
	if err != nil {
		return nil, err
	}
	state := massageView{Event: request.Event, View: massageMine}
	if err = b.saveMassageState(ctx, owner, source.in.chat, request.Update, state); err != nil {
		return nil, err
	}
	if err = b.RenderMassage(ctx, owner, source.in.chat, ""); err != nil {
		return nil, err
	}
	return result, nil
}
