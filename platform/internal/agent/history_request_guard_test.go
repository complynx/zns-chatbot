package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

type summaryAdmissionRecorder struct {
	phase   string
	entered chan struct{}
	release chan struct{}
	notSent atomic.Int32
	settled atomic.Int32
}

func (*summaryAdmissionRecorder) RequestTier() string { return "" }
func (r *summaryAdmissionRecorder) Reserve(ctx context.Context, _ credits.Attempt) error {
	return r.wait(ctx, "reserve")
}
func (r *summaryAdmissionRecorder) Dispatch(ctx context.Context, _ string) error {
	return r.wait(ctx, "dispatch")
}
func (r *summaryAdmissionRecorder) wait(ctx context.Context, phase string) error {
	if r.phase != phase {
		return nil
	}
	close(r.entered)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *summaryAdmissionRecorder) NotSent(context.Context, string) error {
	r.notSent.Add(1)
	return nil
}
func (r *summaryAdmissionRecorder) Settle(context.Context, string, credits.Settlement) error {
	r.settled.Add(1)
	return nil
}

func TestHistorySummaryRechecksAfterAccountingAdmission(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"reserve", "dispatch"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			recorder := &summaryAdmissionRecorder{
				phase:   phase,
				entered: make(chan struct{}),
				release: make(chan struct{}),
			}
			model := agent.OpenAI{Key: "synthetic", BaseURL: server.URL, HTTP: server.Client(), Accounting: recorder}
			var revoked atomic.Bool
			denied := errors.New("summary source retired")
			input := agent.HistorySummaryInput{
				Events: []conversation.Event{{Kind: "user", Text: "private synthetic history"}},
				BeforeProvider: func(context.Context) error {
					if revoked.Load() {
						return denied
					}
					return nil
				},
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := model.SummarizeHistory(ctx, input); result <- err }()
			select {
			case <-recorder.entered:
			case <-ctx.Done():
				t.Fatal("summary did not enter accounting admission")
			}
			revoked.Store(true)
			close(recorder.release)
			select {
			case err := <-result:
				require.ErrorIs(t, err, denied)
			case <-ctx.Done():
				t.Fatal("summary did not finish")
			}
			assert.Zero(t, calls.Load(), "revoked history must not reach the provider")
			assert.EqualValues(t, 1, recorder.notSent.Load())
			assert.Zero(t, recorder.settled.Load(), "no sent request may be settled")
		})
	}
}

func TestHistorySummaryRemoteGuardAndSerialization(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Len(t, payload, 2)
		assert.Contains(t, payload, "events")
		_, _ = w.Write([]byte(`{"text":"synthetic summary"}`))
	}))
	t.Cleanup(server.Close)
	model := agent.Remote{URL: server.URL, HTTP: server.Client()}
	denied := errors.New("history generation changed")
	input := agent.HistorySummaryInput{
		Events:         []conversation.Event{{Kind: "user", Text: "synthetic"}},
		BeforeProvider: func(context.Context) error { return denied },
	}
	_, err := model.SummarizeHistory(t.Context(), input)
	require.ErrorIs(t, err, denied)
	require.Zero(t, calls.Load())
	input.BeforeProvider = func(context.Context) error { return nil }
	summary, err := model.SummarizeHistory(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, "synthetic summary", summary)
	assert.EqualValues(t, 1, calls.Load())
}

func TestHistorySummaryCodexGuardPreventsProcessStart(t *testing.T) {
	t.Parallel()
	denied := errors.New("history retired")
	model := agent.Codex{SyntheticOnly: true, Executable: filepath.Join(t.TempDir(), "must-not-execute")}
	_, err := model.SummarizeHistory(t.Context(), agent.HistorySummaryInput{
		Events:         []conversation.Event{{Kind: "user", Text: "synthetic"}},
		BeforeProvider: func(context.Context) error { return denied },
	})
	require.ErrorIs(t, err, denied)
}
