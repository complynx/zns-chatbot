package integration_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestKnowledgeActionRecoverySeparatesPreparedExpiredAndCompleted(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	uri, err := url.Parse(f.h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := uri.Query()
	query.Set("pool_max_conns", "1")
	uri.RawQuery = query.Encode()
	store, err := db.Open(t.Context(), uri.String())
	require.NoError(t, err)
	defer store.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	op := uuid.NewString()
	status, err := store.KnowledgeAction(ctx, f.user, "suggest", op)
	require.NoError(t, err)
	require.Equal(t, "unknown", status.State)
	require.Nil(t, status.Preparation)
	content := knowledgeContent()
	change := db.KnowledgeChange{
		Kind:      "suggest",
		ArticleID: uuid.New(),
		Content:   &content,
		Reason:    "A recoverable proposed reference",
	}
	prep, err := store.PrepareKnowledge(ctx, f.user, op, change)
	require.NoError(t, err)
	status, err = store.KnowledgeAction(ctx, f.user, "suggest", op)
	require.NoError(t, err)
	require.Equal(t, "prepared", status.State)
	require.NotNil(t, status.Preparation)
	require.Equal(t, prep.ID, status.Preparation.ID)
	require.Equal(t, content.Body, status.Preparation.Arguments.Content.Body)
	foreign, err := store.KnowledgeAction(ctx, f.other, "suggest", op)
	require.NoError(t, err)
	require.Equal(t, "unknown", foreign.State)
	require.Nil(t, foreign.Preparation)
	recent, err := store.RecentKnowledgeActions(ctx, f.user, time.Time{})
	require.NoError(t, err)
	require.Len(t, recent, 1)
	require.Equal(t, op, recent[0].OperationID)
	cutoff, err := store.RecentKnowledgeActions(ctx, f.user, recent[0].CreatedAt)
	require.NoError(t, err)
	require.Empty(t, cutoff)
	_, err = store.Pool().
		Exec(ctx, "UPDATE shuttle_knowledge_preparations SET created_at=clock_timestamp()-interval "+
			"'2 seconds',expires_at=clock_timestamp()-interval '1 second' WHERE actor_id=$1 "+
			"AND operation_id=$2", f.user.UserID, op)
	require.NoError(t, err)
	status, err = store.KnowledgeAction(ctx, f.user, "suggest", op)
	require.NoError(t, err)
	require.Equal(t, "expired", status.State)
	require.Nil(t, status.Result)
	_, err = store.ConfirmKnowledge(ctx, f.user, op, prep.Arguments, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	op = uuid.NewString()
	prep, err = store.PrepareKnowledge(ctx, f.user, op, change)
	require.NoError(t, err)
	result, err := store.ConfirmKnowledge(ctx, f.user, op, prep.Arguments, prep.ID)
	require.NoError(t, err)
	_, err = store.Pool().
		Exec(ctx, "UPDATE shuttle_knowledge_preparations SET created_at=clock_timestamp()-interval "+
			"'2 seconds',expires_at=clock_timestamp()-interval '1 second' WHERE actor_id=$1 "+
			"AND operation_id=$2", f.user.UserID, op)
	require.NoError(t, err)
	status, err = store.KnowledgeAction(ctx, f.user, "suggest", op)
	require.NoError(t, err)
	require.Equal(t, "completed", status.State)
	require.NotNil(t, status.Result)
	require.JSONEq(t, knowledgeReceiptJSON(t, result), knowledgeReceiptJSON(t, *status.Result))
	recent, err = store.RecentKnowledgeActions(ctx, f.user, time.Time{})
	require.NoError(t, err)
	require.Len(t, recent, 2)
	require.Equal(t, "completed", recent[0].State)
	require.Equal(t, "expired", recent[1].State)
}
