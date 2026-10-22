//nolint:testpackage // Reuse the IAM wire fixture and signed service credentials.
package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

type linkFixture struct {
	mu             sync.Mutex
	links          []idpLink
	adds           int
	addStatus      int
	loseReply      bool
	suppressRead   bool
	deactivate     bool
	incomplete     bool
	malformedReply string
	omitZeroTotal  bool
	missingDetails bool
}

func linkedFixture(t *testing.T) (*linkFixture, *iamFixture, *Client, Account) {
	t.Helper()
	f, c := fixture(t)
	account, err := c.EnsureUser(
		context.Background(),
		telegram.User{ID: 456, FirstName: "Fixture"},
		"persisted-id",
		func(context.Context, bool) error { return nil },
	)
	require.NoError(t, err)
	links := &linkFixture{}
	original := f.server.Config.Handler
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/users/persisted-id/links") {
			original.ServeHTTP(w, r)
			return
		}
		links.serve(w, r, f)
	})
	return links, f, c, account
}

func TestTelegramIDPLinkCreatesOnceAndPreservesOpaqueSubject(t *testing.T) {
	t.Parallel()
	links, _, client, account := linkedFixture(t)
	for range 2 {
		require.NoError(
			t,
			client.EnsureTelegramIDPLink(context.Background(), account, 456, "provider", "opaque-different-from-456"),
		)
	}
	links.mu.Lock()
	defer links.mu.Unlock()
	require.Equal(t, 1, links.adds)
	require.Equal(t, "telegram:123:opaque-different-from-456", links.links[0].Subject)
}

func TestTelegramIDPLinkRecoversLostWriteReply(t *testing.T) {
	t.Parallel()
	links, _, client, account := linkedFixture(t)
	links.loseReply = true
	require.NoError(
		t,
		client.EnsureTelegramIDPLink(context.Background(), account, 456, "provider", "opaque-different-from-456"),
	)
	require.NoError(
		t,
		client.EnsureTelegramIDPLink(context.Background(), account, 456, "provider", "opaque-different-from-456"),
	)
	require.Equal(t, 1, links.adds)
}

func TestTelegramIDPLinkRequiresReadbackAndRejectsConflicts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(*linkFixture)
		want  error
		adds  int
	}{
		{"foreign owner conflict", func(f *linkFixture) { f.addStatus = http.StatusConflict }, ErrConflict, 1},
		{
			"another subject already bound",
			func(f *linkFixture) { f.links = []idpLink{{Provider: "provider", Subject: "telegram:123:another"}} },
			ErrConflict,
			0,
		},
		{"projection not caught up", func(f *linkFixture) { f.suppressRead = true }, ErrUnavailable, 1},
		{"missing link metadata", func(f *linkFixture) { f.missingDetails = true }, ErrUnavailable, 0},
		{"incomplete link listing", func(f *linkFixture) { f.incomplete = true }, ErrUnavailable, 0},
		{"inactive after write", func(f *linkFixture) { f.deactivate = true }, ErrDenied, 1},
		{"denied remote write", func(f *linkFixture) { f.addStatus = http.StatusForbidden }, ErrDenied, 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			links, _, client, account := linkedFixture(t)
			test.setup(links)
			err := client.EnsureTelegramIDPLink(
				context.Background(),
				account,
				456,
				"provider",
				"opaque-different-from-456",
			)
			require.ErrorIs(t, err, test.want)
			require.Equal(t, test.adds, links.adds)
		})
	}
}

func TestTelegramIDPLinkReconcilesConflictOnlyWithExactReadback(t *testing.T) {
	t.Parallel()
	links, _, client, account := linkedFixture(t)
	// Model another caller winning between our initial read and write.
	original := client.http.Transport
	client.http.Transport = linkRaceTransport{next: original, links: links}
	require.NoError(
		t,
		client.EnsureTelegramIDPLink(context.Background(), account, 456, "provider", "opaque-different-from-456"),
	)
	require.Equal(t, 1, links.adds)
}

type linkRaceTransport struct {
	next  http.RoundTripper
	links *linkFixture
}

func (tr linkRaceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v2/users/persisted-id/links" {
		tr.links.mu.Lock()
		tr.links.links = []idpLink{{Provider: "provider", Subject: "telegram:123:opaque-different-from-456"}}
		tr.links.addStatus = http.StatusConflict
		tr.links.mu.Unlock()
	}
	next := tr.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}

