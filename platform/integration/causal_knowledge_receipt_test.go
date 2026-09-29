package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestCausalKnowledgeReceiptIsReadOnlyAndPrivate(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	service := knowledge.Service{DB: f.db}
	generation := int64(0)
	source := readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
	command := knowledge.Command{
		Name:    knowledge.MemoSet,
		Key:     "lookup-only",
		FactKey: "diet",
		Text:    "RECEIPT-PRIVATE-BODY",
	}
	_, found, err := service.CommandReceipt(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.False(t, found)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='bob'`).Scan(&count),
	)
	require.Zero(t, count)
	memos, err := service.Memos(t.Context(), "bob")
	require.NoError(t, err)
	require.Empty(t, memos)
	_, err = service.ExecuteDerived(t.Context(), "bob", command, source)
	require.NoError(t, err)
	receipt, found, err := service.CommandReceipt(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, command.Text, receipt.Memo.Text)
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob' AND permission='review'`)
	require.NoError(t, err)
	receipt, found, err = service.CommandReceipt(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, receipt.Redacted)
	require.Empty(t, receipt.Memo.Text)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	receipt, found, err = service.CommandReceipt(t.Context(), "bob", command, source)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, receipt.Redacted)
	require.Empty(t, receipt.Memo.Text)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_operations WHERE actor='bob'`).Scan(&count),
	)
	require.Equal(t, 1, count)
}
