package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type TelegramLinks interface {
	Telegram(context.Context, int64) (identity.User, error)
}

type TokenExchanger interface {
	Exchange(context.Context, string) (string, error)
}

type principalKey struct{}

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
			if errors.Is(err, ErrProvisioningDenied) {
				b.denyOnboarding(ctx, in, update)
				return ctx, in, false, nil
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
		_, err = b.API.userToken(ctx, owner)
		if errors.Is(err, identity.ErrZitadelUserInactive) {
			b.denyOnboarding(ctx, in, update)
			return ctx, in, false, nil
		}
	}
	in.owner = owner
	return ctx, in, err == nil, err
}

// AuthenticateTelegram accepts only a sender already verified by Telegram polling
// or signed Mini App initData. The principal stays outside model input.
func (c APIClient) AuthenticateTelegram(ctx context.Context, sender int64) (context.Context, string, error) {
	if c.Exchange == nil && c.Links == nil {
		owner, ok := identity.Subject(sender)
		if !ok {
			return ctx, "", identity.ErrZitadelIdentity
		}
		return ctx, owner, nil
	}
	if c.Exchange == nil || c.Links == nil {
		return ctx, "", identity.ErrZitadelIdentity
	}
	user, err := c.Links.Telegram(ctx, sender)
	if err != nil {
		if errors.Is(err, identity.ErrZitadelIdentity) {
			return ctx, "", identity.ErrZitadelIdentity
		}
		return ctx, "", errors.New("identity lookup unavailable")
	}
	if user.Owner == "" || user.Subject == "" {
		return ctx, "", identity.ErrZitadelIdentity
	}
	return context.WithValue(ctx, principalKey{}, user), user.Owner, nil
}

// Background recipients come only from the authenticated service queue or the
// durable database. They must resolve to the same owner before rendering cards.
func (c APIClient) notificationContext(ctx context.Context, owner string, telegramID int64) (context.Context, error) {
	if c.Exchange == nil && c.Links == nil {
		return ctx, nil
	}
	resolved, recipient, err := c.AuthenticateTelegram(ctx, telegramID)
	if err != nil {
		return ctx, err
	}
	if recipient != owner {
		return ctx, identity.ErrZitadelIdentity
	}
	return resolved, nil
}

func (c APIClient) userToken(ctx context.Context, owner string) (string, error) {
	if c.Exchange == nil && c.Links == nil {
		return c.Signer.Token(owner), nil
	}
	user, ok := ctx.Value(principalKey{}).(identity.User)
	if c.Exchange == nil || c.Links == nil || !ok || user.Owner != owner || user.Subject == "" {
		return "", identity.ErrZitadelIdentity
	}
	return c.Exchange.Exchange(ctx, user.Subject)
}
