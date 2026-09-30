package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgeDatabaseFailureRollsBackFactAndReceipt(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"INSERT INTO core.knowledge_facts", "INSERT INTO core.knowledge_operations", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			healthy := knowledgeFixture(t)
			fault := &renderReadFailure{query: stage}
			config := healthy.DB.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			command := knowledge.Command{Name: knowledge.Curate, Key: "knowledge-sql-recovery",
				Event: "kb-current", Topic: "travel", FactKey: "venue", Text: "Synthetic venue"}
			_, err = (knowledge.Service{DB: broken}).Execute(t.Context(), "kbadmin", command)
			require.True(t, fault.fired)
			require.NoError(t, fault.closeErr)
			require.Equal(t, core.ErrDatabase, err)
			var facts, receipts, audits int
			require.NoError(t, healthy.DB.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.knowledge_facts WHERE scope='kb-current' AND fact_key='venue'),
 (SELECT count(*) FROM core.knowledge_operations WHERE actor='kbadmin'),
 (SELECT count(*) FROM core.knowledge_audit WHERE actor='kbadmin')`).Scan(&facts, &receipts, &audits))
			require.Zero(t, facts)
			require.Zero(t, receipts)
			require.Zero(t, audits)
			result, err := healthy.Execute(t.Context(), "kbadmin", command)
			require.NoError(t, err)
			require.NotNil(t, result.Fact)
			require.Equal(t, command.Text, result.Fact.Text)
			replay, err := healthy.Execute(t.Context(), "kbadmin", command)
			require.NoError(t, err)
			assertKnowledgeReplay(t, result, replay)
			require.NoError(t, healthy.DB.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.knowledge_facts WHERE scope='kb-current' AND fact_key='venue'),
 (SELECT count(*) FROM core.knowledge_operations WHERE actor='kbadmin'),
 (SELECT count(*) FROM core.knowledge_audit WHERE actor='kbadmin')`).Scan(&facts, &receipts, &audits))
			require.Equal(t, 1, facts)
			require.Equal(t, 1, receipts)
			require.Equal(t, 1, audits)
		})
	}
}
