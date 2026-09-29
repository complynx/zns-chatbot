package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestZitadelActorRenewal(t *testing.T) {
	t.Parallel()
	var acquisitions atomic.Int32
	var fail atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		id, secret, ok := r.BasicAuth()
		assert.True(t, ok)
		if r.PostForm.Get("grant_type") == "client_credentials" {
			acquisitions.Add(1)
			assert.Equal(t, "actor-client", id)
			assert.Equal(t, "actor-secret", secret)
			assert.Equal(t, "openid urn:zitadel:iam:org:project:id:project:aud", r.PostForm.Get("scope"))
			if fail.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			assert.NoError(
				t,
				json.NewEncoder(w).
					Encode(map[string]any{"access_token": "renewed-actor", "token_type": "Bearer", "expires_in": 1}),
			)
			return
		}
		assert.Equal(t, "bot-client", id)
		assert.Equal(t, "bot-secret", secret)
		assert.Equal(t, "renewed-actor", r.PostForm.Get("actor_token"))
		assert.NoError(
			t,
			json.NewEncoder(w).
				Encode(map[string]any{"access_token": "delegated", "token_type": "Bearer", "issued_token_type": "urn:ietf:params:oauth:token-type:jwt", "expires_in": 60}),
		)
	}))
	defer server.Close()
	config := zitadelTestConfig(server)
	config.ActorToken = ""
	config.ActorClientID, config.ActorClientSecret = "actor-client", "actor-secret"
	client, err := identity.NewZitadel(config)
	require.NoError(t, err)
	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			token, exchangeErr := client.Exchange(t.Context(), "mapped-user")
			errors <- exchangeErr
			assert.Equal(t, "delegated", token)
		})
	}
	workers.Wait()
	close(errors)
	for exchangeErr := range errors {
		require.NoError(t, exchangeErr)
	}
	assert.EqualValues(t, 1, acquisitions.Load())
	time.Sleep(time.Second)
	_, err = client.Exchange(t.Context(), "mapped-user")
	require.NoError(t, err)
	assert.EqualValues(t, 2, acquisitions.Load())
	fail.Store(true)
	time.Sleep(time.Second)
	token, err := client.Exchange(t.Context(), "mapped-user")
	require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
	assert.Empty(t, token)
}
