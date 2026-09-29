package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

func (c Client) Current(ctx context.Context, owner string) (workflow.Workflow, error) {
	var w workflow.Workflow
	e := c.Call(ctx, owner, "GET", "/v1/workflow", nil, &w)
	return w, e
}

func (c Client) Catalog(ctx context.Context, owner string) ([]workflow.Slot, error) {
	var v []workflow.Slot
	e := c.Call(ctx, owner, "GET", "/v1/catalog", nil, &v)
	return v, e
}

func (c Client) Execute(ctx context.Context, owner string, a workflow.Action) (workflow.Workflow, error) {
	var w workflow.Workflow
	e := c.Call(ctx, owner, "POST", "/v1/actions", a, &w)
	return w, e
}
