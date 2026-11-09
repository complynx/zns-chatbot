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

// CheckReady reads public readiness without authenticating the marker-only actor.
// The configured HTTP timeout and bounded response decoder also apply to this probe.
func (c *Client) CheckReady(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/readyz", nil)
	if err != nil {
		return &Error{Outcome: NotStarted}
	}
	response, err := c.http.Do(req)
	if err != nil {
		return &Error{Outcome: Rejected}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &Error{Outcome: Rejected, StatusCode: response.StatusCode}
	}
	var reply struct {
		Status string `json:"status"`
	}
	if err = decodeResponse(response, &reply, false, false); err != nil {
		return err
	}
	if reply.Status != "ready" {
		return errors.New("loom is not ready")
	}
	return nil
}
