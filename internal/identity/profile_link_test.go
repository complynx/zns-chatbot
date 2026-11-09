//nolint:testpackage // Reuse the signed IAM wire fixture and profile state.
package identity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const profileProviderIssuer = "https://provider.invalid/tg"

func completedProfileFixture(t *testing.T) (*profileFixture, *Client, Account) {
	t.Helper()
	p, client, account := namedFixture(t)
	client.options.TelegramProviderIssuer = profileProviderIssuer
	client.options.TelegramProviderID = "provider"
	p.user.Human.Email.Address = "existing@example.invalid"
	p.links = []idpLink{{Provider: "other", Subject: "unrelated"},
		{Provider: "provider", Subject: "telegram:123:opaque"}}
	account.TelegramLink = &TelegramProfileLink{
		ProviderIssuer: profileProviderIssuer, ProviderID: "provider", ExternalSubject: "opaque",
		Subject: account.Subject, Organization: account.OrganizationID, BotID: 123, TelegramID: 456,
		CompletedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
	}
	return p, client, account
}

func TestProfileNameCompletedTelegramLinkPreservesOrdinaryAccount(t *testing.T) {
	t.Parallel()
	p, client, account := completedProfileFixture(t)
	ctx := context.Background()
	name, err := client.ReadProfileName(ctx, account, 456)
	require.NoError(t, err)
	require.Equal(t, ProfileName{FirstName: "Old", LastName: "Name"}, name)
	want := ProfileName{FirstName: "New", LastName: "Name"}
	require.NoError(t, client.SyncProfileName(ctx, account, 456, want))
	require.NoError(t, client.SyncProfileName(ctx, account, 456, want))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.writes)
	require.Equal(t, "existing@example.invalid", p.user.Human.Email.Address)
	require.Len(t, p.links, 2)
	require.Equal(t, []map[string]any{{"human": map[string]any{"profile": map[string]any{
		"givenName": want.FirstName, "familyName": want.LastName,
	}}}}, p.bodies)
}

func TestProfileNameCompletedLinkRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*Client, *Account)
	}{
		{"missing evidence", func(_ *Client, a *Account) { a.TelegramLink = nil }},
		{"missing pins", func(c *Client, _ *Account) { c.options.TelegramProviderID = "" }},
		{"wrong issuer", func(_ *Client, a *Account) { a.TelegramLink.ProviderIssuer = "https://other.invalid" }},
		{"wrong provider", func(_ *Client, a *Account) { a.TelegramLink.ProviderID = "other" }},
		{"wrong subject", func(_ *Client, a *Account) { a.TelegramLink.Subject = "other" }},
		{"wrong organization", func(_ *Client, a *Account) { a.TelegramLink.Organization = "other" }},
		{"wrong bot", func(_ *Client, a *Account) { a.TelegramLink.BotID = 999 }},
		{"wrong Telegram ID", func(_ *Client, a *Account) { a.TelegramLink.TelegramID = 999 }},
		{"missing opaque", func(_ *Client, a *Account) { a.TelegramLink.ExternalSubject = "" }},
		{"pending equivalent", func(_ *Client, a *Account) { a.TelegramLink.CompletedAt = time.Time{} }},
		{"missing expiry", func(_ *Client, a *Account) { a.TelegramLink.ExpiresAt = time.Time{} }},
		{"completed after expiry", func(_ *Client, a *Account) {
			a.TelegramLink.CompletedAt = a.TelegramLink.ExpiresAt.Add(time.Second)
		}},
		{"future completion", func(_ *Client, a *Account) {
			a.TelegramLink.CompletedAt = time.Now().Add(time.Hour)
			a.TelegramLink.ExpiresAt = a.TelegramLink.CompletedAt.Add(time.Minute)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, client, account := completedProfileFixture(t)
			test.change(client, &account)
			wantErr := ErrDenied
			if account.TelegramLink == nil {
				wantErr = ErrConflict
			}
			_, err := client.ReadProfileName(t.Context(), account, 456)
			require.ErrorIs(t, err, wantErr)
			require.ErrorIs(t, client.SyncProfileName(t.Context(), account, 456,
				ProfileName{FirstName: "New", LastName: "Name"}), wantErr)
			require.Zero(t, p.writes)
		})
	}
}

