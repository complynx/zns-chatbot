package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestMemoryDeletionRedactsOperationReplay(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{knowledge.DocumentSet, knowledge.DocumentDelete}, {knowledge.MemoSet, knowledge.MemoDelete}, {knowledge.Curate, knowledge.RemoveFact}} {
		t.Run(pair[0], func(t *testing.T) {
			t.Parallel()
			s := knowledgeFixture(t)
			actor := "alice"
			c := knowledge.Command{Name: pair[0], Key: "original", FactKey: "secret", Text: "deleted-memory-secret"}
			if pair[0] != knowledge.MemoSet {
				c.Topic = "travel"
			}
			if pair[0] == knowledge.Curate {
				actor = "kbadmin"
				c.Event = "kb-current"
			}
			_, err := s.Execute(t.Context(), actor, c)
			require.NoError(t, err)
			deletion := c
			deletion.Name = pair[1]
			deletion.Key = "delete"
			deletion.Text = ""
			deletion.Version = 1
			_, err = s.Execute(t.Context(), actor, deletion)
			require.NoError(t, err)
			epoch, err := s.MemoryDeletions(t.Context(), actor)
			require.NoError(t, err)
			other, err := s.MemoryDeletions(t.Context(), "bob")
			require.NoError(t, err)
			if pair[0] == knowledge.Curate {
				assert.EqualValues(t, 1, epoch.SharedGeneration)
				assert.Equal(t, epoch, other)
			} else {
				assert.EqualValues(t, 1, epoch.PrivateGeneration)
				assert.Zero(t, other.PrivateGeneration)
			}
			replay, err := s.Execute(t.Context(), actor, c)
			require.NoError(t, err)
			encoded, err := json.Marshal(replay)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), c.Text)
			assert.True(t, replay.Redacted)
			changed := c
			changed.Text = "different request"
			_, err = s.Execute(t.Context(), actor, changed)
			requireCode(t, err, "idempotency_conflict")
			var active bool
			err = s.DB.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM core.memory_revisions WHERE active AND body=$1)`, c.Text).
				Scan(&active)
			require.NoError(t, err)
			assert.False(t, active)
			replacement := c
			replacement.Key = "replacement"
			replacement.Version = 2
			replacement.Text = "new-memory-body"
			_, err = s.Execute(t.Context(), actor, replacement)
			require.NoError(t, err)
			replay, err = s.Execute(t.Context(), actor, c)
			require.NoError(t, err)
			assert.True(t, replay.Redacted)
			current, err := s.Execute(t.Context(), actor, replacement)
			require.NoError(t, err)
			encoded, err = json.Marshal(current)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), replacement.Text)
		})
	}
}

func TestMemoryDeletionMigrationBackfillsBeforeRecreation(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	_, err := s.DB.Exec(t.Context(), `DROP TRIGGER memory_fact_receipt_deletion ON core.knowledge_facts;
 DROP TRIGGER memory_memo_receipt_deletion ON core.knowledge_memos;
 DROP TRIGGER memory_document_receipt_deletion ON core.memory_documents;
 DROP FUNCTION core.delete_memory_receipts();
 DROP FUNCTION core.scrub_memory_receipts(text,text,text,text,text,bigint);
 DROP TABLE core.memory_deletion_epochs;
 DELETE FROM public.zns_schema_migrations WHERE name='053_memory_deletion_receipts.sql'`)
	require.NoError(t, err)
	c := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "old",
		Topic:   "travel",
		FactKey: "note",
		Text:    "old-deleted-body",
	}
	_, err = s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.DocumentDelete,
			Key:     "delete",
			Topic:   c.Topic,
			FactKey: c.FactKey,
			Version: 1,
		},
	)
	require.NoError(t, err)
	newCommand := c
	newCommand.Key, newCommand.Text, newCommand.Version = "new", "new-live-body", 2
	_, err = s.Execute(t.Context(), "alice", newCommand)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), s.DB))
	old, err := s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	assert.True(t, old.Redacted)
	assert.Empty(t, old.Document.Text)
	current, err := s.Execute(t.Context(), "alice", newCommand)
	require.NoError(t, err)
	assert.Equal(t, newCommand.Text, current.Document.Text)
	epoch, err := s.MemoryDeletions(t.Context(), "alice")
	require.NoError(t, err)
	assert.EqualValues(t, 1, epoch.PrivateGeneration)
}

func TestMemoryDeletionRedactsApprovedProposalCopies(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	c := knowledge.Command{
		Name:    knowledge.Suggest,
		Key:     "suggest",
		Event:   "kb-current",
		Topic:   "travel",
		FactKey: "venue",
		Text:    "deleted-approved-proposal",
	}
	proposal, err := s.Execute(t.Context(), "alice", c)
	require.NoError(t, err)
	assessment := knowledge.Assessment{
		Key:        "assess",
		ProposalID: proposal.Proposal.ID,
		Version:    1,
		Worthwhile: true,
		Reason:     "Relevant",
	}
	filtered, err := s.Assess(t.Context(), "alice", assessment)
	require.NoError(t, err)
	review := knowledge.Command{
		Name:       knowledge.Review,
		Key:        "review",
		Event:      c.Event,
		ProposalID: proposal.Proposal.ID,
		Version:    filtered.Proposal.Version,
		Decision:   "approve",
	}
	_, err = s.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"kbadmin",
		knowledge.Command{
			Name:    knowledge.RemoveFact,
			Key:     "delete",
			Event:   c.Event,
			Topic:   c.Topic,
			FactKey: c.FactKey,
			Version: 1,
		},
	)
	require.NoError(t, err)
	for _, replay := range []struct {
		actor   string
		command knowledge.Command
	}{{"alice", c}, {"bob", review}} {
		result, replayErr := s.Execute(t.Context(), replay.actor, replay.command)
		require.NoError(t, replayErr)
		assert.True(t, result.Redacted)
		assert.Empty(t, result.Proposal.Text)
	}
	result, err := s.Assess(t.Context(), "alice", assessment)
	require.NoError(t, err)
	assert.Empty(t, result.Proposal.Text)
	var retained bool
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM core.knowledge_operations WHERE result::text LIKE '%deleted-approved-proposal%')`).
			Scan(&retained),
	)
	assert.False(t, retained)
}
