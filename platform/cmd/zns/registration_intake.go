package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// nativeRegistrationAuthorizer uses the same live identity chain as a native
// operation. The callback runs outside the registration transaction.
func nativeRegistrationAuthorizer(
	db *pgxpool.Pool,
	botID int64,
	verify api.VerifyOwner,
	adapter *identity.Zitadel,
	links identity.Links,
	signer identity.Signer,
) derivedmutation.NativeRegistrationAuthorizer {
	client := appclient.Client{}
	if adapter != nil {
		client.Exchange = adapter
		client.Links = links
	} else {
		client.SandboxToken = signer.Token
	}
	authorizer := applicationauth.Authorizer{DB: db, Verify: applicationauth.VerifyOwner(verify)}
	return func(ctx context.Context, owner string, receivedBot, sender int64) (bool, error) {
		if receivedBot != botID {
			return false, nil
		}
		live, err := client.NotificationContext(ctx, owner, sender)
		if err != nil {
			return nativeIdentityResult(err)
		}
		token, err := client.UserToken(live, owner)
		if err != nil {
			return nativeIdentityResult(err)
		}
		principal, err := authorizer.Authorize(live, token)
		if err != nil {
			return nativeIdentityResult(err)
		}
		return principal.Owner() == owner, nil
	}
}

func nativeIdentityResult(err error) (bool, error) {
	if errors.Is(err, identity.ErrZitadelIdentity) || errors.Is(err, identity.ErrZitadelUserInactive) ||
		errors.Is(err, identity.ErrSandboxIdentity) || errors.Is(err, applicationauth.ErrUnauthorized) ||
		errors.Is(err, applicationauth.ErrForbidden) {
		return false, nil
	}
	return false, err
}
