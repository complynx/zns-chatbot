package loom

import (
	"context"
	"net/http"
)

// MemoryContext reads the authenticated subject's existing memory without starting a run.
// The caller must never send this private context back to the broadcast author.
func (c *Client) MemoryContext(ctx context.Context, auth Auth) (string, error) {
	var result struct {
		Micro struct {
			Text string `json:"text"`
		} `json:"micro"`
		Index struct {
			Text string `json:"text"`
		} `json:"index"`
	}
	err := c.call(ctx, http.MethodGet, "/users/me/memory", auth, "", nil, &result, false, false)
	return result.Micro.Text + "\n" + result.Index.Text, err
}
