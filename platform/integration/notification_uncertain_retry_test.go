package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sink receives the real message before the adapter loses its response.
type notificationLostResponse struct {
	mu    sync.Mutex
	text  string
	calls int
	drops int
}

func (l *notificationLostResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(request.URL.Path, "/sendMessage") {
		return http.DefaultTransport.RoundTrip(request)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	var message struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}
	if err = json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil || message.ChatID != 202 {
		return response, err
	}
	l.mu.Lock()
	if l.text == "" {
		l.text = message.Text
	}
	primary := l.text == message.Text
	if primary {
		l.calls++
	}
	lost := primary && l.calls <= l.drops
	l.mu.Unlock()
	if !lost {
		return response, nil
	}
	_, err = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	return nil, errors.New("synthetic accepted notification response lost")
}

func (l *notificationLostResponse) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func requireNotificationAccepted(t *testing.T, r *notificationRuntimeFixture, loss *notificationLostResponse) {
	t.Helper()
	loss.mu.Lock()
	text := loss.text
	loss.mu.Unlock()
	seen := false
	for _, message := range chatMessages(t, r.f, 202) {
		seen = seen || message.Text == text
	}
	require.True(t, seen, "the real sink accepted the exact notification before response loss")
}

func TestNotificationUncertainRetrySurvivesRestart(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			first := r.status(t, r.first)
			require.Equal(t, "pending", first.State)
			require.Equal(t, 1, loss.count())
			assert.GreaterOrEqual(t, first.AvailableAt.Sub(time.Now()), 4*time.Second)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 1, loss.count(), "no wire before durable backoff")
			r.restartNotificationOwner(t, domain)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			assert.Equal(t, first.AvailableAt, r.status(t, r.first).AvailableAt)
			r.wake(t)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, "sent", r.status(t, r.first).State)
			assert.Equal(t, 2, loss.count())
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 2, loss.count(), "known success does not resend")
		})
	}
}

func TestNotificationUncertainRetryExhaustionReleasesFollower(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 10}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			for attempt := range 4 {
				require.NoError(t, dispatch(t.Context(), r.first))
				requireNotificationAccepted(t, r, loss)
				require.Equal(t, attempt+1, loss.count())
				state := r.status(t, r.first)
				if attempt == 3 {
					assert.Equal(t, "failed", state.State)
					assert.Equal(t, "telegram_uncertain_retry_exhausted", state.Reason)
					break
				}
				require.Equal(t, "pending", state.State)
				assert.GreaterOrEqual(t, state.AvailableAt.Sub(time.Now()),
					time.Duration(5*(1<<attempt))*time.Second-time.Second)
				require.NoError(t, dispatch(t.Context(), r.second))
				assert.Equal(t, "pending", r.status(t, r.second).State)
				r.restartNotificationOwner(t, domain)
				require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
				r.wake(t)
			}
			r.wake(t)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 4, loss.count(), "exhaustion never resurrects")
			require.NoError(t, dispatch(t.Context(), r.second))
			assert.Equal(t, "sent", r.status(t, r.second).State)
		})
	}
}
