package bot

import (
	"context"

	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func (b *Bot) executePassTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record agenthost.ScriptToolRecord,
	input *agent.Input,
) (any, error) {
	if record.PassReceiptID != "" {
		return b.executePassReceipt(ctx, owner, call, record)
	}
	if record.Pass == nil {
		if call.Name == scriptPassOperations {
			var query derivedmutation.PassOperationQuery
			if err := decodeScriptArguments(call.Arguments, &query); err != nil {
				return nil, err
			}
			return b.passOperations(ctx, owner, query)
		}
		if call.Name == scriptPassTiers {
			return b.readPassTiers(ctx, owner, call)
		}
		return b.readPassTool(ctx, owner, call, input)
	}
	request := record.Pass
	if err := b.authorizePassRequest(ctx, owner, request); err != nil {
		return nil, err
	}
	if record.Source == nil || !record.Source.Valid() {
		return nil, errors.New("missing admitted source")
	}
	result, err := b.executePassRequest(ctx, owner, request, *record.Source)
	if err != nil {
		if core.IsDatabaseFailure(err) {
			return nil, core.ErrDatabase
		}
		var problem *core.ProblemError
		if errors.As(err, &problem) && problem.Status < http.StatusInternalServerError {
			return nil, err
		}
		// The operation was admitted durably. Do not convert an uncertain commit into
		// a new mutation; expose its owner-bound reference for exact continuation.
		return map[string]any{registrationOperationID: request.ID, "complete": false, "interrupted": true}, nil
	}
	complete := true
	if request.Name == scriptPassExport {
		delivered, ok := result.(map[string]bool)
		complete = ok && delivered[botReceiptDelivered]
	}
	return map[string]any{registrationOperationID: request.ID, "complete": complete, "result": result}, nil
}

func (b *Bot) executePassRequest(
	ctx context.Context,
	owner string,
	request *agenthost.ScriptPassRequest,
	source readsource.Derivation,
) (any, error) {
	switch {
	case request.Menu != nil:
		if err := b.storePassMenuWithSource(
			ctx,
			owner,
			request.Chat,
			request.ExportUpdate,
			*request.Menu,
			&source,
		); err != nil {
			return nil, err
		}
		err := b.RenderPassMenu(ctx, owner, request.Chat, "")
		return map[string]bool{"shown": err == nil}, err
	case request.Command != nil:
		return (interaction.RegistrationExecutor{Derived: b.Host}).Command(ctx, owner, *request.Command, &source)
	case request.Assignment != nil:
		return (interaction.RegistrationExecutor{Derived: b.Host}).Assignment(ctx, owner, *request.Assignment, &source)
	case request.Batch != nil:
		return b.executePassBatch(ctx, owner, request, source)
	case request.Name == scriptPassExport:
		notice, err := b.exportPassesWithSource(
			ctx,
			incoming{owner: owner, chat: request.Chat},
			request.ExportUpdate,
			&source,
		)
		return map[string]bool{botReceiptDelivered: err == nil && notice == i18n.RegistrationExported}, err
	default:
		return nil, errors.New("pass operation unavailable")
	}
}

func (b *Bot) readPassTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	input *agent.Input,
) (any, error) {
	var args struct {
		Event  string `json:"event"`
		Target string `json:"target"`
		View   string `json:"view"`
		Cursor string `json:"cursor"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	p := agent.RegistrationProposal{
		Name:   agent.RegistrationRead,
		Event:  args.Event,
		Target: args.Target,
		View:   args.View,
		Cursor: args.Cursor,
	}
	if call.Name != scriptPassRead && args.View != "" {
		return nil, errors.New("invalid read view")
	}
	switch call.Name {
	case scriptPassRead:
		if args.Target != "" {
			return nil, errors.New("invalid read target")
		}
		switch p.View {
		case "", passMenuHome, passMenuInvitations, registrationAdminsView, registrationPayment:
		default:
			return nil, errors.New("invalid read view")
		}
	default:
		view, known := agenthost.PassPrivilegedReadView(call.Name)
		if !known {
			return nil, errors.New("pass read unavailable")
		}
		p.View = view
	}
	if err := agent.Validate(agent.Plan{View: agent.RegistrationView, RegistrationAction: &p}); err != nil {
		return nil, err
	}
	source, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || source.owner != owner {
		return nil, errors.New("pass source unavailable")
	}
	if err := b.performRegistrationRead(ctx, owner, source.update.ID, p, input); err != nil {
		return nil, err
	}
	for _, read := range input.Registration.Reads {
		if read.Request == p {
			return read, nil
		}
	}
	return nil, errors.New("pass read omitted")
}

func (b *Bot) readPassTiers(ctx context.Context, owner string, call scriptclient.ToolCall) (any, error) {
	var args struct {
		Event  string `json:"event"`
		Cursor string `json:"cursor"`
	}
	if err := decodeScriptArguments(call.Arguments, &args); err != nil {
		return nil, err
	}
	if args.Event == "" || len(args.Event) > 100 {
		return nil, errors.New("invalid pass event")
	}
	cursor, err := core.DecodeReadCursor(args.Cursor, owner, scriptPassTiers+":"+args.Event)
	if err != nil {
		return nil, err
	}
	result, err := b.API.PassTierStatus(ctx, owner, args.Event)
	if err != nil {
		return nil, err
	}
	return core.JSONReadChunk(result, cursor)
}