func TestProfileNameCompletedLinkRejectsRemoteChanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*profileFixture)
		want   error
	}{
		{"foreign reserved email", func(p *profileFixture) {
			p.user.Human.Email.Address = "tg+123+999@telegram.invalid"
		}, ErrConflict},
		{"unverified synthetic", func(p *profileFixture) {
			p.user.Human.Email.Address = "tg+123+456@telegram.invalid"
			p.user.Human.Email.Verified = false
		}, ErrConflict},
		{"foreign organization", func(p *profileFixture) { p.user.Details.Org = "other" }, ErrConflict},
		{"wrong account", func(p *profileFixture) { p.user.ID = "other" }, ErrConflict},
		{"inactive", func(p *profileFixture) { p.user.State = "USER_STATE_INACTIVE" }, ErrDenied},
		{"missing link", func(p *profileFixture) { p.links = nil }, ErrConflict},
		{"same provider conflict", func(p *profileFixture) {
			p.links = append(p.links, idpLink{Provider: "provider", Subject: "telegram:123:other"})
		}, ErrConflict},
		{"duplicate exact link", func(p *profileFixture) { p.links = append(p.links, p.links[1]) }, ErrConflict},
		{"partial listing", func(p *profileFixture) { p.partial = true }, ErrUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, client, account := completedProfileFixture(t)
			test.change(p)
			_, err := client.ReadProfileName(t.Context(), account, 456)
			require.ErrorIs(t, err, test.want)
			// An unchanged name must still recheck authority.
			require.ErrorIs(t, client.SyncProfileName(t.Context(), account, 456,
				ProfileName{FirstName: "Old", LastName: "Name"}), test.want)
			require.Zero(t, p.writes)
		})
	}
}

func TestProfileNameCompletedLinkRechecksBeforeAndAfterPatch(t *testing.T) {
	t.Parallel()
	for _, afterPatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "before PATCH", true: "after PATCH"}[afterPatch], func(t *testing.T) {
			t.Parallel()
			p, client, account := completedProfileFixture(t)
			if afterPatch {
				p.afterPatch = func(p *profileFixture) { p.links = nil }
			} else {
				p.afterLinks = func(p *profileFixture) { p.links = nil }
			}
			require.ErrorIs(t, client.SyncProfileName(t.Context(), account, 456,
				ProfileName{FirstName: "New", LastName: "Name"}), ErrConflict)
			p.mu.Lock()
			defer p.mu.Unlock()
			if afterPatch {
				require.Equal(t, 1, p.writes)
			} else {
				require.Zero(t, p.writes)
			}
		})
	}
}

func TestProfileNameCompletedLinkRechecksActiveBeforeUnchangedReturn(t *testing.T) {
	t.Parallel()
	p, client, account := completedProfileFixture(t)
	p.afterLinks = func(p *profileFixture) { p.user.State = "USER_STATE_INACTIVE" }
	require.ErrorIs(t, client.SyncProfileName(t.Context(), account, 456,
		ProfileName{FirstName: "Old", LastName: "Name"}), ErrDenied)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.writes)
}

func TestProfileNameSyntheticIgnoresLegacyEvidenceAndDisabledPins(t *testing.T) {
	t.Parallel()
	for _, providerID := range []string{"", "partial-pin"} {
		t.Run(providerID, func(t *testing.T) {
			t.Parallel()
			p, client, account := namedFixture(t)
			client.options.TelegramProviderID = providerID
			account.TelegramLink = &TelegramProfileLink{ProviderIssuer: "foreign", Subject: "foreign"}
			issuer, provider := client.TelegramProfileLinkPins()
			require.Empty(t, issuer)
			require.Empty(t, provider)
			_, err := client.ReadProfileName(t.Context(), account, 456)
			require.NoError(t, err)
			require.NoError(t, client.SyncProfileName(t.Context(), account, 456,
				ProfileName{FirstName: "New", LastName: "Name"}))
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Equal(t, 1, p.writes)
			require.Zero(t, p.linkReads)
		})
	}
}

func TestProfileNameCompletedLinkReturnsFinalValidatedName(t *testing.T) {
	t.Parallel()
	p, client, account := completedProfileFixture(t)
	concurrent := ProfileName{FirstName: "Concurrent", LastName: "Change"}
	p.afterLinks = func(p *profileFixture) { p.user.Human.Profile = concurrent }
	got, err := client.ReadProfileName(t.Context(), account, 456)
	require.NoError(t, err)
	require.Equal(t, concurrent, got)
}

func TestProfileNameCompletedLinkDoesNotAcknowledgeStaleUnchangedName(t *testing.T) {
	t.Parallel()
	p, client, account := completedProfileFixture(t)
	want := ProfileName{FirstName: "Old", LastName: "Name"}
	p.afterLinks = func(p *profileFixture) {
		p.user.Human.Profile = ProfileName{FirstName: "Concurrent", LastName: "Change"}
		p.afterLinks = nil
	}
	require.NoError(t, client.SyncProfileName(t.Context(), account, 456, want))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.writes)
	require.Equal(t, want, p.user.Human.Profile)
}

func TestProfileNameCompletedLinkKeepsChangedReadbackPending(t *testing.T) {
	t.Parallel()
	p, client, account := completedProfileFixture(t)
	p.afterPatch = func(p *profileFixture) {
		p.afterLinks = func(p *profileFixture) {
			p.user.Human.Profile = ProfileName{FirstName: "Concurrent", LastName: "Change"}
		}
	}
	require.ErrorIs(t, client.SyncProfileName(t.Context(), account, 456,
		ProfileName{FirstName: "New", LastName: "Name"}), ErrUnavailable)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.writes)
}

func TestTelegramProfileLinkPinsWithoutClient(t *testing.T) {
	t.Parallel()
	var client *Client
	issuer, provider := client.TelegramProfileLinkPins()
	require.Empty(t, issuer)
	require.Empty(t, provider)
}
