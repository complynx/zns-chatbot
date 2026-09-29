package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
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
		{"blocked", `{"ok":false,"error_code":403,"description":"sensitive recipient failure"}`, "failed", "admin_telegram_rejected"},
		{"ambiguous", `broken JSON`, "failed", "telegram_outcome_unknown"},
		{"rate_limit", `{"ok":false,"error_code":429,"description":"retry"}`, "pending", "telegram_rate_limit"},
		{"negative_cooldown", `{"ok":false,"error_code":429,"parameters":{"retry_after":-1}}`, "failed", "telegram_invalid_cooldown"},
		{"excessive_cooldown", `{"ok":false,"error_code":429,"parameters":{"retry_after":86401}}`, "failed", "telegram_invalid_cooldown"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service := adminmessage.Service{DB: f.db}
			preview, err := service.PreviewCommand(
				t.Context(),
				"bob",
				"delivery",
				`/send_message_to @Channel:7 --html '<b>exact</b>'`,
			)
			require.NoError(t, err)
			require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
			var received struct {
				Chat   string `json:"chat_id"`
				Thread int64  `json:"message_thread_id"`
				Text   string `json:"text"`
				Mode   string `json:"parse_mode"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if decodeErr := json.NewDecoder(r.Body).Decode(&received); decodeErr != nil {
					t.Error(decodeErr)
				}
				_, _ = w.Write([]byte(scenario.response))
			}))
			t.Cleanup(server.Close)
			f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
			require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
			assert.Equal(t, "@channel", received.Chat)
			assert.EqualValues(t, 7, received.Thread)
			assert.Equal(t, "<b>exact</b>", received.Text)
			assert.Equal(t, "HTML", received.Mode)
			results, err := service.Results(t.Context(), "bob", preview.ID)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, scenario.state, results[0].State)
			assert.Equal(t, scenario.failure, results[0].Failure)
		})
	}
}

func TestAdminMessageRuntimeHonorsLongCooldown(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := adminmessage.Service{DB: f.db}
	preview, err := service.PreviewCommand(t.Context(), "bob", "cooldown", `/send_message_to 101 --msg "wait"`)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"retry","parameters":{"retry_after":120}}`))
	}))
	t.Cleanup(server.Close)
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
	for attempt := int64(1); attempt <= 3; attempt++ {
		require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
		results, resultErr := service.Results(t.Context(), "bob", preview.ID)
		require.NoError(t, resultErr)
		require.Len(t, results, 1)
		assert.Equal(t, attempt, results[0].Attempt)
		if attempt == 3 {
			assert.Equal(t, "failed", results[0].State)
			break
		}
		assert.Equal(t, "pending", results[0].State)
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
	}
}

func TestAdminMessageRuntimeCooldownFloor(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db}
	preview, err := service.PreviewCommand(t.Context(), "bob", "floor", `/send_message_to 101 --msg "wait"`)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	delivery, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	result := adminmessage.Completion{
		ID:         delivery.ID,
		Attempt:    delivery.Attempt,
		Failure:    "telegram_rate_limit",
		Retry:      true,
		RetryAfter: 1,
	}
	for _, invalid := range []int64{-1, adminmessage.MaxRetryAfterSeconds + 1} {
		result.RetryAfter = invalid
		requireCode(t, service.CompleteDelivery(t.Context(), result), "admin_message_invalid")
	}
	result.RetryAfter = 1
	require.NoError(t, service.CompleteDelivery(t.Context(), result))
	var seconds float64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT EXTRACT(EPOCH FROM available_at-clock_timestamp())::float8 FROM core.admin_message_deliveries WHERE id=$1`, delivery.ID).
			Scan(&seconds),
	)
	assert.Greater(t, seconds, float64(29))
	assert.LessOrEqual(t, seconds, float64(30))
}
