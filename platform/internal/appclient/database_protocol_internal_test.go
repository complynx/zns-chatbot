package appclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestCoreDatabaseResponseReaders(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		name, header, body string
		status             int
		marked             bool
	}{
		{"sql", "1", `{"code":"internal_error"}`, 500, true},
		{"provider", "", `{"code":"internal_error"}`, 500, false},
		{"provider503", "", `{"code":"identity_provisioning_unavailable"}`, 503, false},
		{"sql503", "1", `{"code":"identity_provisioning_unavailable"}`, 503, true},
		{"malformed", "1", `{private SQL password=secret`, 500, false},
		{"empty", "1", `{}`, 500, false},
		{"trailing", "1", `{"code":"internal_error"} secret`, 500, false},
		{"unrecognized", "true", `{"code":"internal_error"}`, 500, false},
		{"success", "1", `{}`, 200, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NotEmpty(t, r.Header.Get("Authorization"))
				w.Header().Set(core.DatabaseFailureHeader, sample.header)
				w.WriteHeader(sample.status)
				_, _ = w.Write([]byte(sample.body))
			}))
			t.Cleanup(server.Close)
			client := Client{
				Base:         server.URL,
				HTTP:         server.Client(),
				SandboxToken: func(string) string { return "test-token" },
			}
			host := Host{
				Base:      server.URL,
				HTTP:      server.Client(),
				Signer:    identity.Signer{Key: []byte("01234567890123456789012345678901")},
				UserToken: func(context.Context, string) (string, error) { return "test-token", nil },
			}
			readers := map[string]func() error{
				"order_export": func() error { _, err := client.ExportOrders(t.Context(), "alice", "event"); return err },
				"order_proof":  func() error { _, err := client.DownloadOrderProof(t.Context(), "alice", "event", "order"); return err },
				"json": func() error {
					var out any
					return client.Call(t.Context(), "alice", http.MethodGet, "/test", nil, &out)
				},
				"export":   func() error { _, err := client.ExportPasses(t.Context(), "alice"); return err },
				"snapshot": func() error { _, err := host.ExportPassSnapshot(t.Context(), "alice"); return err },
				"payment":  func() error { _, err := client.DownloadPassProof(t.Context(), "alice", "event", "alice"); return err },
				"food": func() error {
					_, err := client.FoodProof(
						t.Context(),
						"alice",
						legacyfood.Command{EventID: "event", OrderID: "order", Kind: legacyfood.Meals},
					)
					return err
				},
				"provision": func() error { return host.ProvisionTelegram(t.Context(), 77, telegram.User{ID: 101}) },
			}
			for name, read := range readers {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					err := read()
					assert.Equal(t, sample.marked, errors.Is(err, core.ErrDatabase))
					if sample.marked {
						var problem *core.ProblemError
						require.ErrorAs(t, err, &problem)
						assert.Equal(t, sample.status, problem.Status)
					}
					if err != nil {
						assert.NotContains(t, err.Error(), "password=secret")
					}
				})
			}
		})
	}
}

type databaseUnavailableTransport struct{}

func (databaseUnavailableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private upstream SQL-looking password=secret")
}

func TestCoreTransportFailureIsNotDatabase(t *testing.T) {
	t.Parallel()
	client := Client{
		Base:         "https://configured-core.invalid",
		HTTP:         &http.Client{Transport: databaseUnavailableTransport{}},
		SandboxToken: func(string) string { return "test-token" },
	}
	var out any
	err := client.Call(t.Context(), "alice", http.MethodGet, "/test", nil, &out)
	require.Error(t, err)
	require.NotErrorIs(t, err, core.ErrDatabase)
	assert.NotContains(t, err.Error(), "password=secret")
}
