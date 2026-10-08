package integration_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const deletedFetchBody = "deleted delayed fetch body"
const survivingSiblingBody = "fresh committed sibling body"

func assertKnowledgeSiblingReads(
	t *testing.T,
	reads []agent.KnowledgeReadResult,
	current knowledge.MemoryDeletionState,
	freshRequest agent.KnowledgeProposal,
	durable bool,
) {
	t.Helper()
	siblingFound := false
	for _, read := range reads {
		require.Equal(t, current, read.MemoryState)
		body, err := json.Marshal(read)
		require.NoError(t, err)
		if !strings.Contains(string(body), survivingSiblingBody) {
			continue
		}
		require.False(t, read.Omitted)
		if durable {
			require.Equal(t, freshRequest, read.Request)
		}
		siblingFound = true
	}
	require.True(t, siblingFound, "committed sibling must remain readable")
	raw, err := json.Marshal(reads)
	require.NoError(t, err)
	require.NotContains(t, string(raw), deletedFetchBody)
	require.NotContains(t, string(raw), "deleted request")
	require.Contains(t, string(raw), survivingSiblingBody)
}

func assertKnowledgeSiblingProjection(
	t *testing.T,
	visible *agent.KnowledgeContext,
	current knowledge.MemoryDeletionState,
	freshRequest agent.KnowledgeProposal,
) {
	t.Helper()
	assertKnowledgeSiblingReads(t, visible.Reads, current, freshRequest, false)
	raw, err := json.Marshal(visible)
	require.NoError(t, err)
	require.NotContains(t, string(raw), deletedFetchBody)
	require.NotContains(t, string(raw), "deleted request")
	require.Contains(t, string(raw), survivingSiblingBody)
}

// Hold genuine fetched data before its read completes, while other calls use
// the normal authenticated domain and durable PostgreSQL read store.
type heldKnowledgeFetch struct {
	agenthost.KnowledgeDomain

	key     string
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldKnowledgeFetch) wait(ctx context.Context, key string, err error) {
	if key != h.key || err != nil {
		return
	}
	h.once.Do(func() {
		close(h.reached)
		select {
		case <-h.release:
		case <-ctx.Done():
		}
	})
}

func (h *heldKnowledgeFetch) Memo(ctx context.Context, owner, key string) (knowledge.Memo, error) {
	value, err := h.KnowledgeDomain.Memo(ctx, owner, key)
	h.wait(ctx, key, err)
	return value, err
}

func (h *heldKnowledgeFetch) KnowledgeFact(
	ctx context.Context, owner, event, topic, key string,
) (knowledge.Fact, error) {
	value, err := h.KnowledgeDomain.KnowledgeFact(ctx, owner, event, topic, key)
	h.wait(ctx, key, err)
	return value, err
}

func TestAgentHostKnowledgeLateFetchPreservesFreshSibling(t *testing.T) {
	t.Parallel()
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "shared"}[shared], func(t *testing.T) {
			t.Parallel()
			s := knowledgeFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			const owner = "alice"
			const updateID int64 = 94201
			const oldKey = "delayed"
			const freshKey = "survivor"
			write := func(key, body string) {
				if shared {
					knowledgeFact(t, s, "", key, body, 0)
					return
				}
				_, err := s.Execute(ctx, owner, knowledge.Command{
					Name: knowledge.MemoSet, Key: "create-" + key, FactKey: key, Text: body,
				})
				require.NoError(t, err)
			}
			write(oldKey, deletedFetchBody)
			signer := identity.Signer{Key: []byte("held-knowledge-test-key-32-bytes!!")}
			server := httptest.NewServer(api.AuthenticatedHandler(
				appservices.NewServices(s.DB, appservices.Options{}), signer, slog.New(slog.DiscardHandler),
				func(_ context.Context, token string) (string, error) { return signer.Verify(token) },
			))
			defer server.Close()
			domain := &heldKnowledgeFetch{
				KnowledgeDomain: appclient.Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token},
				key:             oldKey, reached: make(chan struct{}), release: make(chan struct{}),
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(domain.release) }) }
			defer release()
			store := agenthost.ReadStore{DB: s.DB, Memory: agenthost.MemoryReadStore{DB: s.DB}}
			reader := agenthost.KnowledgeReader{Domain: domain, Store: store}
			projection, err := reader.Knowledge(ctx, owner, updateID)
			require.NoError(t, err)
			oldInput := agent.Input{Knowledge: projection}
			oldRequest := agent.KnowledgeProposal{
				Name: agent.KnowledgeMemoRead, FactKey: oldKey, Text: "deleted request",
			}
			if shared {
				oldRequest.Name, oldRequest.Topic = agent.KnowledgeRead, "travel"
			}
			done := make(chan error, 1)
			go func() { done <- reader.ReadKnowledge(ctx, owner, updateID, oldRequest, &oldInput) }()
			select {
			case <-domain.reached:
			case <-ctx.Done():
				t.Fatal("read did not reach the held fetched result")
			}
			command := knowledge.Command{Name: knowledge.MemoDelete, Key: "delete-delayed", FactKey: oldKey, Version: 1}
			actor := owner
			if shared {
				command.Name, command.Topic, actor = knowledge.RemoveFact, "travel", "kbadmin"
			}
			_, err = s.Execute(ctx, actor, command)
			require.NoError(t, err)
			current, err := s.MemoryDeletions(ctx, owner)
			require.NoError(t, err)
			write(freshKey, survivingSiblingBody)
			projection, err = reader.Knowledge(ctx, owner, updateID)
			require.NoError(t, err)
			freshInput := agent.Input{Knowledge: projection}
			freshRequest := oldRequest
			freshRequest.FactKey, freshRequest.Text = freshKey, "current request"
			require.NoError(t, reader.ReadKnowledge(ctx, owner, updateID, freshRequest, &freshInput))
			require.Zero(t, freshInput.Knowledge.Remaining)
			assertKnowledgeSiblingProjection(t, freshInput.Knowledge, current, freshRequest)
			release()
			select {
			case err = <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("late completion did not finish within its original bound")
			}
			assertKnowledgeSiblingProjection(t, oldInput.Knowledge, current, freshRequest)
			require.Zero(t, oldInput.Knowledge.Remaining)
			assertKnowledgeSiblingProjection(t, freshInput.Knowledge, current, freshRequest)
			stored, err := store.Knowledge(ctx, owner, updateID)
			require.NoError(t, err)
			assertKnowledgeSiblingReads(t, stored, current, freshRequest, true)
			_, err = store.ReserveKnowledge(ctx, owner, updateID, freshRequest)
			require.ErrorContains(t, err, "budget exhausted")
		})
	}
}
