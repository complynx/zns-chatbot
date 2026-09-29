package miniapp

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

var errOnboardingUnavailable = errors.New("identity provisioning unavailable")

func (g Gateway) onboardVerified(ctx context.Context, user telegram.User) error {
	if user.ID <= 0 || user.ID >= 1<<52 || user.IsBot {
		return identity.ErrZitadelIdentity
	}
	if g.Onboarding == nil {
		return nil
	}
	_, _, err := g.API.AuthenticateTelegram(ctx, user.ID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, identity.ErrZitadelIdentity) {
		return errOnboardingUnavailable
	}
	if g.Onboarding(ctx, user) != nil {
		return errOnboardingUnavailable
	}
	return nil
}
