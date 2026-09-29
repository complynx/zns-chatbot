package bot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestHistoryResponseLimitFallbackIsExact(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name     string
		body     string
		status   int
		fallback bool
		fail     bool
	}{
		{"ordinary", `[]`, http.StatusOK, false, false},
		{"exact_limit", `[]` + strings.Repeat(" ", appclient.MaxAPIBytes-2), http.StatusOK, false, false},
		{"over_limit", `[]` + strings.Repeat(" ", appclient.MaxAPIBytes-1), http.StatusOK, true, false},
		{"oversized_truncated", `[` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, true, false},
		{"oversized_string_prefix", `[{"order_id":"` + strings.Repeat("x", appclient.MaxAPIBytes), http.StatusOK, true, false},
		{"oversized_malformed_prefix", `[{broken}]` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, false, true},
		{"oversized_invalid_escape", `["\q` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, false, true},
		{"oversized_trailing_malformed", `[]broken` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, false, true},
		{"oversized_extra_value", `[][]` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, false, true},
		{"oversized_extra_incomplete", `[][` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, false, true},
		{"oversized_wrong_type", `true` + strings.Repeat(" ", appclient.MaxAPIBytes), http.StatusOK, false, true},
		{"exact_limit_truncated", `[` + strings.Repeat(" ", appclient.MaxAPIBytes-1), http.StatusOK, false, true},
		{"truncated", `[`, http.StatusOK, false, true},
		{"malformed", `[{broken}]`, http.StatusOK, false, true},
		{"trailing_malformed", `[]broken`, http.StatusOK, false, true},
		{"extra_value", `[][]`, http.StatusOK, false, true},
		{"denied", `{"code":"access_denied"}`, http.StatusForbidden, false, true},
		{"oversized_denied", strings.Repeat("x", appclient.MaxAPIBytes+1), http.StatusUnauthorized, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if strings.HasSuffix(r.URL.Path, "/history-recent") {
					_, err := w.Write([]byte(`[]`))
					assert.NoError(t, err)
					return
				}
				w.WriteHeader(scenario.status)
				_, err := w.Write([]byte(scenario.body))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			client := appclient.Client{
				Base:     server.URL,
				Exchange: &authExchange{},
				Links: authLinks{
					user: identity.User{Owner: "alice", Subject: "z-alice"},
				},
				SandboxToken: (identity.Signer{}).Token,
			}
			ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
			require.NoError(t, err)
			_, err = client.OrderHistory(ctx, owner, "event")
			if scenario.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			want := 1
			if scenario.fallback {
				want++
			}
			assert.EqualValues(t, want, requests.Load())
		})
	}
}

type historyUnavailableTransport struct{ calls int }

func (transport *historyUnavailableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return nil, errors.New("synthetic unavailable")
}

func TestHistoryTransportDoesNotRetryUnavailableOrUntrustedIdentity(t *testing.T) {
	t.Parallel()
	transport := &historyUnavailableTransport{}
	client := appclient.Client{
		Base:         "https://synthetic.invalid",
		HTTP:         &http.Client{Transport: transport},
		Exchange:     &authExchange{},
		Links:        authLinks{user: identity.User{Owner: "alice", Subject: "z-alice"}},
		SandboxToken: (identity.Signer{}).Token,
	}
	ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	_, err = client.OrderHistory(ctx, owner, "event")
	require.Error(t, err)
	assert.Equal(t, 1, transport.calls)
	_, err = client.OrderHistory(context.Background(), owner, "event")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	assert.Equal(t, 1, transport.calls)
}

type historyBoundProbeBody struct {
	read   int
	closed bool
}

func (body *historyBoundProbeBody) Read(target []byte) (int, error) {
	if body.read >= appclient.MaxAPIBytes+1 {
		return 0, errors.New("synthetic read exceeded bounded prefix")
	}
	for index := range target {
		target[index] = ' '
	}
	if body.read == 0 && len(target) > 0 {
		target[0] = '['
	}
	body.read += len(target)
	return len(target), nil
}

func (body *historyBoundProbeBody) Close() error {
	body.closed = true
	return nil
}

type historyBoundProbeTransport struct{ body *historyBoundProbeBody }

func (transport historyBoundProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := io.NopCloser(strings.NewReader(`[]`))
	if strings.HasSuffix(request.URL.Path, "/history") {
		body = transport.body
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
}

func TestHistoryTransportBoundsTheObservedPrefix(t *testing.T) {
	t.Parallel()
	body := &historyBoundProbeBody{}
	client := appclient.Client{
		Base:         "https://synthetic.invalid",
		HTTP:         &http.Client{Transport: historyBoundProbeTransport{body: body}},
		Exchange:     &authExchange{},
		Links:        authLinks{user: identity.User{Owner: "alice", Subject: "z-alice"}},
		SandboxToken: (identity.Signer{}).Token,
	}
	ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	_, err = client.OrderHistory(ctx, owner, "event")
	require.NoError(t, err)
	assert.Equal(t, appclient.MaxAPIBytes+1, body.read)
	assert.True(t, body.closed)
}
