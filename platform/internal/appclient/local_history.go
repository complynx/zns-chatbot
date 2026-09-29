package appclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// LocalHistory shares conversation transactions without granting Host operations
// through the user client.
type LocalHistory struct {
	Service    conversation.Service
	Authorizer applicationauth.Authorizer
}

func directHistory[T any](ctx context.Context, c Client, owner string,
	operation func(conversation.Service, string) (T, error),
) (T, error) {
	return authenticatedHistory(
		ctx,
		c.LocalHistory,
		c.UserToken,
		owner,
		http.StatusForbidden,
		orderApplicationError,
		operation,
	)
}

func hostHistory[T any](ctx context.Context, c Host, owner string,
	operation func(conversation.Service, string) (T, error),
) (T, error) {
	return authenticatedHistory(
		ctx,
		c.LocalHistory,
		c.UserToken,
		owner,
		http.StatusUnauthorized,
		knowledgeApplicationError,
		operation,
	)
}

func hostHistoryWrite(ctx context.Context, c Host, owner string,
	operation func(conversation.Service, string) error,
) error {
	_, err := hostHistory(ctx, c, owner, func(s conversation.Service, actor string) (bool, error) {
		err := operation(s, actor)
		return err == nil, err
	})
	return err
}

func authenticatedHistory[T any](ctx context.Context, local *LocalHistory,
	tokenFor func(context.Context, string) (string, error), owner string, mismatchStatus int,
	mapDomainError func(error) error,
	operation func(conversation.Service, string) (T, error),
) (T, error) {
	var zero T
	if tokenFor == nil {
		return zero, identity.ErrZitadelIdentity
	}
	token, err := tokenFor(ctx, owner)
	if err != nil {
		if errors.Is(err, identity.ErrZitadelIdentity) || errors.Is(err, identity.ErrZitadelUserInactive) {
			return zero, err
		}
		return zero, orderApplicationError(err)
	}
	actor, err := authorizeOwnerStatus(ctx, local.Authorizer, token, owner, mismatchStatus)
	if err != nil {
		return zero, err
	}
	value, err := operation(local.Service, actor)
	if err != nil {
		return zero, mapDomainError(err)
	}
	return value, nil
}
