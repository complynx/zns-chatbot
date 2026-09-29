package agenthost

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

type deadlineScriptCatalog struct {
	liveScriptCatalog

	deadline time.Time
	done     <-chan struct{}
	block    bool
}

func (c *deadlineScriptCatalog) Capabilities(ctx context.Context, _ string) (core.BusinessCapabilities, error) {
	c.deadline, _ = ctx.Deadline()
	c.done = ctx.Done()
	if c.block {
		<-ctx.Done()
		return core.BusinessCapabilities{}, ctx.Err()
	}
	return core.BusinessCapabilities{}, nil
}

func TestInitialCatalogHasFiniteFreshOperationContext(t *testing.T) {
	t.Parallel()
	catalog := &deadlineScriptCatalog{}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: catalog}}
	started := time.Now()
	_, err := host.initialTools(t.Context(), "owner")
	require.NoError(t, err)
	require.WithinDuration(t, started.Add(scriptprotocol.HostCallTimeout), catalog.deadline, time.Second)
	select {
	case <-catalog.done:
	default:
		t.Fatal("catalog context outlived operation")
	}
	parent, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	deadline, _ := parent.Deadline()
	catalog.block = true
	_, err = host.initialTools(parent, "owner")
	require.Error(t, err)
	require.Equal(t, deadline, catalog.deadline)
	require.ErrorIs(t, parent.Err(), context.DeadlineExceeded)
}
