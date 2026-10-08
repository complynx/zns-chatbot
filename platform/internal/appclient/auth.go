package appclient

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type TelegramLinks interface {
	Telegram(context.Context, int64) (identity.User, error)
}

type TokenExchanger interface {
	Exchange(context.Context, string) (string, error)
}

type principalKey struct{}

// AuthenticateTelegram accepts only a sender already verified by Telegram polling
// or signed Mini App initData. The principal stays outside model input.
func (c Client) AuthenticateTelegram(ctx context.Context, sender int64) (context.Context, string, error) {
	if c.Exchange == nil && c.Links == nil {
		if c.SandboxTelegramOwners != nil {
			owner, known := c.SandboxTelegramOwners[sender]
			if !known || owner == "" {
				return ctx, "", identity.ErrZitadelIdentity
			}
			return context.WithValue(ctx, principalKey{}, identity.User{Owner: owner}), owner, nil
		}
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
		return ctx, "", clientBoundaryError(ctx, err, "identity lookup unavailable")
	}
	if user.Owner == "" || user.Subject == "" {
		return ctx, "", identity.ErrZitadelIdentity
	}
	return context.WithValue(ctx, principalKey{}, user), user.Owner, nil
}

// NotificationContext resolves a trusted queue/database recipient to the same
// owner before background rendering; it does not accept model-supplied identity.
func (c Client) NotificationContext(ctx context.Context, owner string, telegramID int64) (context.Context, error) {
	if c.Exchange == nil && c.Links == nil && c.SandboxTelegramOwners == nil {
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

// UserToken requires the principal established by AuthenticateTelegram to match
// the requested owner. Configured identity failures never fall back to sandbox.
func (c Client) UserToken(ctx context.Context, owner string) (string, error) {
	if c.Exchange == nil && c.Links == nil {
		if c.SandboxToken == nil {
			return "", identity.ErrZitadelIdentity
		}
		if c.SandboxTelegramOwners != nil {
			user, verified := ctx.Value(principalKey{}).(identity.User)
			if !verified || user.Owner != owner || owner == "" {
				return "", identity.ErrZitadelIdentity
			}
		}
		return c.SandboxToken(owner), nil
	}
	user, ok := ctx.Value(principalKey{}).(identity.User)
	if c.Exchange == nil || c.Links == nil || !ok || user.Owner != owner || user.Subject == "" {
		return "", identity.ErrZitadelIdentity
	}
	return c.Exchange.Exchange(ctx, user.Subject)
}
