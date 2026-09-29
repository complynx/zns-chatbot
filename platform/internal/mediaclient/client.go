// Package mediaclient calls the operator-configured media broker over authenticated HTTP.
// It uses wire result types only; decoding and native media tools remain in the worker.
package mediaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

type Kind string

const (
	Audio            Kind = "audio"
	Voice            Kind = "voice"
	Video            Kind = "video"
	VideoNote        Kind = "video_note"
	maxResponseBytes      = 4 << 20
	requestTimeout        = 160 * time.Second
	maxDurationMS         = 240000
	maxFrames             = 8
)

var (
	ErrConfiguration = errors.New("invalid media worker configuration")
	ErrInput         = errors.New("invalid media worker input")
	ErrResponse      = errors.New("invalid media worker response")
)

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("media worker HTTP status %d", e.Status) }

// Config is operator-owned. Request data cannot change the worker destination.
// HTTP supplies only the transport; this client enforces its timeout and no redirects.
type Config struct {
	CreditsEnforce bool
	URL            string
	Secret         string
	HTTP           *http.Client
}
type Client struct {
	creditsEnforce bool
	base           url.URL
	secret         string
	http           http.Client
}

func New(config Config) (*Client, error) {
	base, err := url.Parse(config.URL)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") ||
		strings.TrimSpace(config.Secret) == "" || strings.ContainsAny(config.Secret, "\r\n") {
		return nil, ErrConfiguration
	}
	client := http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if config.HTTP != nil {
		client.Transport = config.HTTP.Transport
	}
	return &Client{base: *base, secret: config.Secret, http: client, creditsEnforce: config.CreditsEnforce}, nil
}

type Range struct {
	StartMS, EndMS int64
	Count          int
}

func (c *Client) Preprocess(ctx context.Context, kind Kind, body []byte) (mediaproc.Result, error) {
	return c.call(ctx, kind, body, nil)
}

// Storyboard extracts a bounded video range without invoking speech recognition.
func (c *Client) Storyboard(ctx context.Context, kind Kind, body []byte, selection Range) (mediaproc.Result, error) {
	if (kind != Video && kind != VideoNote) || selection.StartMS < 0 || selection.EndMS <= selection.StartMS ||
		selection.EndMS > maxDurationMS || selection.Count < 1 || selection.Count > maxFrames {
		return mediaproc.Result{}, ErrInput
	}
	return c.call(ctx, kind, body, &selection)
}

func (c *Client) call(ctx context.Context, kind Kind, body []byte, selection *Range) (mediaproc.Result, error) {
	if (kind != Audio && kind != Voice && kind != Video && kind != VideoNote) || len(body) == 0 ||
		len(body) > mediaproc.MaxInputBytes {
		return mediaproc.Result{}, ErrInput
	}
	endpoint := c.base
	endpoint.Path = "/v1/preprocess"
	query := url.Values{"kind": {string(kind)}}
	if selection != nil {
		endpoint.Path = "/v1/storyboard"
		query.Set("start_ms", strconv.FormatInt(selection.StartMS, 10))
		query.Set("end_ms", strconv.FormatInt(selection.EndMS, 10))
		query.Set("count", strconv.Itoa(selection.Count))
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return mediaproc.Result{}, ErrInput
	}
	request.Header.Set("Authorization", "Bearer "+c.secret)
	request.Header.Set("Content-Type", "application/octet-stream")
	credits.SetRequestScope(request)
	credits.SetRequestMode(request, c.creditsEnforce)
	response, err := c.http.Do(request)
	if err != nil {
		return mediaproc.Result{}, fmt.Errorf("call media worker: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return mediaproc.Result{}, &HTTPError{Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return mediaproc.Result{}, fmt.Errorf("read media worker response: %w", err)
	}
	if len(data) > maxResponseBytes || !utf8.Valid(data) {
		return mediaproc.Result{}, ErrResponse
	}
	var result mediaproc.Result
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || !validResult(result, kind, selection) {
		return mediaproc.Result{}, ErrResponse
	}
	return result, nil
}
