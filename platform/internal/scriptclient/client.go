// Package scriptclient sends bounded data-only scripts to an isolated Unix worker.
// It does not import a JavaScript VM or expose application tools to scripts.
package scriptclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

const (
	maxCode     = 16 << 10
	maxInput    = 128 << 10
	maxOutput   = 64 << 10
	maxResponse = maxOutput + 64
)

type Request struct {
	Code  string          `json:"code"`
	Input json.RawMessage `json:"input"`
}

type Client struct {
	http   *http.Client
	socket string
}

func New(socket string) (*Client, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("script socket must be absolute")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	const timeout = scriptprotocol.EvaluateClientTimeout
	return &Client{socket: socket, http: &http.Client{Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) Evaluate(ctx context.Context, in Request) (json.RawMessage, error) {
	if len(in.Code) == 0 || len(in.Code) > maxCode || !utf8.ValidString(in.Code) ||
		len(in.Input) == 0 || len(in.Input) > maxInput || !utf8.Valid(in.Input) || !json.Valid(in.Input) {
		return nil, errors.New("invalid script input")
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, errors.New("invalid script input")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://script/evaluate", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("script worker unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("script worker rejected request")
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if len(data) > maxResponse {
		return nil, ErrResultLimit
	}
	if err != nil {
		return nil, errors.New("script response unavailable")
	}
	return decode(data)
}

func decode(data []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidResult
	}
	// Success and failure are mutually exclusive, single-key objects.
	key, err := decoder.Token()
	if err != nil || (key != "result" && key != "error") {
		return nil, ErrInvalidResult
	}
	var value json.RawMessage
	if decoder.Decode(&value) != nil || decoder.More() {
		return nil, ErrInvalidResult
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalidResult
	}
	if !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return nil, ErrInvalidResult
	}
	if key == "error" {
		return nil, workerError(value)
	}
	if len(value) > maxOutput {
		return nil, ErrResultLimit
	}
	if !utf8.Valid(value) {
		return nil, ErrInvalidResult
	}
	return value, nil
}
