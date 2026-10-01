package integration_test

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

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDurableDeliveryRevocationCancelsUncertaintyAndReleasesFollower(t *testing.T) {
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
	received := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr != nil {
			t.Error(decodeErr)
			return
		}
		received <- payload.Text
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
	require.Equal(t, "private delivery canary", <-received)
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
	require.NoError(t, restarted.RecoverDeliveries(t.Context()))
	assertRevokedDelivery(t, f, active, "cancelled")
	entries := messageRetryCandidates(t, f.db, s.Delivery.BotID)
	require.Len(t, entries, 1)
	assert.Equal(t, "101", entries[0].Destination.Chat, "revoked uncertainty must release its same-chat follower")
	var marker, resends, messageID int64
	var reason, failure, terminalBefore string
	var recorded time.Time
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT last_uncertain_attempt,last_uncertain_reason,last_uncertain_recorded_at,uncertain_resends,telegram_message_id,failure,to_jsonb(d)::text FROM core.admin_message_deliveries d WHERE message_id=$1`, active).
			Scan(&marker, &reason, &recorded, &resends, &messageID, &failure, &terminalBefore),
	)
	assert.EqualValues(t, 1, marker)
	assert.Equal(t, "telegram_outcome_unknown", reason)
	assert.WithinDuration(t, time.Now(), recorded, 5*time.Second)
	assert.Zero(t, resends)
	assert.Zero(t, messageID)
	assert.Equal(t, "publication_cancelled", failure)
	once.Do(func() { close(release) })
	select {
	case err = <-done:
		require.ErrorContains(t, err, "admin_message_stale_attempt")
	case <-time.After(10 * time.Second):
		t.Fatal("delivery did not complete after transport release")
	}
	assertRevokedDelivery(t, f, active, "cancelled")
	var terminalAfter string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT to_jsonb(d)::text FROM core.admin_message_deliveries d WHERE message_id=$1`, active).
			Scan(&terminalAfter),
	)
	assert.JSONEq(
		t,
		terminalBefore,
		terminalAfter,
		"late malformed uncertainty cannot change terminal state or factual evidence",
	)
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	select {
	case text := <-received:
		require.Equal(t, "synthetic", text, "only the ordinary follower may cross the wire after cancellation")
	case <-time.After(10 * time.Second):
		t.Fatal("ordinary follower did not reach the synthetic transport")
	}
	assert.EqualValues(t, 2, sends.Load())
	assertRevokedDelivery(t, f, active, "cancelled")
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
