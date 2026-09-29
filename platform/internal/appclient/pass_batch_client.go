package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// RunPassBatch preserves any source stored by the original agent-started batch.
func (c Client) RunPassBatch(
	ctx context.Context,
	owner string,
	command passbooking.RuntimeBatch,
) ([]passbooking.RuntimeBatchItem, error) {
	if c.LocalRegistration != nil {
		actor, err := c.registrationActor(ctx, owner)
		if err != nil {
			return nil, err
		}
		value, err := c.LocalRegistration.Batches.RunManualPassBatch(ctx, actor, command)
		if err != nil {
			return nil, orderApplicationError(err)
		}
		return value, nil
	}
	var result []passbooking.RuntimeBatchItem
	err := c.Call(ctx, owner, http.MethodPost, "/v1/passes/batches", command, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
