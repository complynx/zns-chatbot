// Package appclient provides authenticated typed application operations.
package appclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Client carries only user-authenticated application operations.
type Client struct {
	LocalHistory      *LocalHistory
	LocalKnowledge    *LocalKnowledge
	LocalOrders       *LocalOrders
	LocalRegistration *LocalRegistration
	Exchange          TokenExchanger
	Links             TelegramLinks
	Base              string
	// SandboxToken is injected only by sandbox composition; it cannot mint host credentials.
	SandboxToken func(string) string
	// SandboxTelegramOwners is an explicit fixture attestation; nil retains the default actors.
	SandboxTelegramOwners map[int64]string
	HTTP                  *http.Client
}

const (
	apiTimeout      = 10 * time.Second
	invalidJSONCode = "invalid_json"
	// MaxAPIBytes is the JSON response bound, also used by binary error readers.
	MaxAPIBytes = 1 << 20
)

type apiResponseLimitError struct{}

func (*apiResponseLimitError) Error() string { return "core API response limit exceeded" }

// HTTPClient copies caller settings and prevents credentials following redirects.
func (c Client) HTTPClient() http.Client { return boundedHTTPClient(c.HTTP) }
func boundedHTTPClient(transport *http.Client) http.Client {
	client := http.Client{Timeout: apiTimeout}
	if transport != nil {
		client = *transport
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// Call encodes a typed request and performs an owner-authenticated JSON exchange.
func (c Client) Call(ctx context.Context, owner, method, path string, in, out any) error {
	var body []byte
	var e error
	if in != nil {
		body, e = json.Marshal(in)
		if e != nil {
			return e
		}
	}
	return c.Request(ctx, owner, method, path, body, out)
}

// Request obtains a bounded-lifetime user token for the verified principal.
func (c Client) Request(ctx context.Context, owner, method, path string, body []byte, out any) error {
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return err
	}
	return requestToken(ctx, c.Base, c.HTTP, token, method, path, body, out)
}

// requestToken sends an explicitly supplied user or service credential only to
// the configured endpoint. The caller retains the endpoint's audience policy.
func requestToken(
	ctx context.Context,
	base string,
	transport *http.Client,
	token, method, path string,
	body []byte,
	out any,
) error {
	return requestAuthorized(ctx, base, transport, token, "", method, path, body, out)
}

func requestAuthorized(
	ctx context.Context,
	base string,
	transport *http.Client,
	token, hostToken, method, path string,
	body []byte,
	out any,
) error {
	return requestAuthorizedLimit(ctx, base, transport, token, hostToken, method, path, body, out, MaxAPIBytes)
}

func requestAuthorizedLimit(
	ctx context.Context,
	base string,
	transport *http.Client,
	token, hostToken, method, path string,
	body []byte,
	out any,
	responseLimit int64,
) error {
	r, e := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if e != nil {
		return clientBoundaryError(ctx, e, "invalid core API request")
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if hostToken != "" {
		r.Header.Set("X-Zns-Derivation", hostToken)
	}
	client := boundedHTTPClient(transport)
	resp, e := client.Do(r)
	if e != nil {
		return clientBoundaryError(ctx, e, "core API unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return coreResponseError(ctx, resp, "invalid core API response")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if e != nil {
		return clientBoundaryError(ctx, e, "core API response unavailable")
	}
	if int64(len(data)) > responseLimit {
		return oversizedAPIResponse(data, out)
	}
	return json.Unmarshal(data, out)
}

// Called only for non-success responses from the authenticated configured Core.
// A malformed response cannot establish database provenance, even with a header.
func coreResponseError(ctx context.Context, response *http.Response, invalid string) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxAPIBytes+1))
	if err != nil {
		return clientBoundaryError(ctx, err, invalid)
	}
	if len(body) > MaxAPIBytes {
		return errors.New(invalid)
	}
	var problem core.ProblemError
	if err = json.Unmarshal(body, &problem); err != nil {
		return clientBoundaryError(ctx, err, invalid)
	}
	if problem.Code == "" {
		return errors.New(invalid)
	}
	problem.Status = response.StatusCode
	if response.StatusCode >= http.StatusBadRequest && response.Header.Get(core.DatabaseFailureHeader) == "1" {
		return core.DatabaseFailure(&problem)
	}
	return &problem
}

// Keep cancellation observable without exposing wrapped transport or database details.
func clientBoundaryError(ctx context.Context, err error, message string) error {
	if core.IsDatabaseFailure(err) {
		return core.DatabaseFailure(errors.New(message))
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New(message)
}

// Classify only the observed, bounded prefix. A value cut by the byte limit
// may continue, but known syntax/type errors or a second value cannot be hidden.
func oversizedAPIResponse(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	err := decoder.Decode(out)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return &apiResponseLimitError{}
	}
	if err != nil {
		return err
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("core API response contains multiple JSON values")
	}
	return &apiResponseLimitError{}
}
