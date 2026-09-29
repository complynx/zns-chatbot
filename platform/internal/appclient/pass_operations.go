package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// ReadPassOperation authorizes one ledger admission using fresh delegated identity.
// The coordinator supplies typed domain parameters, never a serialized script.
func (c Host) ReadPassOperation(
	ctx context.Context,
	owner string,
	input derivedmutation.PassOperationInput,
) (derivedmutation.PassOperationRead, error) {
	if err := input.Validate(); err != nil {
		return derivedmutation.PassOperationRead{}, orderApplicationError(err)
	}
	if c.LocalDerived != nil {
		return directDerived(
			ctx,
			c,
			owner,
			func(s derivedmutation.Service, actor string) (derivedmutation.PassOperationRead, error) {
				return s.ReadPassOperation(ctx, actor, input)
			},
		)
	}
	if c.UserToken == nil {
		return derivedmutation.PassOperationRead{}, identity.ErrZitadelIdentity
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return derivedmutation.PassOperationRead{}, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return derivedmutation.PassOperationRead{}, err
	}
	var result derivedmutation.PassOperationRead
	err = requestAuthorizedLimit(ctx, c.Base, c.HTTP, token, c.Signer.DerivedMutationToken(owner), http.MethodPost,
		"/internal/derived/pass-operation", body, &result, MaxAPIBytes)
	if err != nil {
		return derivedmutation.PassOperationRead{}, err
	}
	return result, nil
}
