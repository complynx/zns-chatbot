package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const mealOrderAgentPath = "/internal/agent/meals/"

func mealOrderAppFixture(t *testing.T) (paymentFixture, *peers, appSession, db.OrderSubmission) {
	t.Helper()
	f, p, session, change := mealsAppFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	catalog, err := f.H.store.OwnMealCatalog(t.Context(), owner, f.Event.ID, 0)
	require.NoError(t, err)
	deadline := catalog.ObservedAt.Add(2 * time.Hour).Truncate(time.Microsecond)
	catalog.Document.PaymentDeadline = &deadline
	published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
	change = completeMeal(change)
	change.ExpectedCatalogVersion = published.Version
	code, raw, _ := appCall(t, f.H, http.MethodPost, mealsAppPath(f.Event.ID)+"/draft",
		mealsAppBody(t, change), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	saved := mealsAppDraft(t, raw)
	return f, p, session, db.OrderSubmission{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version,
		ExpectedCatalogVersion: published.Version, ExpectedDraftVersion: saved.Version}
}

func prepareMealApp(t *testing.T, f paymentFixture, session appSession, operation uuid.UUID,
	args db.OrderSubmission,
) db.OrderPreparation {
	t.Helper()
	code, raw, _ := appCall(t, f.H, http.MethodPost, mealsAppPath(args.EventID)+"/submit/prepare",
		paymentAppJSON(t, orderAppPrepareBody(operation, args)), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared db.OrderPreparation
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.NotNil(t, prepared.Snapshot.Meal)
	require.Equal(t, int64(715), prepared.Snapshot.Quote.TotalMinor)
	return prepared
}

func prepareMealWire(
	t *testing.T,
	f paymentFixture,
	operation uuid.UUID,
	args db.OrderSubmission,
) orderWirePreparation {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, mealOrderAgentPath+"submit/prepare",
		map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args})
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared orderWirePreparation
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.Equal(t, "prepared", prepared.Status)
	require.NotEqual(t, uuid.Nil, prepared.Preconditions.ProposalID)
	require.NotEmpty(t, prepared.Description)
	require.LessOrEqual(t, len(prepared.Description), 4000)
	return prepared
}

func TestMealOrderHTTPNativeOnlyRecoveryWithoutLoom(t *testing.T) {
	t.Parallel()
	f, p, session, args := mealOrderAppFixture(t)
	p.loom.Close()
	path := mealsAppPath(args.EventID)
	op := uuid.New()
	prepared := prepareMealApp(t, f, session, op, args)
	body := orderAppSubmitBody(op, prepared)
	body["confirmed"] = false
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmed"] = true
	code, first, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(first))
	var submitted db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(first, &submitted))
	require.Equal(t, db.OrderKindMeal, submitted.Order.OrderKind)
	require.Nil(t, submitted.Selection)
	require.True(t, submitted.MealDraft.Cleared)
	f.H.stop(t)
	f.H.start(t)
	code, raw, headers := appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/operation-receipts/submit/"+op.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	var receipt db.OrderOperation
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.JSONEq(t, string(first), string(receipt.Result))
	cancelBody := paymentAppJSON(
		t,
		map[string]any{"operation_id": uuid.New(), "order_id": submitted.Order.ID, "expected_version": 1},
	)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/cancel", cancelBody, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var cancelled db.SubmittedOrder
	require.NoError(t, json.Unmarshal(raw, &cancelled))
	require.Equal(t, "cancelled", cancelled.State)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/submitted/"+submitted.Order.ID.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var current db.SubmittedOrder
	require.NoError(t, json.Unmarshal(raw, &current))
	require.Equal(t, cancelled, current)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw), "A repeated submission retains its original state after cancellation")
	var tasks, evidence int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT (SELECT count(*) FROM shuttle_tasks),(SELECT count(*) FROM shuttle_order_evidence)").
		Scan(&tasks, &evidence))
	require.Zero(t, tasks, "Native requests must not start proactive agent work")
	require.Zero(t, evidence, "Submission never confirms payment")
}

func TestMealOrderHTTPAgentAndNativeShareCanonicalPreparation(t *testing.T) {
	t.Parallel()
	f, _, session, args := mealOrderAppFixture(t)
	path := mealsAppPath(args.EventID)
	nativeOp, agentOp := uuid.New(), uuid.New()
	native := prepareMealApp(t, f, session, nativeOp, args)
	agent := prepareMealWire(t, f, agentOp, args)
	require.Equal(t, native.ID, agent.Preconditions.ProposalID)
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     agentOp,
		"idempotency_key":  agentOp,
		"arguments":        args,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": native.ID},
		},
	}
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, mealOrderAgentPath+"submit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var effect orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(raw, &effect))
	require.Equal(t, "succeeded", effect.Status)
	require.NotNil(t, effect.Result)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, orderAppSubmitBody(nativeOp, native)),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var alias db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(raw, &alias))
	require.Equal(t, *effect.Result, alias)
	for _, method := range []string{"submit/status", "operation"} {
		request := map[string]any{"protocol_version": 1, "operation_id": agentOp}
		if method == "operation" {
			request["arguments"] = map[string]any{"operation_id": nativeOp, "kind": "submit"}
		}
		code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, mealOrderAgentPath+method, request)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), alias.Order.ID.String())
	}
	code, raw = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		mealOrderAgentPath+"list",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "meal-list",
			"arguments":        map[string]any{"event_id": args.EventID},
		},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), alias.Order.ID.String())
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/submitted?limit=1", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.OrdersPage
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Orders, 1)
	require.Equal(t, alias.Order, page.Orders[0])
	code, raw = adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		mealOrderAgentPath+"get",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "other-owner",
			"arguments":        map[string]any{"event_id": args.EventID, "order_id": alias.Order.ID},
		},
	)
	require.Equal(
		t,
		http.StatusNotFound,
		code,
		string(raw),
		"Review authority is not permission to read another owner's order",
	)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Equal(t, 1, count)
}

func TestMealOrderHTTPRejectsStaleParametersAndCurrentAccessLoss(t *testing.T) {
	t.Parallel()
	f, _, session, args := mealOrderAppFixture(t)
	path := mealsAppPath(args.EventID)
	op := uuid.New()
	prepared := prepareMealApp(t, f, session, op, args)
	body := orderAppSubmitBody(op, prepared)
	for _, origin := range []string{"https://untrusted.invalid", ""} {
		code, raw, _ := appCall(t, f.H, http.MethodPost, path+"/submit", paymentAppJSON(t, body), session.Token, origin)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	body["expected_draft_version"] = args.ExpectedDraftVersion + 1
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	body["expected_draft_version"] = args.ExpectedDraftVersion
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	for _, target := range []string{"submit/prepare", "get"} {
		arguments := any(args)
		if target == "get" {
			arguments = map[string]any{"event_id": args.EventID, "order_id": uuid.New()}
		}
		code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, mealOrderAgentPath+target,
			map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": arguments})
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
}
