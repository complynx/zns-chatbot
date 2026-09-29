package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type APIClient struct {
	Exchange TokenExchanger
	Links    TelegramLinks
	Base     string
	Signer   identity.Signer
	HTTP     *http.Client
}

const (
	apiTimeout  = 10 * time.Second
	maxAPIBytes = 1 << 20
)

type apiResponseLimitError struct{}

func (*apiResponseLimitError) Error() string { return "core API response limit exceeded" }

// Core API credentials are scoped to the configured endpoint, including metadata
// and service requests. Copy the client so callers retain their own policy.
func (c APIClient) httpClient() http.Client {
	client := http.Client{Timeout: apiTimeout}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

func (c APIClient) call(ctx context.Context, owner, method, path string, in, out any) error {
	var body []byte
	var e error
	if in != nil {
		body, e = json.Marshal(in)
		if e != nil {
			return e
		}
	}
	return c.request(ctx, owner, method, path, body, out)
}

func (c APIClient) request(ctx context.Context, owner, method, path string, body []byte, out any) error {
	token, err := c.userToken(ctx, owner)
	if err != nil {
		return err
	}
	return c.requestToken(ctx, token, method, path, body, out)
}

func (c APIClient) requestToken(ctx context.Context, token, method, path string, body []byte, out any) error {
	r, e := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(body))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	client := c.httpClient()
	resp, e := client.Do(r)
	if e != nil {
		return fmt.Errorf("core API unavailable")
	}
	defer resp.Body.Close()
	d := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBytes))
	if resp.StatusCode != http.StatusOK {
		var p core.ProblemError
		if e = d.Decode(&p); e != nil {
			return e
		}
		p.Status = resp.StatusCode
		return &p
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, maxAPIBytes+1))
	if e != nil {
		return e
	}
	if len(data) > maxAPIBytes {
		return oversizedAPIResponse(data, out)
	}
	return json.Unmarshal(data, out)
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
func (c APIClient) Current(ctx context.Context, owner string) (core.Workflow, error) {
	var w core.Workflow
	e := c.call(ctx, owner, "GET", "/v1/workflow", nil, &w)
	return w, e
}
func (c APIClient) Catalog(ctx context.Context, owner string) ([]core.Slot, error) {
	var v []core.Slot
	e := c.call(ctx, owner, "GET", "/v1/catalog", nil, &v)
	return v, e
}
func (c APIClient) Execute(ctx context.Context, owner string, a core.Action) (core.Workflow, error) {
	var w core.Workflow
	e := c.call(ctx, owner, "POST", "/v1/actions", a, &w)
	return w, e
}
