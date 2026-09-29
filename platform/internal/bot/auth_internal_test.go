package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type authLinks struct {
	user identity.User
	err  error
}

func (l authLinks) Telegram(_ context.Context, sender int64) (identity.User, error) {
	if sender != 101 {
		return identity.User{}, identity.ErrZitadelIdentity
	}
	return l.user, l.err
}

type authExchange struct{ subjects []string }

func (e *authExchange) Exchange(_ context.Context, subject string) (string, error) {
	e.subjects = append(e.subjects, subject)
	return "delegated-token", nil
}

func TestTrustedPrincipalRequiredForEachRequest(t *testing.T) {
	t.Parallel()
	exchange := &authExchange{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer delegated-token", r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	client := APIClient{
		Base:     server.URL,
		Exchange: exchange,
		Links:    authLinks{user: identity.User{Owner: "alice", Subject: "z-alice"}},
	}
	ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	assert.Equal(t, "alice", owner)
	for range 2 {
		_, err = client.Current(ctx, owner)
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"z-alice", "z-alice"}, exchange.subjects)
	_, err = client.Current(ctx, "bob")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	_, err = client.Current(t.Context(), "alice")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	_, err = client.notificationContext(t.Context(), "bob", 101)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	assert.Len(t, exchange.subjects, 2)
}

func TestIdentityFailureCannotFallBackToSandbox(t *testing.T) {
	t.Parallel()
	for _, client := range []APIClient{
		{Exchange: &authExchange{}},
		{Links: authLinks{}},
		{Exchange: &authExchange{}, Links: authLinks{err: identity.ErrZitadelIdentity}},
		{Exchange: &authExchange{}, Links: authLinks{user: identity.User{Owner: "alice"}}},
	} {
		_, _, err := client.AuthenticateTelegram(t.Context(), 101)
		require.ErrorIs(t, err, identity.ErrZitadelIdentity)
		_, err = client.userToken(t.Context(), "alice")
		require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	}
}

func TestIdentityOutageRetriesUpdate(t *testing.T) {
	t.Parallel()
	b := Bot{API: APIClient{Exchange: &authExchange{}, Links: authLinks{err: errors.New("database outage")}}}
	update := telegram.Update{
		Message: &telegram.Message{From: telegram.User{ID: 101}, Chat: telegram.Chat{ID: 101, Type: "private"}},
	}
	require.EqualError(t, b.Handle(t.Context(), update), "identity lookup unavailable")
	b.API.Links = authLinks{err: identity.ErrZitadelIdentity}
	require.NoError(t, b.Handle(t.Context(), update))
}
