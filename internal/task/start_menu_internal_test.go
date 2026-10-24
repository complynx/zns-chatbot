package task

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestNonStartCompletionDoesNotPresentWelcome(t *testing.T) {
	t.Parallel()
	service := &Service{}
	out, err := service.completedStartWelcome(
		t.Context(),
		&db.Task{},
		checkpoint{},
		intent{Kind: kindMessage, Text: "hello"},
	)
	require.NoError(t, err)
	require.Empty(t, out)
}
