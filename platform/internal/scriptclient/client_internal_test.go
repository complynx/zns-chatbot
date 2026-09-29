package scriptclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateResponseContract(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"valid":     `{"result":{"total":42}}`,
		"null":      `{"result":null}`,
		"error":     `{"error":"execution_failed"}`,
		"both":      `{"result":42,"error":"failed"}`,
		"duplicate": `{"result":42,"result":43}`,
		"alias":     `{"Result":42}`,
		"trailing":  `{"result":42} {}`,
		"oversize":  `{"result":"` + strings.Repeat("x", maxOutput) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/evaluate", r.URL.Path)
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			defer server.Close()
			client := &Client{http: server.Client()}
			// Supply a transport that sends the fixed logical host to this test server.
			client.http.Transport = rewriteTransport{base: server.Client().Transport, target: server.URL}
			result, err := client.Evaluate(t.Context(), Request{Code: "return input", Input: json.RawMessage(`{}`)})
			if name == "valid" || name == "null" {
				require.NoError(t, err)
				assert.True(t, json.Valid(result))
			} else {
				require.Error(t, err)
			}
		})
	}
}

type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (r rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copyRequest, err := http.NewRequestWithContext(
		request.Context(), request.Method, r.target+request.URL.Path, request.Body,
	)
	if err != nil {
		return nil, err
	}
	return r.base.RoundTrip(copyRequest)
}

func TestEvaluateCanceled(t *testing.T) {
	t.Parallel()
	client, err := New(t.TempDir() + "/socket")
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Evaluate(ctx, Request{Code: "return 1", Input: json.RawMessage(`null`)})
	require.ErrorIs(t, err, context.Canceled)
}
