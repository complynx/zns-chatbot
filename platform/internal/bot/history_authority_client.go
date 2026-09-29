package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (b *Bot) readAuthoritiesChanged(
	ctx context.Context,
	owner string,
	authorities []readsource.Authority,
) (bool, error) {
	return agenthost.ReadAuthoritiesChanged(ctx, b.Host, owner, authorities)
}

const historyStale = "history_stale"
