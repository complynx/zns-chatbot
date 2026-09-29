package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserAuthFakeUsernameOptionalAndClearing(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "/start", "username": "alice"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "/start"})
	updates, err := f.b.TG.Updates(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, updates, 2)
	assert.Equal(t, "alice", updates[0].Message.From.Username)
	assert.Empty(t, updates[1].Message.From.Username)
}
