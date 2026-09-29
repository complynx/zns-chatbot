package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func downloadCalls() map[string]func(context.Context, appclient.Client, string) ([]byte, error) {
	return map[string]func(context.Context, appclient.Client, string) ([]byte, error){
		"pass proof": func(ctx context.Context, c appclient.Client, owner string) ([]byte, error) {
			proof, err := c.DownloadPassProof(ctx, owner, "event", "recipient")
			return proof.Body, err
		},
		"order proof": func(ctx context.Context, c appclient.Client, owner string) ([]byte, error) {
			proof, err := c.DownloadOrderProof(ctx, owner, "event", "order")
			return proof.Body, err
		},
		"pass export": func(ctx context.Context, c appclient.Client, owner string) ([]byte, error) {
			return c.ExportPasses(ctx, owner)
		},
		"order export": func(ctx context.Context, c appclient.Client, owner string) ([]byte, error) {
			return c.ExportOrders(ctx, owner, "event")
		},
		"media": func(ctx context.Context, c appclient.Client, owner string) ([]byte, error) {
			attachment, err := c.Media(ctx, owner, "attachment")
			return attachment.Body, err
		},
	}
}

func TestCoreAPIRejectsCredentialRedirects(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var redirected atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect-target" {
					redirected.Add(1)
					w.WriteHeader(http.StatusForbidden)
					return
				}
				http.Redirect(w, r, "/redirect-target", status)
			}))
			t.Cleanup(server.Close)
			client := appclient.Client{
				Base:     server.URL,
				HTTP:     server.Client(),
				Exchange: &authExchange{},
				Links: authLinks{
					user: identity.User{Owner: "alice", Subject: "z-alice"},
				},
				SandboxToken: (identity.Signer{}).Token,
			}
			ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
			require.NoError(t, err)
			_, err = client.Media(ctx, owner, "attachment")
			require.Error(t, err)

			_, err = (appclient.Host{Base: client.Base, HTTP: client.HTTP}).PendingNotifications(ctx)
			require.Error(t, err)
			assert.Zero(t, redirected.Load(), "neither media metadata nor service requests may forward credentials")
			assert.Nil(t, client.HTTP.CheckRedirect)
		})
	}
}

func TestDownloadsRequireDelegatedPrincipal(t *testing.T) {
	t.Parallel()
	for name, download := range downloadCalls() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer delegated-token" {
					w.WriteHeader(http.StatusUnauthorized)
					_, err := w.Write([]byte(`{"code":"unauthorized"}`))
					assert.NoError(t, err)
					return
				}
				if r.URL.Path == "/v1/media/attachment" {
					_, err := w.Write([]byte(`{"id":"attachment"}`))
					assert.NoError(t, err)
					return
				}
				w.Header().Set("X-Pass-Version", "3")
				w.Header().Set("X-Order-Version", "3")
				w.Header().Set("X-Payment-Attempt", "attempt")
				w.Header().Set("Content-Disposition", `attachment; filename="proof.pdf"`)
				_, err := w.Write([]byte("file bytes"))
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
			body, err := download(ctx, client, owner)
			require.NoError(t, err)
			assert.Equal(t, "file bytes", string(body))
			before := requests.Load()
			_, err = download(t.Context(), client, owner)
			require.ErrorIs(t, err, identity.ErrZitadelIdentity)
			_, err = download(ctx, client, "bob")
			require.ErrorIs(t, err, identity.ErrZitadelIdentity)
			assert.Equal(t, before, requests.Load(), "invalid principals must not reach the API")
		})
	}
}

func TestDownloadsRejectRedirects(t *testing.T) {
	t.Parallel()
	for name, download := range downloadCalls() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var redirected atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/media/attachment":
					_, err := w.Write([]byte(`{"id":"attachment"}`))
					assert.NoError(t, err)
				case "/redirect-target":
					redirected.Store(true)
					w.WriteHeader(http.StatusForbidden)
				default:
					http.Redirect(w, r, "/redirect-target", http.StatusFound)
				}
			}))
			t.Cleanup(server.Close)
			client := appclient.Client{
				Base:     server.URL,
				HTTP:     server.Client(),
				Exchange: &authExchange{},
				Links: authLinks{
					user: identity.User{Owner: "alice", Subject: "z-alice"},
				},
				SandboxToken: (identity.Signer{}).Token,
			}
			ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
			require.NoError(t, err)
			_, err = download(ctx, client, owner)
			require.Error(t, err)
			assert.False(t, redirected.Load(), "delegated bearer must not follow even same-host redirects")
			assert.Nil(t, client.HTTP.CheckRedirect, "caller HTTP configuration must remain unchanged")
		})
	}
}

type failedDownloadExchange struct{ err error }

func (e failedDownloadExchange) Exchange(context.Context, string) (string, error) {
	return "", e.err
}

func TestDownloadsDoNotFallbackAfterExchangeFailure(t *testing.T) {
	t.Parallel()
	for name, download := range downloadCalls() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)
			failure := errors.New("identity provider unavailable")
			client := appclient.Client{
				Base:     server.URL,
				Exchange: failedDownloadExchange{err: failure},
				Links: authLinks{
					user: identity.User{Owner: "alice", Subject: "z-alice"},
				},
				SandboxToken: (identity.Signer{}).Token,
			}
			ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
			require.NoError(t, err)
			_, err = download(ctx, client, owner)
			require.ErrorIs(t, err, failure)
			assert.Zero(t, requests.Load())
		})
	}
}
