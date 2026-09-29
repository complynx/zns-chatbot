package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) authenticatedUpdate(
	ctx context.Context,
	update telegram.Update,
) (context.Context, incoming, bool, error) {
	in, accepted := parseUpdate(update)
	if !accepted {
		return ctx, in, false, nil
	}
	ctx, owner, err := b.API.AuthenticateTelegram(ctx, in.chat)
	if errors.Is(err, identity.ErrZitadelIdentity) && b.Onboarding != nil {
		if err = b.onboardIncoming(ctx, update); err != nil {
			if errors.Is(err, appclient.ErrProvisioningDenied) {
				return ctx, in, false, b.denyOnboarding(ctx, in, update)
			}
			return ctx, in, false, err
		}
		ctx, owner, err = b.API.AuthenticateTelegram(ctx, in.chat)
	}
	if errors.Is(err, identity.ErrZitadelIdentity) {
		return ctx, in, false, nil
	}
	// Local links do not prove that the provider still permits this user. Check
	// before recording private input or invoking any business operation.
	if err == nil && b.API.Exchange != nil {
		_, err = b.API.UserToken(ctx, owner)
		if errors.Is(err, identity.ErrZitadelUserInactive) {
			return ctx, in, false, b.denyOnboarding(ctx, in, update)
		}
	}
	in.owner = owner
	return ctx, in, err == nil, err
}
