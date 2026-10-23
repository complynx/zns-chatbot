package task

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestStartExistingRunAndFrozenCreationKeepLegacyPresentation(t *testing.T) {
	t.Parallel()
	service := &Service{}
	for _, existing := range []struct {
		task       db.Task
		checkpoint checkpoint
	}{
		{task: db.Task{RunID: "existing-run"}},
		{checkpoint: checkpoint{Create: &loom.CreateRunRequest{}}},
	} {
		ready, err := service.prepareStart(t.Context(), &existing.task, existing.checkpoint,
			intent{Kind: kindMessage, Text: startCommand})
		require.NoError(t, err)
		require.True(t, ready)
	}
}
