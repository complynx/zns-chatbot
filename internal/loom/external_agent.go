package loom

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const externalAgentProfilePath = "/users/me/external-agent"
const ExternalAgentProfilePermission = "external_agent.profile.write"

type ExternalAgentCapability struct {
	Resource      string   `json:"resource"`
	Access        []string `json:"access"`
	TokenServices []string `json:"token_services"`
	Version       int      `json:"version"`
}

// ExternalAgentProfile is the user's consent and its compare-and-swap version.
type ExternalAgentProfile struct {
	Enabled bool  `json:"enabled"`
	History bool  `json:"history"`
	Memory  bool  `json:"memory"`
	Skills  bool  `json:"skills"`
	Writes  bool  `json:"writes,omitempty"`
	Version int64 `json:"version"`
}

type ExternalApproval struct {
	ID          string          `json:"operation_id"`
	Subject     string          `json:"subject"`
	OrgID       string          `json:"organization"`
	Client      string          `json:"client"`
	Service     string          `json:"service"`
	Module      string          `json:"module"`
	Method      string          `json:"method"`
	Description string          `json:"description"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	Remember    bool            `json:"remember"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

func (c *Client) DecideExternalApproval(ctx context.Context, auth Auth, id, choice string) (string, error) {
	if auth.SubjectToken == "" || auth.SessionID != "" {
		return "", &Error{Outcome: NotStarted}
	}
	path, err := resourcePath("users/me/external-agent/approvals", id, "/decision")
	if err != nil {
		return "", err
	}
	var result struct {
		Status string `json:"status"`
	}
	err = c.call(ctx, http.MethodPost, path, auth, "", struct {
		Choice string `json:"choice"`
	}{choice}, &result, true, false)
	return result.Status, err
}

// ExternalAgent reads the supported MCP capability.
func (c *Client) ExternalAgent(ctx context.Context, auth Auth) (*ExternalAgentCapability, error) {
	var result struct {
		ExternalAgent *ExternalAgentCapability `json:"external_agent"`
	}
	if err := c.call(ctx, http.MethodGet, "/capabilities", auth, "", nil, &result, false, false); err != nil {
		return nil, err
	}
	capability := result.ExternalAgent
	if capability == nil || capability.Version != 1 || !validExternalAgentResource(capability.Resource) {
		return nil, &Error{Code: "external_agent_unsupported", Outcome: NotStarted}
	}
	return capability, nil
}

func validExternalAgentResource(resource string) bool {
	u, err := url.Parse(resource)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path == "" || strings.HasSuffix(u.Path, "/") ||
		u.RawQuery != "" || u.ForceQuery || strings.Contains(resource, "#") || strings.ContainsAny(resource, " \t\r\n") {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "https" ||
		u.Scheme == "http" && (host == "localhost" || net.ParseIP(host).IsLoopback())
}

func (c *Client) ExternalAgentProfile(ctx context.Context, auth Auth) (ExternalAgentProfile, error) {
	var result ExternalAgentProfile
	if auth.SubjectToken == "" || auth.SessionID != "" {
		return result, &Error{Outcome: NotStarted}
	}
	err := c.call(ctx, http.MethodGet, externalAgentProfilePath, auth, "", nil, &result, false, false)
	return result, err
}

// PutExternalAgentProfile replaces consent only at the version in profile.
func (c *Client) PutExternalAgentProfile(
	ctx context.Context, auth Auth, profile ExternalAgentProfile,
) (ExternalAgentProfile, error) {
	var result ExternalAgentProfile
	if auth.SubjectToken == "" || auth.SessionID != "" || profile.Version < 0 {
		return result, &Error{Outcome: NotStarted}
	}
	err := c.call(ctx, http.MethodPut, externalAgentProfilePath, auth, "", profile, &result, true, false)
	profile.Version++
	if err == nil && result != profile {
		err = &Error{Outcome: Unknown}
	}
	return result, err
}
