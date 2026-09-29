// Package stickerclient calls the isolated sticker broker and validates its output.
package stickerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/stickermedia"
)

const (
	Path                 = "/v1/normalize"
	DecoderAuthorization = "unix-local-sticker-decoder"
	Timeout              = 20 * time.Second
	maxResponseBytes     = 12 << 20
	maxFrameBytes        = 2 << 20
	maxSide              = 512
	maxFrames            = 4
	maxDuration          = 3 * time.Second
)

var ErrResponse = errors.New("invalid sticker decoder response")

type Client struct {
	endpoint string
	secret   string
	http     *http.Client
}

func New(endpoint, secret string) (*Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") ||
		secret == "" {
		return nil, errors.New("sticker client requires an HTTP origin and secret")
	}
	parsed.Path = Path
	return &Client{endpoint: parsed.String(), secret: secret, http: newHTTPClient(http.DefaultTransport)}, nil
}

func NewUnix(socket string) (*Client, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("sticker socket must be absolute")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}}
	return &Client{endpoint: "http://decoder" + Path, secret: DecoderAuthorization, http: newHTTPClient(transport)}, nil
}

func newHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport:     transport,
		Timeout:       Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (client *Client) Normalize(
	ctx context.Context,
	data []byte,
	format stickermedia.Format,
) ([]stickermedia.Frame, error) {
	if !ValidFormat(format) || len(data) == 0 {
		return nil, stickermedia.ErrInvalid
	}
	if len(data) > stickermedia.MaxInputBytes {
		return nil, stickermedia.ErrLimit
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.endpoint+"?format="+string(format),
		bytes.NewReader(data),
	)
	if err != nil {
		return nil, fmt.Errorf("sticker request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.secret)
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sticker transport: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sticker decoder HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read sticker response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, ErrResponse
	}
	frames, err := decodeFrames(body)
	if err != nil || !ValidFrames(frames, format) {
		return nil, ErrResponse
	}
	return frames, nil
}

func decodeFrames(body []byte) ([]stickermedia.Frame, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, ErrResponse
	}
	frames := make([]stickermedia.Frame, 0, maxFrames)
	for decoder.More() {
		if len(frames) == maxFrames {
			return nil, ErrResponse
		}
		var frame stickermedia.Frame
		if decoder.Decode(&frame) != nil {
			return nil, ErrResponse
		}
		frames = append(frames, frame)
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim(']') {
		return nil, ErrResponse
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, ErrResponse
	}
	return frames, nil
}

func ValidFormat(format stickermedia.Format) bool {
	return format == stickermedia.WebP || format == stickermedia.TGS || format == stickermedia.WebM
}

// ValidFrames fully decodes bounded PNGs; headers alone do not establish valid images.
func ValidFrames(frames []stickermedia.Frame, format stickermedia.Format) bool {
	if !ValidFormat(format) || len(frames) == 0 || len(frames) > maxFrames ||
		(format == stickermedia.WebP && len(frames) != 1) {
		return false
	}
	previous := time.Duration(-1)
	for _, frame := range frames {
		if frame.Timestamp < 0 || frame.Timestamp >= maxDuration || frame.Timestamp <= previous ||
			(format == stickermedia.WebP && frame.Timestamp != 0) ||
			!validPNG(frame) {
			return false
		}
		previous = frame.Timestamp
	}
	return true
}

func validPNG(frame stickermedia.Frame) bool {
	if len(frame.PNG) == 0 || len(frame.PNG) > maxFrameBytes || frame.Width < 1 || frame.Height < 1 ||
		frame.Width > maxSide ||
		frame.Height > maxSide {
		return false
	}
	config, err := png.DecodeConfig(bytes.NewReader(frame.PNG))
	if err != nil || config.Width != frame.Width || config.Height != frame.Height {
		return false
	}
	reader := bytes.NewReader(frame.PNG)
	decoded, err := png.Decode(reader)
	return err == nil && reader.Len() == 0 && decoded.Bounds().Dx() == frame.Width &&
		decoded.Bounds().Dy() == frame.Height
}