func TestTelegramIDPLinkDoesNotTrustCallerAccountOrInvalidScope(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"wrong email", "wrong organization", "wrong numeric id", "empty provider", "empty opaque subject", "inactive account"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			links, iam, client, account := linkedFixture(t)
			provider, sub, id := "provider", "opaque-different-from-456", int64(456)
			switch name {
			case "wrong email":
				account.SyntheticEmail = "tg+123+999@telegram.invalid"
			case "wrong organization":
				account.OrganizationID = "foreign"
			case "wrong numeric id":
				id = 999
			case "empty provider":
				provider = ""
			case "empty opaque subject":
				sub = ""
			case "inactive account":
				iam.mu.Lock()
				iam.users[0].State = "USER_STATE_INACTIVE"
				iam.mu.Unlock()
			}
			require.ErrorIs(
				t,
				client.EnsureTelegramIDPLink(context.Background(), account, id, provider, sub),
				ErrDenied,
			)
			require.Zero(t, links.adds)
		})
	}
}

func (links *linkFixture) serve(w http.ResponseWriter, r *http.Request, f *iamFixture) {
	links.mu.Lock()
	defer links.mu.Unlock()
	if r.Method != http.MethodPost || r.Header.Get("Authorization") == "" ||
		r.Header.Get("X-Zitadel-Orgid") != "org" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.URL.Path == "/v2/users/persisted-id/links/_search" {
		if links.missingDetails {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		rows := links.links
		if links.suppressRead && links.adds > 0 {
			rows = nil
		}
		total := len(rows)
		if links.incomplete {
			total++
		}
		if links.omitZeroTotal && total == 0 {
			_ = json.NewEncoder(w).
				Encode(map[string]any{"details": map[string]any{"timestamp": "0001-01-01T00:00:00Z"}})
			return
		}
		_ = json.NewEncoder(w).
			Encode(map[string]any{"result": rows, "details": map[string]any{"totalResult": strconv.Itoa(total)}})
		return
	}
	links.write(w, r, f)
}

func (links *linkFixture) write(w http.ResponseWriter, r *http.Request, f *iamFixture) {
	links.adds++
	if links.addStatus != 0 {
		w.WriteHeader(links.addStatus)
		return
	}
	var input struct {
		Link idpLink `json:"idpLink"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if input.Link.Provider != "provider" || input.Link.Subject != "telegram:123:opaque-different-from-456" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, existing := range links.links {
		if existing.Provider == input.Link.Provider && existing.Subject == input.Link.Subject {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}
	links.links = append(links.links, input.Link)
	if links.deactivate {
		f.mu.Lock()
		f.users[0].State = "USER_STATE_INACTIVE"
		f.mu.Unlock()
	}
	if links.loseReply {
		connection, _, hijackErr := w.(http.Hijacker).Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
		return
	}
	switch links.malformedReply {
	case "empty":
		return
	case "malformed":
		_, _ = w.Write([]byte("{broken"))
		return
	case "oversized":
		_, _ = w.Write([]byte(strings.Repeat(" ", maxResponseBytes) + "{}"))
		return
	}
	_, _ = w.Write([]byte(`{}`))
}

func TestTelegramIDPLinkRecoversCommittedWriteWithUnusableSuccessReply(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"empty", "malformed", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			links, _, client, account := linkedFixture(t)
			links.malformedReply = kind
			require.NoError(
				t,
				client.EnsureTelegramIDPLink(
					context.Background(),
					account,
					456,
					"provider",
					"opaque-different-from-456",
				),
			)
			require.NoError(
				t,
				client.EnsureTelegramIDPLink(
					context.Background(),
					account,
					456,
					"provider",
					"opaque-different-from-456",
				),
			)
			require.Equal(t, 1, links.adds)
		})
	}
}

func TestTelegramIDPLinkConcurrentCallsConverge(t *testing.T) {
	t.Parallel()
	links, _, client, account := linkedFixture(t)
	var calls sync.WaitGroup
	results := make(chan error, 8)
	for range cap(results) {
		calls.Go(func() {
			results <- client.EnsureTelegramIDPLink(context.Background(), account, 456, "provider", "opaque-different-from-456")
		})
	}
	calls.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	links.mu.Lock()
	defer links.mu.Unlock()
	require.Len(t, links.links, 1)
	require.Equal(t, "telegram:123:opaque-different-from-456", links.links[0].Subject)
}

func TestTelegramIDPLinkAcceptsOmittedZeroCountFromProtoJSON(t *testing.T) {
	t.Parallel()
	links, _, client, account := linkedFixture(t)
	links.omitZeroTotal = true
	require.NoError(
		t,
		client.EnsureTelegramIDPLink(context.Background(), account, 456, "provider", "opaque-different-from-456"),
	)
	require.Equal(t, 1, links.adds)
}
