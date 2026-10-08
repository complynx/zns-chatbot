package bot

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// Execution and cached-result authorization share the same typed view mapping.

func (b *Bot) scriptPassToolReadChanged(
	ctx context.Context,
	owner string,
	call agenthost.ScriptToolRecord,
) (bool, error) {
	if len(call.Outcome.Result) == 0 {
		return false, nil
	}
	view, privileged := agenthost.PassPrivilegedReadView(call.Outcome.Name)
	if privileged || call.Outcome.Name == scriptPassRead {
		var read agent.RegistrationReadResult
		if err := json.Unmarshal(call.Outcome.Result, &read); err != nil {
			return false, err
		}
		if read.Error != "" {
			return false, nil
		}
		if read.Request.Name != agent.RegistrationRead || read.Request.Event == "" ||
			privileged && read.Request.View != view {
			return true, nil
		}
		if err := b.registrationRevalidator().Read(ctx, owner, &read); err != nil {
			return false, err
		}
		return read.Error != "", nil
	}
	switch call.Outcome.Name {
	case scriptPassTiers:
		if call.PassRead == nil || call.PassRead.Event == "" {
			return true, nil
		}
		return b.passCapabilityChanged(ctx, owner, call.PassRead.Event, agenthost.PassToolActions()[scriptPassTiers])
	case scriptPassInvitations:
		return b.scriptPassInvitationsChanged(ctx, owner, call)
	default:
		return false, nil
	}
}

func (b *Bot) passCapabilityChanged(ctx context.Context, owner, event, action string) (bool, error) {
	capability, err := b.API.PassCapabilities(ctx, owner, event)
	if err != nil {
		return passPrivacyFailure(err)
	}
	return action == "" || !slices.Contains(capability.Actions, action), nil
}

func (b *Bot) scriptPassInvitationsChanged(
	ctx context.Context,
	owner string,
	call agenthost.ScriptToolRecord,
) (bool, error) {
	if call.PassRead == nil || call.PassRead.Event == "" {
		return true, nil
	}
	var previous agenthost.ScriptDomainPage[passbooking.Invitation]
	if err := json.Unmarshal(call.Outcome.Result, &previous); err != nil {
		return false, err
	}
	if previous.Items == nil || len(previous.Items) > scriptDomainPageItems {
		return true, nil
	}
	args := scriptDomainArguments{Event: call.PassRead.Event, Cursor: call.PassRead.Cursor}
	cursor, err := readScriptCursor(args.Cursor, owner, scriptPassInvitations, scriptDomainScope(args))
	if err != nil {
		return false, err
	}
	current, err := b.API.PassInvitations(ctx, owner, args.Event, cursor.Position)
	if err != nil {
		return passPrivacyFailure(err)
	}
	return !agenthost.PassInvitationsPresent(previous.Items, current.Invitations), nil
}
