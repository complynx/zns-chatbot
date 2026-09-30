package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// Host contains capabilities available only to trusted runtime adapters. User
// revalidation remains mandatory for host operations acting on a user's sources.
type Host struct {
	LocalMemoryReadState *LocalMemoryReadState
	LocalBotDelivery     *LocalBotDelivery
	LocalHistory         *LocalHistory
	LocalKnowledge       *LocalKnowledge
	LocalDerived         *LocalDerived
	Base                 string
	HTTP                 *http.Client
	Signer               identity.Signer
	UserToken            func(context.Context, string) (string, error)
}

func (c Host) requestToken(ctx context.Context, token, method, path string, body []byte, out any) error {
	return requestToken(ctx, c.Base, c.HTTP, token, method, path, body, out)
}
