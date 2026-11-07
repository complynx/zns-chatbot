package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMiniAppMealOrderContextContinuesNativeWorkWithMatchingTools(t *testing.T) {
	t.Parallel()
	f, p, session, args := mealOrderAppFixture(t)
	ctx := t.Context()
	op := uuid.New()
	prepared := prepareMealApp(t, f, session, op, args)
	code, raw, _ := appCall(t, f.H, http.MethodPost, mealsAppPath(args.EventID)+"/submit",
		paymentAppJSON(t, orderAppSubmitBody(op, prepared)), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var result db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(raw, &result))
	p.mu.Lock()
	require.Empty(t, p.runs, "A native meal action does not start agent work")
	p.mu.Unlock()
	id := acceptOrderApprovalMessage(t, f)
	f.H.await(t, id, "completed")
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	actions, err := f.H.store.RecentMiniAppOrderActions(ctx, owner, id)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	for _, action := range actions {
		require.Equal(t, db.OrderKindMeal, action.OrderKind)
		require.Equal(t, op, action.OperationID)
		require.Equal(t, args.EventID, action.EventID)
	}
	p.mu.Lock()
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, `"order_kind":"meal"`)
	require.Contains(t, prompt, result.Order.ID.String())
	require.Contains(t, prompt, "meal uses zinusya_meals")
	require.Contains(t, prompt, "Never infer approval")
	require.NotContains(t, prompt, `"total_minor":715`)
	stranger := ownerProfileScope(t, f.H, f.AmbassadorID)
	foreign, err := f.H.store.RecentMiniAppOrderActions(ctx, stranger, id)
	require.NoError(t, err)
	require.Empty(t, foreign)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['orders'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	hidden, err := f.H.store.RecentMiniAppOrderActions(ctx, owner, id)
	require.NoError(t, err)
	require.Empty(t, hidden, "The service orders module cannot disclose meal history")
}
