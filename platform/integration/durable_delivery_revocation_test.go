package integration_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDurableDeliveryRevocationPreservesDispatchedLane(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	s := configureDeliveryFixture(t, f)
	history := conversation.Service{DB: f.db}
	require.NoError(t, history.AppendOriginal(t.Context(), "bob", "delivery-source", "user", "private delivery canary"))
	var original int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='bob' AND source_key='delivery-source'`).
			Scan(&original),
	)
	active := enqueueDerivedRevocationDelivery(t, s, "active", "101")
	enqueueSyntheticDelivery(t, s, "behind-active", "101")
	pending := enqueueDerivedRevocationDelivery(t, s, "not-dispatched", "303")
	enqueueSyntheticDelivery(t, s, "independent", "202")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		entered <- struct{}{}
		<-release
		_, _ = w.Write([]byte(`{"ok":`))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
	done := make(chan error, 1)
	go func() { done <- f.b.DeliverAdminMessages(t.Context()) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("synthetic transport did not observe dispatch")
	}
	require.NoError(t, history.DeleteContent(t.Context(), "bob", original))
	requireCode(t, s.CheckPublication(t.Context(), "bob", active), "source_revoked")
	requireCode(t, s.CheckPublication(t.Context(), "bob", pending), "source_revoked")
	assertRevokedDelivery(t, f, active, "sending")
	assertRevokedDelivery(t, f, pending, "cancelled")
	independent, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "202", independent.Destination.Chat, "same-chat successor cannot overtake the active send")
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE message_id=$1`,
		active,
	)
	require.NoError(t, err)
	restarted := adminmessage.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	_, found, err = restarted.Claim(t.Context())
	require.NoError(t, err)
	assert.False(t, found, "unknown head must retain its lane fence")
	assertRevokedDelivery(t, f, active, "unknown")
	once.Do(func() { close(release) })
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("delivery did not complete after transport release")
	}
	assertRevokedDelivery(t, f, active, "unknown")
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	assert.EqualValues(t, 1, sends.Load(), "neither the uncertain send nor its successor may be sent automatically")
}

func enqueueDerivedRevocationDelivery(t *testing.T, s adminmessage.Service, key, chat string) int64 {
	t.Helper()
	generation := int64(0)
	preview, err := s.PreviewDerivedCommand(t.Context(), "bob", key,
		`/send_message_to `+chat+` --msg "private delivery canary"`,
		readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}, PrivateHistory: true})
	require.NoError(t, err)
	require.NoError(t, s.Enqueue(t.Context(), "bob", preview.ID))
	return preview.ID
}

func assertRevokedDelivery(t *testing.T, f *fixture, id int64, expected string) {
	t.Helper()
	var state, content string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT state,content::text FROM core.admin_message_deliveries WHERE message_id=$1`, id).
			Scan(&state, &content),
	)
	assert.Equal(t, expected, state)
	assert.JSONEq(t, `{}`, content, "revocation must still redact private payload")
}
