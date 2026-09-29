package conversation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func TestArchivePrivacyDoesNotRouteBusinessIntent(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"My name is Avery Example", "Passport: TEST-DOCUMENT", "password=private", "Bearer abcdef"} {
		value, omitted := conversation.Sanitize(text)
		assert.True(t, omitted)
		assert.NotEqual(t, text, value)
	}
	value, omitted := conversation.Sanitize("Please add a shuttle")
	assert.False(t, omitted)
	assert.Equal(t, "Please add a shuttle", value)
}
