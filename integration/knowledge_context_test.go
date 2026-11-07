package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func knowledgeHistoryTask(t *testing.T, f knowledgeFixture, actor db.PaymentScope, update int64) db.Task {
	t.Helper()
	user, err := f.h.store.GetUser(t.Context(), actor.UserID)
	require.NoError(t, err)
	task, _, err := f.h.store.Intake(
		t.Context(),
		db.IntakeRequest{
			BotID:          actor.BotID,
			UpdateID:       update,
			TelegramUserID: user.TelegramUserID,
			ChatID:         user.TelegramUserID,
			Payload:        json.RawMessage(`{"kind":"message","text":"Continue knowledge"}`),
		},
		nil,
	)
	require.NoError(t, err)
	return task
}

func TestKnowledgeContextUsesTaskOwnerAndOriginalCutoff(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	before := knowledgeHistoryTask(t, f, f.user, 51001)
	content := knowledgeContent()
	op := uuid.NewString()
	prep, err := f.h.store.PrepareKnowledge(
		t.Context(),
		f.user,
		op,
		db.KnowledgeChange{
			Kind:      "suggest",
			ArticleID: uuid.New(),
			Content:   &content,
			Reason:    "A reference to continue",
		},
	)
	require.NoError(t, err)
	after := knowledgeHistoryTask(t, f, f.user, 51002)
	foreign := knowledgeHistoryTask(t, f, f.other, 51003)
	none, err := f.h.store.KnowledgeActionsForTask(t.Context(), f.user, before.ID)
	require.NoError(t, err)
	require.Empty(t, none)
	actions, err := f.h.store.KnowledgeActionsForTask(t.Context(), f.user, after.ID)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, op, actions[0].OperationID)
	require.Equal(t, prep.ID, actions[0].ProposalID)
	require.Equal(t, "prepared", actions[0].State)
	_, err = f.h.store.PrepareKnowledge(
		t.Context(),
		f.user,
		uuid.NewString(),
		db.KnowledgeChange{Kind: "suggest", ArticleID: uuid.New(), Content: &content, Reason: "A later action"},
	)
	require.NoError(t, err)
	still, err := f.h.store.KnowledgeActionsForTask(t.Context(), f.user, after.ID)
	require.NoError(t, err)
	require.Len(t, still, 1)
	require.Equal(t, op, still[0].OperationID)
	_, err = f.h.store.KnowledgeActionsForTask(t.Context(), f.other, after.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	_, err = f.h.store.KnowledgeActionsForTask(t.Context(), f.user, foreign.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	wrong := f.user
	wrong.Organization = "other-organization"
	_, err = f.h.store.KnowledgeActionsForTask(t.Context(), wrong, after.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
	require.NoError(t, f.h.store.RevokeUser(t.Context(), f.user.UserID, true))
	_, err = f.h.store.KnowledgeActionsForTask(t.Context(), f.user, after.ID)
	require.ErrorIs(t, err, db.ErrKnowledgeDenied)
}
