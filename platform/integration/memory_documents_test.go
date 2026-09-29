package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestMemoryDocumentsChunksAndLegacyIsolation(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	text := strings.Repeat("Привет мир. ", 1000)
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "doc",
		Topic:   "travel",
		FactKey: "travel.note",
		Text:    text,
	}
	first, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.NotNil(t, first.Document)
	assert.Equal(t, text, first.Document.Text)
	replay, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assertKnowledgeReplay(t, first, replay)
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "memo", FactKey: "travel.note", Text: "Small legacy memo"},
	)
	require.NoError(t, err)
	memos, err := s.Memos(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, memos, 1)
	assert.Equal(t, "Small legacy memo", memos[0].Text)
	page, err := s.SearchMemory(
		t.Context(),
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Len(t, page.Entries, 2)
	assert.NotEqual(t, page.Entries[0].Ref, page.Entries[1].Ref)
	var reference string
	for _, entry := range page.Entries {
		if entry.SourceKind == "document" {
			reference = entry.Ref
		}
	}
	require.NotEmpty(t, reference)
	var joined strings.Builder
	cursor := ""
	for {
		chunk, readErr := s.ReadMemoryPage(t.Context(), "alice", reference, cursor)
		require.NoError(t, readErr)
		assert.LessOrEqual(t, len([]rune(chunk.Text)), 2000)
		assert.Equal(t, len([]rune(text)), chunk.TotalCharacters)
		joined.WriteString(chunk.Text)
		if !chunk.More {
			break
		}
		cursor = chunk.NextCursor
	}
	assert.Equal(t, text, joined.String())
	_, err = s.ReadMemoryPage(t.Context(), "bob", reference, cursor)
	require.Error(t, err)
	command.Key, command.Text, command.Version = "doc2", "Replacement", 1
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	_, err = s.ReadMemoryPage(t.Context(), "alice", reference, cursor)
	requireCode(t, err, "knowledge_stale")
	history, err := s.MemoryHistory(t.Context(), "alice", reference, "")
	require.NoError(t, err)
	require.Len(t, history.Entries, 2)
	assert.Less(t, len(history.Entries[0].Text), len(text))
	old, err := s.ReadMemoryRevisionPage(t.Context(), "alice", reference, "")
	require.NoError(t, err)
	assert.True(t, old.Historical)
	assert.True(t, old.More)
}

func TestMemoryDocumentsTrustedSourcesAndDeletion(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	history := conversation.Service{DB: s.DB}
	require.NoError(t, history.Append(t.Context(), "alice", "tg-user-5", "user", "Remember the blue train"))
	require.NoError(t, history.Append(t.Context(), "bob", "tg-user-6", "user", "Bob private history"))
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "source-doc",
		Topic:   "travel",
		FactKey: "train",
		Text:    "Blue train",
	}
	_, err := s.ExecuteWithSources(t.Context(), "alice", command, []string{"tg-user-6"})
	requireCode(t, err, "knowledge_not_found")
	_, err = s.ExecuteWithSources(t.Context(), "alice", command, []string{"invented"})
	requireCode(t, err, "knowledge_not_found")
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.NoError(t, s.AttachMemorySources(t.Context(), "alice", command.Key, []string{"tg-user-5"}))
	require.NoError(t, s.AttachMemorySources(t.Context(), "alice", command.Key, []string{"tg-user-5"}))
	require.NoError(t, s.AttachMemorySources(t.Context(), "alice", "never-committed", []string{"invented"}))
	page, err := s.SearchMemory(
		t.Context(),
		"alice",
		knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "travel"},
	)
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	ref := page.Entries[0].Ref
	sources, err := s.MemorySources(t.Context(), "alice", ref)
	require.NoError(t, err)
	require.Len(t, sources.Events, 1)
	assert.Equal(t, "Remember the blue train", sources.Events[0].Text)
	sources, err = s.MemorySources(t.Context(), "bob", ref)
	require.NoError(t, err)
	assert.Empty(t, sources.Events)
	command.Name, command.Key, command.Text, command.Version = knowledge.DocumentDelete, "delete-doc", "", 1
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	err = s.AttachMemorySources(t.Context(), "alice", "source-doc", []string{"tg-user-5"})
	require.NoError(t, err)
	sources, err = s.MemorySources(t.Context(), "alice", ref)
	require.NoError(t, err)
	assert.Empty(t, sources.Events)
	old, err := s.ReadMemoryRevisionPage(t.Context(), "alice", ref, "")
	require.NoError(t, err)
	assert.Empty(t, old.Text)
}

func TestMemorySharedSourceTransferPreservesConversationPrivacy(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	history := conversation.Service{DB: s.DB}
	require.NoError(
		t,
		history.Append(t.Context(), "alice", "tg-user-7", "user", "Public suggestion from my private chat"),
	)
	result, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "suggest-source",
			Topic:   "travel",
			FactKey: "train",
			Text:    "Take the train",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result.Proposal)
	assessed, err := s.Assess(
		t.Context(),
		"alice",
		knowledge.Assessment{Key: "filter-source", ProposalID: result.Proposal.ID, Version: 1, Worthwhile: true},
	)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"kbadmin",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "review-source",
			ProposalID: result.Proposal.ID,
			Version:    assessed.Proposal.Version,
			Decision:   "approve",
		},
	)
	require.NoError(t, err)
	require.NoError(t, s.AttachMemorySources(t.Context(), "alice", "suggest-source", []string{"tg-user-7"}))
	page, err := s.SearchMemory(t.Context(), "alice", knowledge.MemoryQuery{Namespace: knowledge.MemoryShared})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	sources, err := s.MemorySources(t.Context(), "alice", page.Entries[0].Ref)
	require.NoError(t, err)
	require.Len(t, sources.Events, 1)
	sources, err = s.MemorySources(t.Context(), "kbadmin", page.Entries[0].Ref)
	require.NoError(t, err)
	assert.Empty(t, sources.Events)
}

func TestMemoryDocumentCapacityAndReplacement(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	_, err := s.DB.Exec(t.Context(), `INSERT INTO core.memory_documents(owner,topic,document_key,body,version)
 SELECT 'alice','notes','item_'||i,'Existing document',1 FROM generate_series(1,$1::int) i`, knowledge.MaxDocuments)
	require.NoError(t, err)
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "full",
		Topic:   "notes",
		FactKey: "overflow",
		Text:    "No space",
	}
	_, err = s.Execute(t.Context(), "alice", command)
	requireCode(t, err, "knowledge_document_capacity")
	command.Key, command.FactKey, command.Version = "replace", "item_1", 1
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	command.Name, command.Key, command.Text, command.Version = knowledge.DocumentDelete, "free", "", 2
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	command.Name, command.Key, command.Text, command.FactKey, command.Version = knowledge.DocumentSet, "reuse", "Available", "overflow", 0
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	command.Key, command.FactKey, command.Text = "oversize", "too-large", strings.Repeat(
		"a",
		knowledge.MaxDocumentText+1,
	)
	_, err = s.Execute(t.Context(), "bob", command)
	requireCode(t, err, "knowledge_invalid")
}
