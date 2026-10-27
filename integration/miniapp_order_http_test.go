package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func saveOrderAppDraft(
	t *testing.T,
	f paymentFixture,
	session appSession,
	draft db.OrdersSelectionChange,
) db.OrderSubmission {
	t.Helper()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		ordersAppPath(draft.EventID)+"/selection",
		ordersAppBody(t, draft),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	saved := ordersAppSelection(t, raw)
	return db.OrderSubmission{
		EventID:                draft.EventID,
		ExpectedEventVersion:   draft.ExpectedEventVersion,
		ExpectedCatalogVersion: draft.ExpectedCatalogVersion,
		ExpectedDraftVersion:   saved.Version,
	}
}

func orderAppPrepareBody(operation uuid.UUID, args db.OrderSubmission) map[string]any {
	return map[string]any{
		"operation_id":             operation,
		"expected_event_version":   args.ExpectedEventVersion,
		"expected_catalog_version": args.ExpectedCatalogVersion,
		"expected_draft_version":   args.ExpectedDraftVersion,
	}
}

func prepareOrderApp(
	t *testing.T,
	f paymentFixture,
	session appSession,
	operation uuid.UUID,
	args db.OrderSubmission,
) db.OrderPreparation {
	t.Helper()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		ordersAppPath(args.EventID)+"/submit/prepare",
		paymentAppJSON(t, orderAppPrepareBody(operation, args)),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared db.OrderPreparation
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.NotEqual(t, uuid.Nil, prepared.ID)
	return prepared
}

func orderAppSubmitBody(operation uuid.UUID, prepared db.OrderPreparation) map[string]any {
	body := orderAppPrepareBody(operation, prepared.Arguments)
	body["proposal_id"], body["confirmed"] = prepared.ID, true
	return body
}

func TestMiniAppOrderHTTPBothHandoffsRestartAndNativeWithoutLoom(t *testing.T) {
	t.Parallel()
	f, p, session, draft := ordersAppFixture(t)
	args := saveOrderAppDraft(t, f, session, draft)
	operation := uuid.New()
	prepared := prepareOrderApp(t, f, session, operation, args)
	path := ordersAppPath(args.EventID)
	// Starting in the form and finishing through the agent shares the exact proposal.
	agent := prepareOrderWire(t, f, uuid.New(), args)
	require.Equal(t, prepared.ID, agent.Preconditions.ProposalID)
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": prepared.ID},
		},
	}
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var effect orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(raw, &effect))
	require.Equal(t, "succeeded", effect.Status)
	require.NotNil(t, effect.Result)
	code, raw, headers := appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/operation-receipts/submit/"+operation.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.Equal(t, "no-referrer", headers.Get("Referrer-Policy"))
	var receipt db.OrderOperation
	require.NoError(t, json.Unmarshal(raw, &receipt))
	var recovered db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(receipt.Result, &recovered))
	require.Equal(t, *effect.Result, recovered)
	f.H.stop(t)
	f.H.start(t)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/operation-receipts/submit/"+operation.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.NoError(t, json.Unmarshal(receipt.Result, &recovered))
	require.Equal(t, *effect.Result, recovered)
	// Starting with an agent preparation and completing in the form needs no Loom.
	draft.OperationID = uuid.New()
	draft.ExpectedDraftVersion = effect.Result.Selection.Version
	args = saveOrderAppDraft(t, f, session, draft)
	agent = prepareOrderWire(t, f, uuid.New(), args)
	prepared = prepareOrderApp(t, f, session, uuid.New(), args)
	require.Equal(t, agent.Preconditions.ProposalID, prepared.ID)
	p.loom.Close()
	nativeOp := uuid.New()
	nativeBody := paymentAppJSON(t, orderAppSubmitBody(nativeOp, prepared))
	code, first, _ := appCall(t, f.H, http.MethodPost, path+"/submit", nativeBody, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(first))
	code, retry, _ := appCall(t, f.H, http.MethodPost, path+"/submit", nativeBody, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(retry))
	require.JSONEq(t, string(first), string(retry))
	var second db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(first, &second))
	require.NotEqual(t, recovered.Order.ID, second.Order.ID)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/submitted?limit=1", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.OrdersPage
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Orders, 1)
	require.NotNil(t, page.Next)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/submitted?limit=1&after="+page.Next.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var last db.OrdersPage
	require.NoError(t, json.Unmarshal(raw, &last))
	require.Len(t, last.Orders, 1)
	require.Nil(t, last.Next)
	require.NotEqual(t, page.Orders[0].ID, last.Orders[0].ID)
	cancelOp := uuid.New()
	cancelBody := paymentAppJSON(
		t,
		map[string]any{"operation_id": cancelOp, "order_id": second.Order.ID, "expected_version": 1},
	)
	code, cancelled, _ := appCall(t, f.H, http.MethodPost, path+"/cancel", cancelBody, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(cancelled))
	var cancelledOrder db.SubmittedOrder
	require.NoError(t, json.Unmarshal(cancelled, &cancelledOrder))
	require.Equal(t, second.Order.ID, cancelledOrder.ID)
	require.Equal(t, "cancelled", cancelledOrder.State)
	require.Equal(t, second.Order.Version+1, cancelledOrder.Version)
	code, again, _ := appCall(t, f.H, http.MethodPost, path+"/cancel", cancelBody, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(again))
	require.JSONEq(t, string(cancelled), string(again))
	code, current, _ := appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/submitted/"+second.Order.ID.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(current))
	require.JSONEq(t, string(cancelled), string(current))
	var tasks int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Zero(t, tasks, "Native business requests cannot start proactive agent work")
}

