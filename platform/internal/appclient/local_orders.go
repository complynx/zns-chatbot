package appclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// LocalOrders is the typed boundary used when the order service shares the process.
// It never accepts a previously verified principal as an authorization grant.
type LocalOrders struct {
	Service    orders.Service
	Authorizer applicationauth.Authorizer
}

func directOrder[T any](
	ctx context.Context,
	c Client,
	owner string,
	operation func(orders.Service, string) (T, error),
) (T, error) {
	var zero T
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return zero, err
	}
	actor, err := authorizeOwner(ctx, c.LocalOrders.Authorizer, token, owner)
	if err != nil {
		return zero, err
	}
	result, err := operation(c.LocalOrders.Service, actor)
	if err != nil {
		return zero, orderApplicationError(err)
	}
	return result, nil
}

func orderApplicationError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, known := errors.AsType[*core.ProblemError](err); known {
		return err
	}
	public := &core.ProblemError{Status: http.StatusInternalServerError, Code: "internal_error"}
	if core.IsDatabaseFailure(err) {
		return core.DatabaseFailure(public)
	}
	return public
}

func (c Client) localOrderPages(ctx context.Context, owner, event string, inbox bool) ([]orders.Order, error) {
	result := []orders.Order{}
	cursor := ""
	for {
		page, err := directOrder(ctx, c, owner, func(s orders.Service, actor string) (orders.Page, error) {
			return s.ListPage(ctx, actor, event, cursor, inbox)
		})
		if err != nil {
			return nil, err
		}
		result = append(result, page.Orders...)
		if page.Next == "" {
			return result, nil
		}
		cursor = page.Next
	}
}

func authorizeOwner(ctx context.Context, authorizer applicationauth.Authorizer, token, owner string) (string, error) {
	return authorizeOwnerStatus(ctx, authorizer, token, owner, http.StatusForbidden)
}

func authorizeOwnerStatus(
	ctx context.Context,
	authorizer applicationauth.Authorizer,
	token, owner string,
	mismatchStatus int,
) (string, error) {
	principal, err := authorizer.Authorize(ctx, token)
	if errors.Is(err, applicationauth.ErrUnauthorized) {
		return "", &core.ProblemError{Status: http.StatusUnauthorized, Code: "unauthorized"}
	}
	if errors.Is(err, applicationauth.ErrForbidden) {
		return "", &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	if err != nil {
		return "", orderApplicationError(err)
	}
	if principal.Owner() != owner {
		code := "forbidden"
		if mismatchStatus == http.StatusUnauthorized {
			code = "unauthorized"
		}
		return "", &core.ProblemError{Status: mismatchStatus, Code: code}
	}

	return principal.Owner(), nil
}
