package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderAgentPath = "/internal/agent/orders/"

type orderWirePreparation struct {
	Status        string             `json:"status"`
	Arguments     db.OrderSubmission `json:"arguments"`
	Description   string             `json:"description"`
	Preconditions struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
	ExpiresAt time.Time `json:"expires_at"`
}

type orderWireResult[T any] struct {
	Status      string `json:"status"`
	OperationID string `json:"operation_id"`
	Result      *T     `json:"result"`
}

func prepareOrderWire(
	t *testing.T,
	f paymentFixture,
	operation uuid.UUID,
	args db.OrderSubmission,
) orderWirePreparation {
	t.Helper()
	code, raw := adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		orderAgentPath+"submit/prepare",
		map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var p orderWirePreparation
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, "prepared", p.Status)
	require.NotEqual(t, uuid.Nil, p.Preconditions.ProposalID)
	return p
}

func TestOrderAgentExactApprovalAndRecoveryUseMatchingResultShape(t *testing.T) {
	t.Parallel()
	f, _, _, args := submissionFixture(t)
	operation := uuid.New()
	p := prepareOrderWire(t, f, operation, args)
	read := func(method string, body any) []byte {
		t.Helper()
		code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+method, body)
		require.Equal(t, http.StatusOK, code, string(raw))
		return raw
	}
	statusBody := map[string]any{"protocol_version": 1, "operation_id": operation}
	var status orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(read("submit/status", statusBody), &status))
	require.Equal(t, "not_found", status.Status)
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": p.Preconditions.ProposalID},
		},
	}
	var first orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(read("submit", body), &first))
	require.Equal(t, "succeeded", first.Status)
	require.NotNil(t, first.Result)
	require.NoError(t, json.Unmarshal(read("submit/status", statusBody), &status))
	require.Equal(t, first, status, "Recovery must return the same result shape, not a nested receipt")
	var retry orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(read("submit", body), &retry))
	require.Equal(t, first, retry)
	cancelOp := uuid.New()
	cancelBody := map[string]any{
		"protocol_version": 1,
		"operation_id":     cancelOp,
		"idempotency_key":  cancelOp,
		"arguments": db.OrderCancellation{
			EventID:         args.EventID,
			OrderID:         first.Result.Order.ID,
			ExpectedVersion: 1,
		},
		"confirmation": map[string]any{"mode": "none"},
	}
	var cancelled orderWireResult[db.SubmittedOrder]
	require.NoError(t, json.Unmarshal(read("cancel", cancelBody), &cancelled))
	require.Equal(t, "succeeded", cancelled.Status)
	require.NotNil(t, cancelled.Result)
	require.Equal(t, "cancelled", cancelled.Result.State)
	var cancelStatus orderWireResult[db.SubmittedOrder]
	require.NoError(
		t,
		json.Unmarshal(
			read("cancel/status", map[string]any{"protocol_version": 1, "operation_id": cancelOp}),
			&cancelStatus,
		),
	)
	require.Equal(t, cancelled, cancelStatus)
	require.NoError(t, json.Unmarshal(read("submit/status", statusBody), &status))
	require.Equal(t, first, status, "Later cancellation cannot rewrite original submission status")
	var current orderWireResult[db.SubmittedOrder]
	require.NoError(
		t,
		json.Unmarshal(
			read(
				"get",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     "read-current",
					"arguments":        map[string]any{"event_id": args.EventID, "order_id": first.Result.Order.ID},
				},
			),
			&current,
		),
	)
	require.Equal(t, cancelled.Result, current.Result)
	var page orderWireResult[db.OrdersPage]
	require.NoError(
		t,
		json.Unmarshal(
			read(
				"list",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     "read-list",
					"arguments":        map[string]any{"event_id": args.EventID},
				},
			),
			&page,
		),
	)
	require.NotNil(t, page.Result)
	require.Equal(t, []db.SubmittedOrder{*cancelled.Result}, page.Result.Orders)
	var receipt orderWireResult[db.OrderOperation]
	require.NoError(
		t,
		json.Unmarshal(
			read(
				"operation",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     "lookup",
					"arguments":        map[string]any{"operation_id": operation, "kind": "prepare"},
				},
			),
			&receipt,
		),
	)
	require.NotNil(t, receipt.Result)
	var recovered db.OrderPreparation
	require.NoError(t, json.Unmarshal(receipt.Result.Result, &recovered))
	require.Equal(t, p.Preconditions.ProposalID, recovered.ID)
}

