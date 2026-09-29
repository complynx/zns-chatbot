package appclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDerivedHostUsesFreshUserAndSeparateOwnerCredential(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte(strings.Repeat("h", 32))}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/derived/order-actions", r.URL.Path)
		assert.Equal(t, "Bearer delegated-"+strconv.Itoa(int(calls.Load())), r.Header.Get("Authorization"))
		owner, err := signer.VerifyDerivedMutation(r.Header.Get("X-Zns-Derivation"))
		assert.NoError(t, err)
		assert.Equal(t, "alice", owner)
		var body struct {
			Command orders.Command        `json:"command"`
			Source  readsource.Derivation `json:"source"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		assert.True(t, body.Source.Valid())
		if assert.NotNil(t, body.Source.Generation) {
			assert.Equal(t, int64(0), *body.Source.Generation)
		}
		_, err = w.Write([]byte(`{}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	host := appclient.Host{
		Base:   server.URL,
		Signer: signer,
		UserToken: func(_ context.Context, owner string) (string, error) {
			require.Equal(t, "alice", owner)
			calls.Add(1)
			return "delegated-" + strconv.Itoa(int(calls.Load())), nil
		},
	}
	generation := int64(0)
	source := readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
	for range 2 {
		_, err := host.ExecuteDerivedOrder(t.Context(), "alice", orders.Command{}, source)
		require.NoError(t, err)
	}
	require.Equal(t, int32(2), calls.Load())
}

func TestMissingUserCapabilityFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := (appclient.Client{}).UserToken(t.Context(), "alice")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	host := appclient.Host{Base: "http://127.0.0.1:1"}
	err = host.ArchiveOriginal(t.Context(), "alice", "key", "user", "text")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	_, err = host.ExecuteDerivedOrder(t.Context(), "alice", orders.Command{}, readsource.Derivation{})
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
}