func TestMiniAppOrderHTTPValidationOriginAndCurrentSession(t *testing.T) {
	t.Parallel()
	f, _, session, draft := ordersAppFixture(t)
	args := saveOrderAppDraft(t, f, session, draft)
	path := ordersAppPath(args.EventID)
	operation := uuid.New()
	body := orderAppPrepareBody(operation, args)
	for _, invalid := range []string{
		"null",
		"{}",
		paymentAppJSON(
			t,
			map[string]any{"operation_id": operation, "expected_event_version": 1, "expected_catalog_version": 1},
		),
		paymentAppJSON(t, body) + " {}",
	} {
		code, raw, _ := appCall(t, f.H, http.MethodPost, path+"/submit/prepare", invalid, session.Token, appTestOrigin)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	body["owner_id"] = uuid.New()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit/prepare",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	delete(body, "owner_id")
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/submit/prepare", paymentAppJSON(t, body), session.Token, "")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	prepared := prepareOrderApp(t, f, session, operation, args)
	submit := orderAppSubmitBody(operation, prepared)
	submit["confirmed"] = false
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, submit),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	submit["confirmed"] = true
	submit["total_minor"] = 1
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, submit),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	delete(submit, "total_minor")
	for _, suffix := range []string{
		"/submitted?limit=0",
		"/submitted?limit=21",
		"/submitted?after=bad",
		"/submitted/00000000-0000-0000-0000-000000000000",
		"/operation-receipts/other/" + operation.String(),
	} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path+suffix, "", session.Token, "")
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, submit),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	for _, suffix := range []string{
		"/submitted",
		"/operation-receipts/prepare/" + operation.String(),
		"/operation-receipts/submit/" + operation.String(),
	} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path+suffix, "", session.Token, "")
		require.Equal(t, http.StatusUnauthorized, code, string(raw))
	}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, submit),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}

func TestMiniAppOrderHTTPUnknownPhaseStaleBodyAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	f, _, session, draft := ordersAppFixture(t)
	args := saveOrderAppDraft(t, f, session, draft)
	operation := uuid.New()
	prepared := prepareOrderApp(t, f, session, operation, args)
	path := ordersAppPath(args.EventID)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/operation-receipts/submit/"+operation.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusNotFound, code, string(raw), "Preparation is not proof of submission")
	other := detailsAppSession(t, f.H, 11003, 119)
	for _, suffix := range []string{"/submitted", "/operation-receipts/prepare/" + operation.String()} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path+suffix, "", other.Token, "")
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	body := orderAppSubmitBody(operation, prepared)
	code, raw, _ = appCall(t, f.H, http.MethodPost, path+"/submit", paymentAppJSON(t, body), other.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code, string(raw))
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
	var result db.OrderSubmissionResult
	require.NoError(t, json.Unmarshal(raw, &result))
	body["proposal_id"] = uuid.New()
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/submit",
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	cancel := map[string]any{"operation_id": uuid.New(), "order_id": result.Order.ID, "expected_version": 2}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/cancel",
		paymentAppJSON(t, cancel),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, path+"/submitted/"+result.Order.ID.String(), "", other.Token, "")
	require.Equal(t, http.StatusNotFound, code, string(raw))
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	for _, suffix := range []string{
		"/submitted",
		"/submitted/" + result.Order.ID.String(),
		"/operation-receipts/submit/" + operation.String(),
	} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path+suffix, "", session.Token, "")
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
}