func TestOrderAgentRejectsMissingApprovalForeignOwnerAndChangedBody(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	operation := uuid.New()
	p := prepareOrderWire(t, f, operation, args)
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
	}
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	body["confirmation"] = map[string]any{"mode": "none"}
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	body["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": p.Preconditions.ProposalID},
	}
	body["idempotency_key"] = uuid.New()
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["idempotency_key"] = operation
	body["arguments"] = map[string]any{
		"event_id":                 args.EventID,
		"expected_event_version":   args.ExpectedEventVersion,
		"expected_catalog_version": args.ExpectedCatalogVersion,
		"expected_draft_version":   args.ExpectedDraftVersion,
		"owner_id":                 f.AmbassadorID,
	}
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	body["arguments"] = args
	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var rejected orderWireResult[db.OrderSubmissionResult]
	require.NoError(t, json.Unmarshal(raw, &rejected))
	require.Equal(t, "rejected", rejected.Status)
	require.Nil(t, rejected.Result)
	code, raw = adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		orderAgentPath+"submit/status",
		map[string]any{"protocol_version": 1, "operation_id": operation},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rejected))
	require.Equal(t, "not_found", rejected.Status)
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	changed := args
	changed.ExpectedDraftVersion++
	body["arguments"] = changed
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"submit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rejected))
	require.Equal(t, "rejected", rejected.Status)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1", args.EventID)
	require.NoError(t, err)
	code, raw = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		orderAgentPath+"submit/status",
		map[string]any{"protocol_version": 1, "operation_id": operation},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rejected))
	require.Equal(t, "not_found", rejected.Status, "Revoked entitlement must hide retained receipt")
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	code, _ = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		orderAgentPath+"list",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "read",
			"arguments":        map[string]any{"event_id": args.EventID},
		},
	)
	require.Equal(t, http.StatusForbidden, code)
}

