package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	deliverypolicy "github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminMessageRuntimeDeliveryFailuresAndTopics(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name     string
		response string
		state    string
		failure  string
	}{
		{"success", `{"ok":true,"result":{"message_id":55}}`, "sent", ""},
		{"blocked", `{"ok":false,"error_code":403,"description":"sensitive recipient failure"}`, "failed", "telegram_recipient_rejected"},
		{"ambiguous", `broken JSON`, "unknown", "telegram_outcome_unknown"},
		{"rate_limit", `{"ok":false,"error_code":429,"description":"retry"}`, "pending", "telegram_rate_limit"},
		{"negative_cooldown", `{"ok":false,"error_code":429,"parameters":{"retry_after":-1}}`, "parked", "telegram_invalid_cooldown"},
		{"large_cooldown", `{"ok":false,"error_code":429,"parameters":{"retry_after":86401}}`, "pending", "telegram_rate_limit"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service := configureDeliveryFixture(t, f)
			service.DestinationResolver = publicationResolver(func(_ context.Context, alias string) (int64, error) {
				require.Equal(t, "@channel", alias)
				return -100123, nil
			})
			preview, err := service.PreviewCommand(
				t.Context(),
				"bob",
				"delivery",
				`/send_message_to @Channel:7 --html '<b>exact</b>'`,
			)
			require.NoError(t, err)
			require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
			var received struct {
				Chat   int64  `json:"chat_id"`
				Thread int64  `json:"message_thread_id"`
				Text   string `json:"text"`
				Mode   string `json:"parse_mode"`
			}
			var sends atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if decodeErr := json.NewDecoder(r.Body).Decode(&received); decodeErr != nil {
					t.Error(decodeErr)
				}
				_, _ = w.Write([]byte(scenario.response))
			}))
			t.Cleanup(server.Close)
			f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			assert.EqualValues(t, -100123, received.Chat)
			assert.EqualValues(t, 7, received.Thread)
			assert.Equal(t, "<b>exact</b>", received.Text)
			assert.Equal(t, "HTML", received.Mode)
			results, err := service.Results(t.Context(), "bob", preview.ID)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, scenario.state, results[0].State)
			assert.Equal(t, scenario.failure, results[0].Failure)
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			assert.EqualValues(t, 1, sends.Load(), "terminal, uncertain and deferred items are not blindly resent")
		})
	}
}

func TestAdminMessageRuntimeHonorsLongCooldown(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := configureDeliveryFixture(t, f)
	preview, err := service.PreviewCommand(t.Context(), "bob", "cooldown", `/send_message_to 101 --msg "wait"`)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"retry","parameters":{"retry_after":120}}`))
	}))
	t.Cleanup(server.Close)
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
	for attempt := int64(1); attempt <= 5; attempt++ {
		require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
		results, resultErr := service.Results(t.Context(), "bob", preview.ID)
		require.NoError(t, resultErr)
		require.Len(t, results, 1)
		assert.Equal(t, attempt, results[0].Attempt)
		assert.Equal(t, "pending", results[0].State)
		var failures int64
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT failure_count FROM core.admin_message_deliveries WHERE id=$1`, results[0].ID).
				Scan(&failures),
		)
		assert.Zero(t, failures)
		var seconds float64
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT EXTRACT(EPOCH FROM available_at-clock_timestamp())::float8 FROM core.admin_message_deliveries WHERE message_id=$1`, preview.ID).
				Scan(&seconds),
		)
		require.Greater(t, seconds, float64(119), "persist the entire Telegram cooldown, not the generic30s backoff")
		_, found, claimErr := service.Claim(t.Context())
		require.NoError(t, claimErr)
		assert.False(t, found)
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second' WHERE message_id=$1`,
			preview.ID,
		)
		require.NoError(t, err)
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
		)
		require.NoError(t, err)
	}
}

func TestAdminMessageRuntimeCooldownFloor(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	preview, err := service.PreviewCommand(t.Context(), "bob", "fallback", `/send_message_to 101 --msg "wait"`)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	item, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := service.BeginDelivery(t.Context(), deliverypolicy.Attempt{ID: item.ID, Generation: item.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	result := adminmessage.Completion{ID: item.ID, Attempt: item.Attempt, Outcome: deliverypolicy.Outcome{
		Kind: deliverypolicy.Deferred, Reason: "telegram_rate_limit", Missing: true}}
	require.NoError(t, service.CompleteDelivery(t.Context(), result))
	var seconds float64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT EXTRACT(EPOCH FROM available_at-clock_timestamp())::float8 FROM core.admin_message_deliveries WHERE id=$1`, item.ID).
			Scan(&seconds),
	)
	assert.Greater(t, seconds, float64(29))
	assert.LessOrEqual(t, seconds, float64(30))
}
