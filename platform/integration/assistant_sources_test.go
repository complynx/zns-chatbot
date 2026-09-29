package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/assistantsource"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestAssistantSourcePersistenceIsolationAndFullRetrieval(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := knowledge.Service{DB: f.db}
	ctx := t.Context()
	identity := assistantsource.Digest([]byte("doc-one"))
	require.NoError(t, service.ConfigureSource(ctx, knowledge.AssistantAbout, identity))
	text := "source-canary " + strings.Repeat("🌍Exact ", 6000) + "tail-canary"
	require.NoError(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantAbout,
			identity,
			assistantsource.Digest([]byte(text)),
			[]string{text},
		),
	)
	page, err := service.SearchMemory(
		ctx,
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: knowledge.AssistantAbout},
	)
	require.NoError(t, err)
	require.Greater(t, len(page.Entries), 1)
	var full strings.Builder
	for _, entry := range page.Entries {
		cursor := ""
		for {
			part, readErr := service.ReadMemoryPage(ctx, "alice", entry.Ref, cursor)
			require.NoError(t, readErr)
			assert.True(t, part.Untrusted)
			require.NotNil(t, part.Source)
			assert.Equal(t, "ready", part.Source.Status)
			full.WriteString(part.Text)
			if !part.More {
				break
			}
			cursor = part.NextCursor
		}
	}
	assert.Equal(t, text, full.String())
	ref := page.Entries[0].Ref
	version := page.Entries[0].Version
	require.NoError(t, service.SourceFailure(ctx, knowledge.AssistantAbout, identity, "fetch_failed"))
	stale, err := service.ReadMemory(ctx, "alice", ref)
	require.NoError(t, err)
	assert.Equal(t, "stale", stale.Source.Status)
	// Re-instantiating/reconfiguring an identical source preserves last-good data.
	require.NoError(t, (knowledge.Service{DB: f.db}).ConfigureSource(ctx, knowledge.AssistantAbout, identity))
	require.NoError(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantAbout,
			identity,
			assistantsource.Digest([]byte(text)),
			[]string{text},
		),
	)
	current, err := service.ReadMemory(ctx, "alice", ref)
	require.NoError(t, err)
	assert.Equal(t, version, current.Version)
	// A corrected converter must publish changed rendered text even if the
	// upstream bytes and their digest did not change.
	require.NoError(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantAbout,
			identity,
			assistantsource.Digest([]byte(text)),
			[]string{"corrected rendering"},
		),
	)
	_, err = service.ReadMemory(ctx, "alice", ref)
	require.Error(t, err)
	private, err := service.SearchMemory(
		ctx,
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Text: "source-canary"},
	)
	require.NoError(t, err)
	assert.Empty(t, private.Entries)
	other := assistantsource.Digest([]byte("doc-two"))
	require.NoError(t, service.ConfigureSource(ctx, knowledge.AssistantAbout, other))
	_, err = service.ReadMemory(ctx, "alice", ref)
	require.Error(t, err)
	_, err = service.MemoryHistory(ctx, "alice", ref, "")
	require.Error(t, err)
	_, err = service.ReadMemoryRevisionPage(ctx, "alice", ref, "")
	require.Error(t, err)
	require.Error(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantAbout,
			identity,
			assistantsource.Digest([]byte("late")),
			[]string{"late"},
		),
	)
	empty, err := service.SearchMemory(ctx, "alice", knowledge.MemoryQuery{Topic: knowledge.AssistantAbout})
	require.NoError(t, err)
	assert.Empty(t, empty.Entries)
	require.NoError(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantAbout,
			other,
			assistantsource.Digest([]byte("new")),
			[]string{"new"},
		),
	)
	next, err := service.SearchMemory(ctx, "alice", knowledge.MemoryQuery{Topic: knowledge.AssistantAbout})
	require.NoError(t, err)
	require.Len(t, next.Entries, 1)
	assert.Greater(t, next.Entries[0].Version, version)
}

func TestAssistantSourceConcurrentPublication(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := knowledge.Service{DB: f.db}
	ctx := t.Context()
	identity := assistantsource.Digest([]byte("concurrent"))
	require.NoError(t, service.ConfigureSource(ctx, knowledge.AssistantQA, identity))
	require.NoError(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantQA,
			identity,
			assistantsource.Digest([]byte("initial")),
			[]string{"initial", "initial"},
		),
	)
	var work errgroup.Group
	work.Go(func() error {
		for i := range 12 {
			text := fmt.Sprintf("epoch-%d", i)
			if err := service.ReplaceSource(
				ctx,
				knowledge.AssistantQA,
				identity,
				assistantsource.Digest([]byte(text)),
				[]string{text, text},
			); err != nil {
				return err
			}
		}
		return nil
	})
	work.Go(func() error {
		for range 30 {
			page, err := service.SearchMemory(
				ctx,
				"alice",
				knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Topic: knowledge.AssistantQA},
			)
			if err != nil {
				return err
			}
			if len(page.Entries) != 2 || page.Entries[0].Version != page.Entries[1].Version ||
				page.Entries[0].Text != page.Entries[1].Text {
				return fmt.Errorf("partial source publication observed")
			}
		}
		return nil
	})
	require.NoError(t, work.Wait())
}

func TestAssistantQAUpdatesAreVersionedAndPreserveCuratedMemory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := knowledge.Service{DB: f.db}
	ctx := t.Context()
	identity := assistantsource.Digest([]byte("static-path"))
	require.NoError(t, service.ConfigureSource(ctx, knowledge.AssistantQA, identity))
	data := []byte("collection:\n- question: First\n  alt_questions: [Alt]\n  answer: Первый\n- answer: Second\n")
	texts, err := assistantsource.ParseQA(data)
	require.NoError(t, err)
	require.NoError(t, service.ReplaceSource(ctx, knowledge.AssistantQA, identity, assistantsource.Digest(data), texts))
	first, err := service.SearchMemory(ctx, "alice", knowledge.MemoryQuery{Topic: knowledge.AssistantQA})
	require.NoError(t, err)
	require.Len(t, first.Entries, 2)
	assert.Contains(t, first.Entries[0].Text, "Alt")
	_, err = f.db.Exec(
		ctx,
		`INSERT INTO core.knowledge_facts(scope,topic,fact_key,body,version,active) VALUES('','assistant_qa','curated','curated-canary',1,true)`,
	)
	require.NoError(t, err)
	require.NoError(
		t,
		service.ReplaceSource(
			ctx,
			knowledge.AssistantQA,
			identity,
			assistantsource.Digest([]byte("changed")),
			[]string{"changed"},
		),
	)
	_, err = service.ReadMemory(ctx, "alice", first.Entries[0].Ref)
	require.Error(t, err)
	historical, err := service.ReadMemoryRevisionPage(ctx, "alice", first.Entries[0].Ref, "")
	require.NoError(t, err)
	assert.Contains(t, historical.Text, "Alt")
	page, err := service.SearchMemory(ctx, "alice", knowledge.MemoryQuery{Text: "curated-canary"})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	assert.Equal(t, knowledge.MemoryFactKind, page.Entries[0].SourceKind)
}