func TestOrderAgentSummaryLocaleAndManifestDeclareRecoveryContract(t *testing.T) {
	t.Parallel()
	f, scope, _, args := submissionFixture(t)
	profile, err := f.H.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	for _, language := range []string{"ru", "en"} {
		profile, err = f.H.store.MutateProfile(
			t.Context(),
			scope,
			db.ProfileMutation{
				OperationID:     uuid.New(),
				ExpectedVersion: profile.Version,
				Kind:            "set_language",
				BotLanguage:     language,
			},
		)
		require.NoError(t, err)
		p := prepareOrderWire(t, f, uuid.New(), args)
		require.LessOrEqual(t, len(p.Description), 4000, "Loom preparation byte bound")
		require.Contains(t, p.Description, "25.68 BYN")
		require.Contains(t, p.Description, "Europe/Minsk")
		if language == "ru" {
			require.Contains(t, p.Description, "Оформить заказ?")
		} else {
			require.Contains(t, p.Description, "Submit order?")
		}
	}
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var manifest struct {
		Revision string `json:"revision"`
		Modules  []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID           string `json:"id"`
				Effect       string `json:"effect"`
				Idempotency  bool   `json:"idempotency"`
				StatusPath   string `json:"status_path"`
				Dedup        int    `json:"deduplication_window_seconds"`
				Confirmation struct {
					Mode        string `json:"mode"`
					PreparePath string `json:"prepare_path"`
				} `json:"confirmation"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	// The global revision covers every module; assert the order contract below.
	require.NotEmpty(t, manifest.Revision)
	found := map[string]bool{}
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_orders" {
			continue
		}
		for _, method := range module.Methods {
			found[method.ID] = true
			if method.ID != "submit" && method.ID != "cancel" {
				continue
			}
			require.Equal(t, "write", method.Effect)
			require.True(t, method.Idempotency)
			require.Positive(t, method.Dedup)
			require.Equal(t, orderAgentPath+method.ID+"/status", method.StatusPath)
			if method.ID == "submit" {
				require.Equal(t, "exact_parameters", method.Confirmation.Mode)
				require.Equal(t, orderAgentPath+"submit/prepare", method.Confirmation.PreparePath)
			} else {
				require.Equal(t, "none", method.Confirmation.Mode)
			}
		}
	}
	for _, method := range []string{"list", "get", "submit", "cancel", "operation"} {
		require.True(t, found[method], method)
	}
	require.NotContains(t, string(raw), "no submitted orders", "Manifest must describe the current orders surface")
}

func TestOrderAgentLargeSnapshotsUseByteBoundedPagesWithoutSkipping(t *testing.T) {
	t.Parallel()
	f, scope, draft, _ := submissionFixture(t)
	ctx := t.Context()
	doc := catalogDocument()
	doc.Items = []orders.Item{}
	doc.Offers[0].ItemIDs = []string{}
	doc.CompletePricing = &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{}}
	draft.Selections = []orders.Selection{}
	for i := range 40 {
		id := fmt.Sprintf("item_%02d", i)
		doc.Items = append(
			doc.Items,
			orders.Item{
				ID:             id,
				Name:           orders.Label{EN: strings.Repeat("A", 128), RU: strings.Repeat("Ж", 128)},
				BasePriceMinor: 1,
			},
		)
		doc.Offers[0].ItemIDs = append(doc.Offers[0].ItemIDs, id)
		draft.Selections = append(draft.Selections, orders.Selection{OfferID: "lunch", ItemID: id, Quantity: 1})
	}
	change := db.OrdersCatalogChange{
		EventID:              draft.EventID,
		ExpectedEventVersion: draft.ExpectedEventVersion,
		ExpectedVersion:      1,
		Document:             doc,
	}
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	draft.ExpectedCatalogVersion = 2
	nextDraftVersion := int64(1)
	expected := map[uuid.UUID]db.SubmittedOrder{}
	for range 21 {
		draft.OperationID = uuid.New()
		draft.ExpectedDraftVersion = nextDraftVersion
		saved, err := f.H.store.SaveOwnOrdersSelection(ctx, scope, draft)
		require.NoError(t, err)
		result := submitSavedOrder(
			t,
			f,
			scope,
			db.OrderSubmission{
				EventID:                draft.EventID,
				ExpectedEventVersion:   draft.ExpectedEventVersion,
				ExpectedCatalogVersion: 2,
				ExpectedDraftVersion:   saved.Version,
			},
		)
		expected[result.Order.ID] = result.Order
		nextDraftVersion = result.Selection.Version
	}
	full, err := f.H.store.ListOwnOrders(ctx, scope, draft.EventID, uuid.Nil, 20)
	require.NoError(t, err)
	rawFull, err := json.Marshal(full)
	require.NoError(t, err)
	require.Greater(t, len(rawFull), 65536, "Fixture must reproduce the original full-page budget failure")
	after := uuid.Nil
	seen := map[uuid.UUID]bool{}
	pages := 0
	for {
		request := map[string]any{
			"protocol_version": 1,
			"operation_id":     "bounded-list",
			"arguments":        map[string]any{"event_id": draft.EventID, "after": after},
		}
		code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, orderAgentPath+"list", request)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Less(t, len(raw), 65536, "Entire wire response must fit Loom's advertised limit")
		var page orderWireResult[db.OrdersPage]
		require.NoError(t, json.Unmarshal(raw, &page))
		require.Equal(t, "succeeded", page.Status)
		require.NotNil(t, page.Result)
		require.NotEmpty(t, page.Result.Orders)
		require.Less(t, len(page.Result.Orders), 20, "The byte bound must trigger before the count limit")
		for _, got := range page.Result.Orders {
			require.False(t, seen[got.ID], "Cursor must not duplicate a retained order")
			require.Equal(t, expected[got.ID], got, "Pagination must retain the complete immutable snapshot")
			seen[got.ID] = true
		}
		pages++
		require.LessOrEqual(t, pages, 21, "Cursor must advance")
		if page.Result.Next == nil {
			break
		}
		require.Equal(t, page.Result.Orders[len(page.Result.Orders)-1].ID, *page.Result.Next)
		after = *page.Result.Next
	}
	require.Len(t, seen, 21, "Byte-bounded pagination must not omit later orders")
	require.Greater(t, pages, 1)
}
