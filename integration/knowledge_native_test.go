package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func nativeKnowledgeSession(t *testing.T, f knowledgeFixture, actor db.PaymentScope) db.MiniAppKnowledgeSession {
	t.Helper()
	user, err := f.h.store.GetUser(t.Context(), actor.UserID)
	require.NoError(t, err)
	native := nativePaymentSession(t, paymentFixture{H: f.h}, actor, user.TelegramUserID)
	return db.MiniAppKnowledgeSession{UserID: actor.UserID, Bearer: native.Bearer,
		Generation: native.Generation, AuthorityUnchanged: func() bool { return true }}
}

func TestKnowledgeNativeAndAgentSharePreparationAndReceipt(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"native", "native_to_agent", "agent_to_native"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeFixture(t)
			actor := f.moderator
			session := nativeKnowledgeSession(t, f, actor)
			content := knowledgeContent()
			change := db.KnowledgeChange{
				Kind:      "edit",
				ArticleID: uuid.New(),
				Content:   &content,
				Reason:    "Publish the reviewed reference",
			}
			op := uuid.NewString()
			var prep db.KnowledgePreparation
			var err error
			if path == "agent_to_native" {
				prep, err = f.h.store.PrepareKnowledge(t.Context(), actor, op, change)
			} else {
				prep, err = f.h.store.PrepareMiniAppKnowledge(t.Context(), actor, op, change, session)
			}
			require.NoError(t, err)
			var result db.KnowledgeResult
			if path == "native_to_agent" {
				result, err = f.h.store.ConfirmKnowledge(t.Context(), actor, op, prep.Arguments, prep.ID)
			} else {
				result, err = f.h.store.ConfirmMiniAppKnowledge(
					t.Context(),
					actor,
					op,
					prep.Arguments,
					prep.ID,
					session,
				)
			}
			require.NoError(t, err)
			reopened, err := db.Open(t.Context(), f.h.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			replay, err := reopened.ConfirmMiniAppKnowledge(t.Context(), actor, op, prep.Arguments, prep.ID, session)
			require.NoError(t, err)
			require.JSONEq(t, knowledgeReceiptJSON(t, result), knowledgeReceiptJSON(t, replay))
			receipt, err := reopened.KnowledgeOperation(t.Context(), actor, "edit", op)
			require.NoError(t, err)
			require.JSONEq(t, knowledgeReceiptJSON(t, result), knowledgeReceiptJSON(t, receipt))
			current, err := reopened.ReadKnowledge(t.Context(), f.user, change.ArticleID, 0)
			require.NoError(t, err)
			require.EqualValues(t, 1, current.Revision)
			require.Equal(t, content.Body, current.Content.Body)
		})
	}
}

func TestKnowledgeNativeRevocationPreventsCommitAndReceiptReplay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		completed, logout bool
	}{
		{"logout_before_commit", false, true}, {"logout_after_commit", true, true},
		{"iam_before_commit", false, false}, {"iam_after_commit", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeFixture(t)
			actor := f.moderator
			session := nativeKnowledgeSession(t, f, actor)
			content := knowledgeContent()
			change := db.KnowledgeChange{
				Kind:      "edit",
				ArticleID: uuid.New(),
				Content:   &content,
				Reason:    "A session-bound reference edit",
			}
			op := uuid.NewString()
			prep, err := f.h.store.PrepareMiniAppKnowledge(t.Context(), actor, op, change, session)
			require.NoError(t, err)
			if tc.completed {
				_, err = f.h.store.ConfirmMiniAppKnowledge(t.Context(), actor, op, prep.Arguments, prep.ID, session)
				require.NoError(t, err)
			}
			if tc.logout {
				require.NoError(
					t,
					f.h.store.LogoutMiniAppSession(t.Context(), actor.BotID, session.Bearer, session.Generation),
				)
			} else {
				session.AuthorityUnchanged = func() bool { return false }
			}
			_, err = f.h.store.PrepareMiniAppKnowledge(t.Context(), actor, op, prep.Arguments, session)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = f.h.store.ConfirmMiniAppKnowledge(t.Context(), actor, op, prep.Arguments, prep.ID, session)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			page, err := f.h.store.SearchKnowledge(
				t.Context(),
				f.user,
				knowledge.SearchRequest{Queries: []string{"zouk"}},
			)
			require.NoError(t, err)
			if tc.completed {
				require.Len(t, page.Items, 1)
				require.EqualValues(t, 1, page.Items[0].Article.Revision)
			} else {
				require.Empty(t, page.Items)
			}
		})
	}
}
