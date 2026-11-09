package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// CheckModels authenticates the caller by reading its catalog. No chat, session,
// run or provider request is created. Loom may provision the machine's own user.
func (c *Client) CheckModels(ctx context.Context, token string) error {
	var reply struct {
		Models *[]json.RawMessage `json:"models"`
	}
	if err := c.call(ctx, http.MethodGet, "/models", Auth{ServiceToken: token}, "", nil, &reply,
		false, false); err != nil {
		return err
	}
	if reply.Models == nil {
		return errors.New("loom model catalog is missing")
	}
	return nil
}
