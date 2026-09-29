package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestLabContactRejectsNonSyntheticAndMixedMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		contact, forward int64
		hidden           bool
		callback         string
		valid            bool
	}{
		{name: "plain", valid: true},
		{name: "contact", contact: 202, valid: true},
		{name: "forward", forward: 202, valid: true},
		{name: "hidden", forward: 202, hidden: true, valid: true},
		{name: "foreign", contact: 99999},
		{name: "negative", forward: -202},
		{name: "mixed", contact: 101, forward: 202},
		{name: "callback", contact: 101, callback: "button"},
		{name: "missing_hidden_sender", hidden: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.valid, validLabIdentity(tc.contact, tc.forward, tc.hidden, tc.callback))
		})
	}
	var message telegram.Message
	setLabIdentity(&message, 0, 202, true)
	assert.Equal(t, "hidden_user", message.ForwardOrigin.Type)
	assert.Nil(t, message.ForwardOrigin.SenderUser)
}
