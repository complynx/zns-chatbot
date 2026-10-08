package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// Discovery can reveal private membership even when event metadata is public.
// The Core marks the access basis at read time; cached metadata is not authority.
func (b *Bot) scriptPassDiscoveryChanged(
	ctx context.Context,
	owner string,
	outcome agent.ScriptToolResult,
) (bool, error) {
	events, invalid, err := agenthost.PassDiscoveryEvents(outcome)
	if err != nil || invalid || len(events) == 0 {
		return invalid, err
	}
	owned, err := b.API.OwnsPassEvents(ctx, owner, events)
	if err != nil {
		return passPrivacyFailure(err)
	}
	return !owned, nil
}
