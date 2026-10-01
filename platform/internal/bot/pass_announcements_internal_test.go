package bot

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestRegistrationAnnouncementDispatcherUsesCapturedText(t *testing.T) {
	t.Parallel()
	completions := make(chan passbooking.AnnouncementCompletion, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/pass-announcements/begin":
			var attempt delivery.Attempt
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&attempt))
			assert.Equal(t, delivery.Attempt{ID: 7, Generation: 2}, attempt)
			_, _ = w.Write([]byte(`{"ready":true}`))
		case "/internal/pass-announcements/complete":
			var completed passbooking.AnnouncementCompletion
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&completed))
			completions <- completed
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected Host route %s", r.URL.Path)
		}
	}))
	t.Cleanup(host.Close)
	requests := make(chan []byte, 1)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		requests <- body
		connection, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(sink.Close)
	thread := int64(42)
	item := passbooking.RegistrationAnnouncement{ID: 7, Attempts: 2, Channel: "-100123", ThreadID: &thread,
		Locale: "en", Name: "Changed name", Role: "leader", Text: "Original &lt;name&gt; applied for a follower pass!"}
	rerendered, err := registrationAnnouncementText(item)
	require.NoError(t, err)
	require.NotEqual(t, item.Text, rerendered)
	b := &Bot{Host: appclient.Host{Base: host.URL}, TG: telegram.Client{Base: sink.URL, Token: "synthetic"}}
	require.NoError(t, b.deliverPreparedRegistrationAnnouncement(t.Context(), item))
	completed := <-completions
	assert.JSONEq(
		t,
		`{"chat_id":-100123,"message_thread_id":42,"parse_mode":"HTML","text":"Original &lt;name&gt; applied for a follower pass!"}`,
		string(<-requests),
	)
	assert.Equal(t, delivery.Uncertain, completed.Outcome.Kind)
	assert.Equal(t, item.ID, completed.ID)
	assert.Equal(t, item.Attempts, completed.Attempt)
	item.Text = ""
	require.ErrorContains(t, b.deliverPreparedRegistrationAnnouncement(t.Context(), item), "text is not captured")
	assert.Empty(t, requests, "missing durable capture must not send")
}

func TestRegistrationAnnouncementSourceText(t *testing.T) {
	t.Parallel()
	item := passbooking.RegistrationAnnouncement{Name: "A < B", Role: "leader", Locale: "en"}
	text, err := registrationAnnouncementText(item)
	require.NoError(t, err)
	assert.Equal(t, "A &lt; B applied for a leader pass!", text)
	item.Locale = "ru"
	item.Role = "follower"
	text, err = registrationAnnouncementText(item)
	require.NoError(t, err)
	assert.Equal(t, "A &lt; B подал заявку на пасс партнёрши!", text)
}

func TestRegistrationAnnouncementUncertainSendIsNotRetried(t *testing.T) {
	t.Parallel()
	result := announcementCompletion(1, 0, errors.New("connection lost"))
	assert.Equal(t, "telegram_outcome_unknown", result.Outcome.Reason)
	result = announcementCompletion(1, 0, &telegram.APIError{Code: http.StatusForbidden})
	assert.Equal(t, "telegram_recipient_rejected", result.Outcome.Reason)
	result = announcementCompletion(1, 44, nil)
	assert.Empty(t, result.Outcome.Reason)
	assert.EqualValues(t, 44, result.Outcome.MessageID)
}
