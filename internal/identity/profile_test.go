//nolint:testpackage // Reuse signed service credentials and the IAM wire fixture.
package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type profileFixture struct {
	mu          sync.Mutex
	user        iamUser
	writes      int
	bodies      []map[string]any
	status      int
	loseReply   bool
	ignoreWrite bool
}

func namedFixture(t *testing.T) (*profileFixture, *Client, Account) {
	t.Helper()
	f, client := fixture(t)
	p := &profileFixture{user: testUser("profile-user")}
	p.user.Human.Profile = ProfileName{FirstName: "Old", LastName: "Name"}
	original := f.server.Config.Handler
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/users/profile-user" {
			original.ServeHTTP(w, r)
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.Header.Get("X-Zitadel-Orgid") != "org" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPatch {
			p.patch(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"user": p.user})
	})
	return p, client, Account{
		Subject: "profile-user", OrganizationID: "org", SyntheticEmail: "tg+123+456@telegram.invalid",
	}
}

func (p *profileFixture) patch(w http.ResponseWriter, r *http.Request) {
	p.writes++
	var body map[string]any
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	p.bodies = append(p.bodies, body)
	if p.status != 0 {
		w.WriteHeader(p.status)
		return
	}
	if !p.ignoreWrite {
		b, _ := json.Marshal(body)
		var update struct {
			Human struct {
				Profile ProfileName `json:"profile"`
			} `json:"human"`
		}
		_ = json.Unmarshal(b, &update)
		p.user.Human.Profile = update.Human.Profile
	}
	if p.loseReply {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
		return
	}
	_, _ = w.Write([]byte(`{}`))
}

func TestProfileNameUpdatesOnlyNamesAndReconcilesReplay(t *testing.T) {
	t.Parallel()
	p, client, account := namedFixture(t)
	ctx := context.Background()
	want := ProfileName{FirstName: "Анна Мария", LastName: "де ла Крус"}
	before, err := client.ReadProfileName(ctx, account, 456)
	require.NoError(t, err)
	require.Equal(t, ProfileName{FirstName: "Old", LastName: "Name"}, before)
	for range 2 {
		require.NoError(t, client.SyncProfileName(ctx, account, 456, want))
	}
	after, err := client.ReadProfileName(ctx, account, 456)
	require.NoError(t, err)
	require.Equal(t, want, after)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.writes)
	require.Equal(t, []map[string]any{{"human": map[string]any{"profile": map[string]any{
		"givenName": want.FirstName, "familyName": want.LastName,
	}}}}, p.bodies)
}

func TestProfileNameLostReplyConvergesWithoutAnotherWrite(t *testing.T) {
	t.Parallel()
	p, client, account := namedFixture(t)
	p.loseReply = true
	want := ProfileName{FirstName: "New", LastName: "Name"}
	require.ErrorIs(t, client.SyncProfileName(context.Background(), account, 456, want), ErrUnavailable)
	require.NoError(t, client.SyncProfileName(context.Background(), account, 456, want))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.writes)
}

func TestProfileNameRequiresVerifiedActiveBinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*profileFixture, *Account)
		want   error
	}{
		{"local org", func(_ *profileFixture, a *Account) { a.OrganizationID = "other" }, ErrDenied},
		{
			"local email",
			func(_ *profileFixture, a *Account) { a.SyntheticEmail = "tg+123+999@telegram.invalid" },
			ErrDenied,
		},
		{"remote org", func(p *profileFixture, _ *Account) { p.user.Details.Org = "other" }, ErrConflict},
		{"remote id", func(p *profileFixture, _ *Account) { p.user.ID = "other" }, ErrConflict},
		{"remote email", func(p *profileFixture, _ *Account) { p.user.Human.Email.Address = "other" }, ErrConflict},
		{"unverified", func(p *profileFixture, _ *Account) { p.user.Human.Email.Verified = false }, ErrConflict},
		{"inactive", func(p *profileFixture, _ *Account) { p.user.State = "USER_STATE_INACTIVE" }, ErrDenied},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, client, account := namedFixture(t)
			test.change(p, &account)
			_, err := client.ReadProfileName(context.Background(), account, 456)
			require.ErrorIs(t, err, test.want)
			require.ErrorIs(t, client.SyncProfileName(context.Background(), account, 456,
				ProfileName{FirstName: "New", LastName: "Name"}), test.want)
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Zero(t, p.writes)
		})
	}
}

func TestProfileNameFailureNeverReportsSuccess(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, http.StatusServiceUnavailable, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			p, client, account := namedFixture(t)
			p.status, p.ignoreWrite = code, true
			wantErr := ErrUnavailable
			if code == http.StatusForbidden {
				wantErr = ErrDenied
			}
			require.ErrorIs(t, client.SyncProfileName(context.Background(), account, 456,
				ProfileName{FirstName: "New", LastName: "Name"}), wantErr)
		})
	}
}

func TestProfileNameRejectsIncompleteAndInvalidNames(t *testing.T) {
	t.Parallel()
	for _, first := range []string{"", " ", " Leading", "New\nName", string([]byte{0xff}), strings.Repeat("я", 201)} {
		t.Run(first, func(t *testing.T) {
			t.Parallel()
			p, client, account := namedFixture(t)
			require.ErrorIs(t, client.SyncProfileName(context.Background(), account, 456,
				ProfileName{FirstName: first, LastName: "Name"}), ErrDenied)
			require.ErrorIs(t, client.SyncProfileName(context.Background(), account, 456,
				ProfileName{FirstName: "Name", LastName: first}), ErrDenied)
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Zero(t, p.writes)
		})
	}
}
