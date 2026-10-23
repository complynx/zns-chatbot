package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMealInspectorKeepsNativeOperationThroughAgentCommitAndReopen(t *testing.T) {
	t.Parallel()
	f, _, session, args := mealOrderAppFixture(t)
	require.Contains(t, f.Event.Document.Modules, "meals")
	require.NotContains(t, f.Event.Document.Modules, "orders")
	op := uuid.New()
	prepared := prepareMealApp(t, f, session, op, args)
	inspectBody := map[string]any{
		"protocol_version": 1, "operation_id": op, "module": "zinusya_meals", "method": "submit",
	}
	var receiptsBefore int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_order_operations").Scan(&receiptsBefore))
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, "/internal/agent/operations/inspect", inspectBody)
	require.Equal(t, http.StatusOK, code, string(raw))
	var inspected struct {
		inspectedKnowledgeOperation

		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(raw, &inspected))
	require.Equal(t, "prepared", inspected.Status)
	require.Equal(t, op.String(), inspected.OperationID)
	require.Equal(t, prepared.ID, inspected.Preconditions.ProposalID)
	require.Equal(t, prepared.Description, inspected.Description)
	require.True(t, prepared.ExpiresAt.Equal(inspected.ExpiresAt))
	var recoveredArgs db.OrderSubmission
	require.NoError(t, json.Unmarshal(inspected.Arguments, &recoveredArgs))
	require.Equal(t, args, recoveredArgs)
	scope := ownerProfileScope(t, f.H, f.OwnerID)
	require.Equal(t, scope.Subject, inspected.Binding.Subject)
	require.Equal(t, scope.Organization, inspected.Binding.OrgID)
	require.Equal(t, "actor", inspected.Binding.Actor)

	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, "/internal/agent/operations/inspect", inspectBody)
	require.Contains(t, []int{http.StatusNotFound, http.StatusForbidden}, code, string(raw))
	inspectBody["module"] = "zinusya_orders"
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, "/internal/agent/operations/inspect", inspectBody)
	require.Contains(t, []int{http.StatusNotFound, http.StatusForbidden}, code, string(raw))
	mealOrderCounts(t, f, 0, 1)
	var receiptsAfter int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_order_operations").Scan(&receiptsAfter))
	require.Equal(t, receiptsBefore, receiptsAfter, "Inspection must not create a preparation or operation")

	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, mealOrderAgentPath+"submit", map[string]any{
		"protocol_version": 1, "operation_id": op, "idempotency_key": op, "arguments": recoveredArgs,
		"confirmation": map[string]any{
			"mode": "exact_parameters", "preconditions": map[string]any{"proposal_id": prepared.ID},
		},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var submitted orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(raw, &submitted))
	require.NotNil(t, submitted.Result)
	require.Equal(t, db.OrderKindMeal, submitted.Result.Order.OrderKind)
	require.NotNil(t, submitted.Result.MealDraft)
	require.True(t, submitted.Result.MealDraft.Cleared)
	require.Equal(t, args.ExpectedDraftVersion+1, submitted.Result.MealDraft.Version)
	require.Equal(t, prepared.Snapshot.Meal, submitted.Result.Order.Snapshot.Meal)

	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	contract := db.OrderPreparationContract{
		Issuer: f.H.peers.iam.URL, Actor: inspected.Binding.Actor, Revision: inspected.ContractRevision,
	}
	completed, err := reopened.InspectMealOrderSubmissionOperation(t.Context(), scope, op, contract)
	require.NoError(t, err)
	require.Equal(t, prepared, completed.Preparation)
	require.Equal(t, submitted.Result, completed.Result)
	replay, err := reopened.SubmitOwnMealOrder(t.Context(), scope, op, recoveredArgs, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, *submitted.Result, replay)
	mealOrderCounts(t, f, 1, 1)
}
