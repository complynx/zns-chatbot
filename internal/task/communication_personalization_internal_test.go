package task

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestCommunicationPersonalizationWaitsAndTerminalFailures(t *testing.T) {
	t.Parallel()
	service := &Service{}
	for _, status := range []string{"queued", "running", "reconciling"} {
		text, err := service.communicationPersonalizationResult(
			t.Context(),
			loom.Auth{},
			db.CommunicationDelivery{},
			loom.Run{Status: status},
		)
		require.NoError(t, err)
		require.Empty(t, text)
	}
	for _, status := range []string{"failed", "cancelled", "canceled", "completed", "succeeded", "waiting_auth"} {
		_, err := service.communicationPersonalizationResult(
			t.Context(),
			loom.Auth{},
			db.CommunicationDelivery{},
			loom.Run{Status: status},
		)
		require.ErrorIs(t, err, errCommunicationPersonalizationFailed)
	}
	for _, reason := range []string{"budget_exhausted", "credits_exhausted", "step_limit", "provider_retry_limit"} {
		_, err := service.communicationPersonalizationResult(
			t.Context(),
			loom.Auth{},
			db.CommunicationDelivery{},
			loom.Run{Status: "paused", Reason: reason},
		)
		require.ErrorIs(t, err, errCommunicationPersonalizationFailed)
	}
}

func TestCommunicationPersonalizationBackoffDeadlineAndErrorClassification(t *testing.T) {
	t.Parallel()
	for polls, want := range []time.Duration{
		2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
		32 * time.Second, time.Minute, time.Minute,
	} {
		require.Equal(t, want, communicationPersonalizationDelay(polls))
	}
	now := time.Now()
	past := now.Add(-communicationPersonalizationLifetime)
	require.False(t, communicationPersonalizationExpired(db.CommunicationDelivery{}, now))
	require.True(t, communicationPersonalizationExpired(db.CommunicationDelivery{PersonalizationStartedAt: &past}, now))
	require.False(t, communicationPersonalizationExpired(db.CommunicationDelivery{
		PersonalizationStartedAt: &past, Text: "Cached result", State: "retryable",
	}, now))
	for _, err := range []error{
		identity.ErrDenied, &loom.Error{Outcome: loom.Rejected, StatusCode: http.StatusForbidden},
		&loom.Error{Code: "native_files_unsupported", Outcome: loom.NotStarted},
	} {
		require.True(t, permanentCommunicationInputError(err))
	}
	for _, err := range []error{
		context.Canceled, &loom.Error{Outcome: loom.Unknown},
		&loom.Error{Outcome: loom.Rejected, Retryable: true},
		&loom.Error{Outcome: loom.Rejected, StatusCode: http.StatusTooManyRequests},
	} {
		require.False(t, permanentCommunicationInputError(err))
	}
}

func TestCommunicationPersonalizationResumesShutdownWithServiceAuth(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/runs/rewrite-run/resume", r.URL.Path)
		assert.Equal(t, "Bearer service", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("X-Subject-Token"))
		assert.Equal(t, fmt.Sprintf("communication-resume:%s:2", id), r.Header.Get("Idempotency-Key"))
		_, err := w.Write([]byte(`{"run_id":"rewrite-run","status":"running","version":3}`))
		assert.NoError(t, err)
	}))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	service := &Service{loom: client}
	text, err := service.communicationPersonalizationResult(
		t.Context(),
		loom.Auth{ServiceToken: "service"},
		db.CommunicationDelivery{ID: id},
		loom.Run{ID: "rewrite-run", Status: "paused", Reason: "shutdown", Version: 2},
	)
	require.NoError(t, err)
	require.Empty(t, text)
}
