package agenthost

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type SourceAuthority interface {
	CheckReadAuthorities(context.Context, string, []readsource.Authority) error
}

func ReadAuthoritiesChanged(
	ctx context.Context,
	authority SourceAuthority,
	owner string,
	authorities []readsource.Authority,
) (bool, error) {
	if len(authorities) == 0 {
		return false, nil
	}
	err := authority.CheckReadAuthorities(ctx, owner, authorities)
	var problem *core.ProblemError
	if errors.As(err, &problem) && problem.Code == "history_stale" {
		return true, nil
	}
	return false, err
}

func (h HistoryReader) checkSnapshot(
	ctx context.Context,
	owner string,
	expected int64,
	authorities []readsource.Authority,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	generation, err := h.Domain.HistoryGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if generation != expected {
		return appclient.ErrReadStale
	}
	changed, err := ReadAuthoritiesChanged(ctx, h.Authority, owner, authorities)
	if err != nil {
		return err
	}
	if changed {
		return appclient.ErrReadStale
	}
	return nil
}

// RefreshHistory retires transformed results when their generation or sources
// no longer authorize exposure. Domain leaves retain their authorization rules.
func (h HistoryReader) RefreshHistory(ctx context.Context, owner string, input *agent.Input) error {
	if input.Conversation == nil && input.Script == nil {
		return nil
	}
	generation, err := h.Domain.HistoryGeneration(ctx, owner)
	if err != nil {
		return err
	}
	authorities, err := HistoryInputReadAuthorities(input)
	if err != nil {
		return err
	}
	revoked, err := ReadAuthoritiesChanged(ctx, h.Authority, owner, authorities)
	if err != nil {
		return err
	}
	if input.HistoryGeneration != generation || revoked {
		ClearRevokedHistoryContext(input, generation, revoked)
	}
	return nil
}
