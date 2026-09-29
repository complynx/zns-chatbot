package sandbox

import (
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func validLabIdentity(contact, forward int64, hidden bool, callback string) bool {
	if contact < 0 || forward < 0 || (contact != 0 && forward != 0) || (hidden && forward == 0) {
		return false
	}
	if callback != "" && (contact != 0 || forward != 0) {
		return false
	}
	for _, id := range []int64{contact, forward} {
		if id != 0 {
			if _, known := identity.Subject(id); !known {
				return false
			}
		}
	}
	return true
}

func setLabIdentity(message *telegram.Message, contact, forward int64, hidden bool) {
	if contact != 0 {
		message.Contact = &telegram.Contact{UserID: contact, FirstName: strconv.FormatInt(contact, 10)}
	}
	if forward == 0 {
		return
	}
	message.ForwardOrigin = &telegram.ForwardOrigin{Type: "hidden_user"}
	if !hidden {
		message.ForwardOrigin = &telegram.ForwardOrigin{
			Type:       "user",
			SenderUser: &telegram.User{ID: forward, FirstName: strconv.FormatInt(forward, 10)},
		}
	}
}
