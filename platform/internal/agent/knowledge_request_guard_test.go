package agent_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestKnowledgeRequestGuardBlocksNextInvocation(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{"openai", "remote"} {
		t.Run(transport, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			var classifier agent.KnowledgeClassifier = agent.OpenAI{Key: "synthetic", BaseURL: server.URL, HTTP: server.Client()}
			if transport == "remote" {
				classifier = agent.Remote{URL: server.URL, HTTP: server.Client()}
			}
			revoked := false
			guardCalls := 0
			denied := errors.New("source retired")
			input := agent.KnowledgeAssessmentInput{
				Text: "Synthetic proposal",
				BeforeProvider: func(context.Context) error {
					guardCalls++
					if revoked {
						return denied
					}
					return nil
				},
			}
			_, err := classifier.AssessKnowledge(t.Context(), input)
			require.Error(t, err)
			require.EqualValues(t, 1, calls.Load())
			revoked = true
			_, err = classifier.AssessKnowledge(t.Context(), input)
			require.ErrorIs(t, err, denied)
			require.Equal(t, 2, guardCalls)
			require.EqualValues(t, 1, calls.Load(), "retired source prevents the next actual request")
		})
	}
}

func TestKnowledgeCodexGuardPreventsProcessStart(t *testing.T) {
	t.Parallel()
	denied := errors.New("source retired")
	classifier := agent.Codex{SyntheticOnly: true, Executable: filepath.Join(t.TempDir(), "must-not-execute")}
	_, err := classifier.AssessKnowledge(t.Context(), agent.KnowledgeAssessmentInput{
		Text: "Synthetic proposal", BeforeProvider: func(context.Context) error { return denied },
	})
	require.ErrorIs(t, err, denied, "the guard runs before an unavailable executable is started")
}
