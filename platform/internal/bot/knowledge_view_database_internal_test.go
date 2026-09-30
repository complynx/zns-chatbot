package bot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestKnowledgeViewJSONFailureIsNotDatabaseFailure(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	b := Bot{DB: db}
	view, err := b.currentKnowledgeView(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, knowledgeFactsMode, view.Mode)
	for _, payload := range []string{`"wrong type"`, `{"after":"private-incompatible"}`} {
		_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('alice',0,$1,$2) ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`,
			knowledgeStateKind, payload)
		require.NoError(t, err)
		_, err = b.currentKnowledgeView(t.Context(), "alice")
		var incompatible *json.UnmarshalTypeError
		require.ErrorAs(t, err, &incompatible)
		require.False(t, core.IsDatabaseFailure(err))
		require.NotContains(t, err.Error(), "private-incompatible")
	}
	_, err = db.Exec(t.Context(), `UPDATE bot.interactions SET content='{"mode":"memos","after":7}'
WHERE owner='alice' AND update_id=0 AND kind=$1`, knowledgeStateKind)
	require.NoError(t, err)
	view, err = b.currentKnowledgeView(t.Context(), "alice")
	require.NoError(t, err)
	require.Equal(t, knowledgeMemoMode, view.Mode)
	require.EqualValues(t, 7, view.After)
	for _, payload := range []string{`{}`, `null`} {
		_, err = db.Exec(t.Context(), `UPDATE bot.interactions SET content=$1
WHERE owner='alice' AND update_id=0 AND kind=$2`, payload, knowledgeStateKind)
		require.NoError(t, err)
		view, err = b.currentKnowledgeView(t.Context(), "alice")
		require.NoError(t, err)
		require.Equal(t, knowledgeView{}, view, "stored empty state retains the pgx decode behavior")
	}
}
