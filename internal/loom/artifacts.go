package loom

import (
	"bytes"
	"context"
	"net/http"
)

// CreateChat isolates a service procedure from the user's conversation and memory history.
// This route has no replay contract: persist a pending marker before calling it.
func (c *Client) CreateChat(ctx context.Context, auth Auth) (string, error) {
	var result struct {
		ID string `json:"id"`
	}
	err := c.call(ctx, http.MethodPost, "/chats", auth, "", struct{}{}, &result, true, false)
	if err == nil && result.ID == "" {
		return "", &Error{Outcome: Unknown}
	}
	return result.ID, err
}

// UploadArtifact sends bounded bytes only. It never accepts a URL or follows redirects.
// Like chat creation, unknown upload outcomes require observation, not automatic replay.
func (c *Client) UploadArtifact(ctx context.Context, auth Auth, mime string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 20*1024*1024 || mime == "" || auth.ServiceToken == "" ||
		auth.SubjectToken != "" && auth.SessionID != "" {
		return "", &Error{Outcome: NotStarted}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/artifacts", bytes.NewReader(raw))
	if err != nil {
		return "", &Error{Outcome: NotStarted}
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+auth.ServiceToken)
	if auth.SubjectToken != "" {
		req.Header.Set("X-Subject-Token", auth.SubjectToken)
	}
	if auth.SessionID != "" {
		req.Header.Set("X-Auth-Session-Id", auth.SessionID)
	}
	req.Header.Set("Content-Type", mime)
	response, err := c.http.Do(req)
	if err != nil {
		return "", &Error{Outcome: Unknown}
	}
	defer response.Body.Close()
	var result struct {
		ID string `json:"artifact_id"`
	}
	err = decodeResponse(response, &result, true, false)
	if err == nil && result.ID == "" {
		return "", &Error{Outcome: Unknown}
	}
	return result.ID, err
}
