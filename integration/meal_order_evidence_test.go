package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func mealEvidenceFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.SubmittedOrder) {
	t.Helper()
	f, scope, args := mealSubmissionFixture(t)
	op := uuid.New()
	p, err := f.H.store.PrepareOwnMealOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnMealOrder(t.Context(), scope, op, args, p.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	return f, scope, result.Order
}

func TestMealEvidenceNativeAgentReviewAndRestart(t *testing.T) {
	t.Parallel()
	f, owner, order := mealEvidenceFixture(t)
	ctx := t.Context()
	deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	native := nativeOrderSession(t, f, owner, 11002)
	args := orderEvidenceArgs(t, f, order, 79101, "meal-shared-proof")
	op := uuid.New()
	admitted, err := f.H.store.SubmitMiniAppOrderEvidence(ctx, deployment, native, op, args)
	require.NoError(t, err)
	require.Equal(t, db.OrderKindMeal, admitted.OrderKind)
	require.Equal(t, db.OrderEvidencePending, admitted.State)
	require.Nil(t, admitted.Review)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	replay, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, op, args)
	require.NoError(t, err)
	require.Equal(t, admitted, replay)
	reviewer := paymentScope(t, f.H, f.AmbassadorID, false)
	review := db.OrderEvidenceReview{
		EventID:         order.EventID,
		EvidenceID:      admitted.ID,
		ExpectedVersion: 1,
		Decision:        db.OrderEvidenceConfirmed,
		Reason:          "Synthetic checked meal receipt",
	}
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		ctx,
		reviewer,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review, Language: "ru"},
	)
	require.NoError(t, err)
	require.Contains(t, w.Preparation.Description, "Состав питания")
	for _, name := range []string{
		"Суп",
		"Горячее",
		"Гарнир",
		"Салат",
		"Чай",
		"BYN",
		order.Snapshot.Quote.Timezone,
	} {
		require.Contains(t, w.Preparation.Description, name)
	}
	reviewerNative := nativePaymentSession(t, f, reviewer, 11003)
	result, err := f.H.store.CommitMiniAppOrderReviewWorkspace(
		ctx,
		reviewer,
		reviewerNative,
		uuid.New(),
		db.OrderReviewCommitChange{WorkspaceID: w.ID, Revision: w.Revision, Review: review},
		w.Preparation.ID,
	)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidenceConfirmed, result.Evidence.State)
	require.NotNil(t, result.Evidence.Review)
	require.Equal(t, admitted.AmountMinor, result.Evidence.Review.AmountMinor)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err = reopened.SubmitOwnOrderEvidence(ctx, owner, op, args)
	require.NoError(t, err)
	require.Equal(t, admitted, replay, "Original pending receipt survives human review and reconnect")
	current, err := reopened.OwnOrderEvidence(ctx, owner, order.EventID, admitted.ID)
	require.NoError(t, err)
	require.Equal(t, result.Evidence, current)
	detail, err := reopened.MiniAppOrderReviewDetail(ctx, reviewer, reviewerNative, order.EventID, admitted.ID)
	require.NoError(t, err)
	require.Equal(t, order, detail.Order)
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
	receipt, err := reopened.MiniAppOrderEvidenceSubmissionReceipt(ctx, deployment, native, order.EventID, op)
	require.NoError(t, err)
	require.Equal(t, admitted, receipt.Result)
}

func TestMealEvidenceOrdersToggleCannotGrantMealAccess(t *testing.T) {
	t.Parallel()
	f, owner, order := mealEvidenceFixture(t)
	ctx := t.Context()
	args := orderEvidenceArgs(t, f, order, 79102, "meal-disabled-proof")
	op := uuid.New()
	admitted, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, op, args)
	require.NoError(t, err)
	reviewer := paymentScope(t, f.H, f.AmbassadorID, false)
	review := db.OrderEvidenceReview{
		EventID:         order.EventID,
		EvidenceID:      admitted.ID,
		ExpectedVersion: 1,
		Decision:        db.OrderEvidenceConfirmed,
		Reason:          "Synthetic checked proof",
	}
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		ctx,
		reviewer,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	native := nativeOrderSession(t, f, owner, 11002)
	reviewerNative := nativePaymentSession(t, f, reviewer, 11003)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','orders'] WHERE id=$1", order.EventID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrderEvidence(ctx, owner, op, args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.OwnOrderEvidence(ctx, owner, order.EventID, admitted.ID)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceOperationByID(
		ctx,
		paymentScope(t, f.H, f.OwnerID, false),
		op,
		db.OrderEvidenceSubmissionReceipt,
	)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceFile(ctx, paymentScope(t, f.H, f.OwnerID, false), admitted.ID, false)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceFile(ctx, reviewer, admitted.ID, true)
	require.Error(t, err)
	_, err = f.H.store.MiniAppOrderEvidenceSubmissionReceipt(
		ctx,
		db.EventScope{BotID: owner.BotID, Organization: owner.Organization},
		native,
		order.EventID,
		op,
	)
	require.Error(t, err)
	_, err = f.H.store.MiniAppOrderReviewDetail(ctx, reviewer, reviewerNative, order.EventID, admitted.ID)
	require.Error(t, err)
	_, err = f.H.store.MiniAppOrderReviewFile(ctx, reviewer, reviewerNative, order.EventID, admitted.ID)
	require.Error(t, err)
	_, err = f.H.store.OrderReviewWorkspaceByID(ctx, reviewer, w.ID)
	require.Error(t, err)
	_, err = f.H.store.CommitMiniAppOrderReviewWorkspace(
		ctx,
		reviewer,
		reviewerNative,
		uuid.New(),
		db.OrderReviewCommitChange{WorkspaceID: w.ID, Revision: w.Revision, Review: review},
		w.Preparation.ID,
	)
	require.Error(t, err)
	for _, audit := range []bool{false, true} {
		scope := paymentScope(t, f.H, f.OwnerID, false)
		if audit {
			scope = reviewer
		}
		page, listErr := f.H.store.OrderEvidenceList(ctx, scope, order.EventID, uuid.Nil, uuid.Nil, 20, audit)
		require.NoError(t, listErr)
		require.Empty(t, page.Evidence)
	}
	var notice uuid.UUID
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT id FROM shuttle_order_evidence_notices WHERE evidence_id=$1 LIMIT 1", admitted.ID).
			Scan(&notice),
	)
	_, _, err = f.H.store.OrderNotificationRecipient(ctx, deploymentFor(owner), notice)
	require.Error(t, err)
	var state string
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT state FROM shuttle_order_evidence WHERE id=$1", admitted.ID).
			Scan(&state),
	)
	require.Equal(t, db.OrderEvidencePending, state)
}

func deploymentFor(scope db.ProfileScope) db.EventScope {
	return db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
}

func TestMealEvidenceAmbiguousProofRequiresDecision(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"amount", "late"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, owner, order := mealEvidenceFixture(t)
			args := orderEvidenceArgs(t, f, order, 79103, "meal-ambiguous-proof")
			if kind == "amount" {
				args.AmountMinor--
			} else {
				args.FileID = paymentFile(
					t,
					f.H,
					f.OwnerID,
					79104,
					"meal-late-proof",
					order.Snapshot.SalesClosesAt.Add(time.Second).Truncate(time.Second),
				)
			}
			proof, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), owner, uuid.New(), args)
			require.NoError(t, err)
			require.Equal(t, db.OrderEvidenceNeedsDecision, proof.State)
			reviewer := paymentScope(t, f.H, f.AmbassadorID, false)
			_, err = f.H.store.PrepareOrderEvidenceReview(
				t.Context(),
				reviewer,
				uuid.New(),
				db.OrderEvidenceReview{
					EventID:         order.EventID,
					EvidenceID:      proof.ID,
					ExpectedVersion: 1,
					Decision:        db.OrderEvidenceConfirmed,
					Reason:          "Cannot automatically approve ambiguous evidence",
				},
			)
			require.ErrorIs(t, err, db.ErrConflict)
			_, _, rejected := reviewOrderEvidence(t, f, proof, db.OrderEvidenceRejected)
			require.Equal(t, db.OrderEvidenceRejected, rejected.State)
			require.Zero(t, orderAllocationUnits(t, f, "held"))
			require.Zero(t, orderAllocationUnits(t, f, "occupied"))
		})
	}
}

func TestMealEvidenceConcurrentNativeAndAgentCreateOneClaim(t *testing.T) {
	t.Parallel()
	f, owner, order := mealEvidenceFixture(t)
	ctx := t.Context()
	native := nativeOrderSession(t, f, owner, 11002)
	args := orderEvidenceArgs(t, f, order, 79105, "meal-concurrent-proof")
	op := uuid.New()
	var wg sync.WaitGroup
	results := make([]db.OrderPaymentEvidence, 2)
	errs := make([]error, 2)
	wg.Go(func() { results[0], errs[0] = f.H.store.SubmitOwnOrderEvidence(ctx, owner, op, args) })
	wg.Go(func() {
		results[1], errs[1] = f.H.store.SubmitMiniAppOrderEvidence(ctx, deploymentFor(owner), native, op, args)
	})
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0], results[1])
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_order_evidence").Scan(&count))
	require.Equal(t, 1, count)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
}

func TestMealEvidenceRetainsOriginalReceipt(t *testing.T) {
	t.Parallel()
	f, owner, order := mealEvidenceFixture(t)
	ctx := t.Context()
	args := orderEvidenceArgs(t, f, order, 79106, "meal-rollback-proof")
	proof, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, uuid.New(), args)
	require.NoError(t, err)
	current, err := f.H.store.OwnOrderEvidence(ctx, owner, order.EventID, proof.ID)
	require.NoError(t, err)
	require.Equal(t, proof, current)
}

func TestMealEvidenceSharedAPIReadsAndDiscovery(t *testing.T) {
	t.Parallel()
	f, owner, order := mealEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 79107, "meal-api-proof")
	op := uuid.New()
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), owner, op, args)
	require.NoError(t, err)
	for _, tc := range []struct {
		token, method string
		args          any
	}{
		{f.Owner, "evidence", map[string]any{"event_id": order.EventID, "evidence_id": evidence.ID}},
		{f.Ambassador, "evidence_for_review", map[string]any{"event_id": order.EventID, "evidence_id": evidence.ID}},
	} {
		code, raw := orderEvidenceRead(t, f, tc.token, tc.method, tc.args)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out orderWireResult[db.OrderPaymentEvidence]
		require.NoError(t, json.Unmarshal(raw, &out))
		require.Equal(t, evidence, *out.Result)
	}
	code, raw := orderEvidenceRead(t, f, f.Ambassador, "discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "review_commit")
	code, raw = orderEvidenceRead(t, f, f.Owner, "list", map[string]any{"event_id": order.EventID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), evidence.ID.String())
}

func largeMealEvidence(t *testing.T, name string) (paymentFixture, db.OrderPaymentEvidence, int) {
	t.Helper()
	localized := name == "large_localized_labels"
	escaped := name == "escaped_labels"
	ctx := t.Context()
	f, owner, change := mealFixture(t)
	if escaped {
		f.Event.Document.Name = strings.Repeat("😀", 256)
		code, raw := adminRequest(t, f.H, f.Admin, http.MethodPut,
			"/internal/admin/events/"+f.Event.ID.String(), map[string]any{
				"operation_id": uuid.NewString(), "expected_version": f.Event.Version,
				"document": f.Event.Document,
			})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.NoError(t, json.Unmarshal(raw, &f.Event))
		change.ExpectedEventVersion = f.Event.Version
	}
	catalog, err := f.H.store.OwnMealCatalog(ctx, owner, f.Event.ID, 0)
	require.NoError(t, err)
	deadline := catalog.ObservedAt.Add(2 * time.Hour).Truncate(time.Microsecond)
	catalog.Document.PaymentDeadline = &deadline
	day := &catalog.Document.Days[0]
	day.Lunch = nil
	day.Dinner.Items = []orders.MealItem{}
	ids := []string{}
	count := orders.MaxItems
	if localized {
		count = 80
	}
	for i := range count {
		id := fmt.Sprintf("dish_%03d", i)
		ids = append(ids, id)
		name := orders.Label{
			EN: fmt.Sprintf("Dish %03d ", i) + strings.Repeat("A", 64),
			RU: fmt.Sprintf("Блюдо %03d ", i) + strings.Repeat("я", 30),
		}
		if localized {
			name = orders.Label{
				EN: id,
				RU: fmt.Sprintf("Блюдо %03d ", i) + strings.Repeat("界", orders.MaxLabelRunes-10),
			}
		}
		if escaped {
			name = orders.Label{EN: id, RU: strings.Repeat("\\", orders.MaxLabelRunes)}
		}
		day.Dinner.Items = append(day.Dinner.Items, orders.MealItem{ID: id, Name: name, PriceMinor: 1})
	}

	published := publishMealCatalog(t, f, catalog.Version, catalog.Document)
	change.ExpectedCatalogVersion = published.Version
	change.Choices = []orders.MealDayChoice{{DayID: day.ID, DinnerIDs: ids}}
	draft, err := f.H.store.SaveOwnMealDraft(ctx, owner, change)
	require.NoError(t, err)
	args := db.OrderSubmission{
		EventID:                f.Event.ID,
		ExpectedEventVersion:   f.Event.Version,
		ExpectedCatalogVersion: published.Version,
		ExpectedDraftVersion:   draft.Version,
	}
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnMealOrderSubmission(ctx, owner, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	submitted, err := f.H.store.SubmitOwnMealOrder(ctx, owner, op, args, prep.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	proof, err := f.H.store.SubmitOwnOrderEvidence(
		ctx,
		owner,
		uuid.New(),
		orderEvidenceArgs(t, f, submitted.Order, 79108, "meal-large-review"),
	)
	require.NoError(t, err)
	return f, proof, count
}

func TestMealEvidenceLargeReviewKeepsFullConsentOutsideLoomSummary(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"balanced_labels", "large_localized_labels", "escaped_labels"} {
		escaped := name == "escaped_labels"
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, proof, count := largeMealEvidence(t, name)
			ctx := t.Context()
			reviewer := paymentScope(t, f.H, f.AmbassadorID, false)
			review := db.OrderEvidenceReview{
				EventID:         f.Event.ID,
				EvidenceID:      proof.ID,
				ExpectedVersion: 1,
				Decision:        db.OrderEvidenceConfirmed,
				Reason:          "Check complete large meal composition",
			}
			if escaped {
				review.Reason = strings.Repeat("😀", 512)
			}
			w, err := f.H.store.PrepareOrderReviewWorkspace(
				ctx,
				reviewer,
				uuid.New(),
				db.OrderReviewDraftChange{Review: review, Language: "ru"},
			)
			require.NoError(t, err)
			saved, err := f.H.store.CurrentOrderReviewWorkspace(ctx, reviewer, f.Event.ID, proof.ID)
			require.NoError(t, err)
			require.Equal(t, w, saved)
			for _, tc := range []struct {
				method string
				args   any
			}{
				{"review_workspace", map[string]any{"event_id": f.Event.ID, "evidence_id": proof.ID}},
			} {
				code, raw := orderEvidenceRead(t, f, f.Ambassador, tc.method, tc.args)
				require.Equal(t, http.StatusOK, code, string(raw))
				require.LessOrEqual(t, len(raw), 64<<10)
			}
			require.Greater(t, len(w.Preparation.Description), 4000)
			commit := db.OrderReviewCommitChange{WorkspaceID: w.ID, Revision: w.Revision, Review: review}
			call := uuid.New()
			code, raw := adminRequest(
				t,
				f.H,
				f.Ambassador,
				http.MethodPost,
				orderEvidenceAPI+"review_commit/prepare",
				map[string]any{"protocol_version": 1, "operation_id": call, "arguments": commit},
			)
			require.Equal(t, http.StatusOK, code, string(raw))
			var wire struct {
				Description string `json:"description"`
			}
			require.NoError(t, json.Unmarshal(raw, &wire))
			require.NotEmpty(t, wire.Description)
			require.LessOrEqual(
				t,
				len(wire.Description),
				4000,
				"Loom preparation description has a strict4000-byte contract",
			)
			replayed, err := f.H.store.PrepareOrderReviewCommit(ctx, reviewer, call, commit)
			require.NoError(t, err)
			require.Equal(t, w.Preparation.Description, replayed.Description)
			code, raw = orderEvidenceRead(t, f, f.Ambassador, "review_operation", map[string]any{"operation_id": call})
			require.Equal(t, http.StatusOK, code, string(raw))
			require.LessOrEqual(t, len(raw), 64<<10)
			receipt, err := f.H.store.OrderReviewOperationByID(ctx, reviewer, call)
			require.NoError(t, err)
			require.Equal(
				t,
				w.Preparation.Description,
				receipt.Preparation.Description,
				"Bot approval retains the complete immutable presentation",
			)
			if escaped {
				require.Contains(t, receipt.Preparation.Description, strings.Repeat("\\\\", orders.MaxLabelRunes))
			} else {
				require.Contains(t, receipt.Preparation.Description, fmt.Sprintf("Блюдо %03d", count-1))
			}
			require.Contains(t, receipt.Preparation.Description, fmt.Sprintf("%d.%02d BYN", count/100, count%100))
			result, err := f.H.store.CommitOrderReviewWorkspace(ctx, reviewer, call, commit, replayed.ID)
			require.NoError(t, err)
			require.Equal(t, db.OrderEvidenceConfirmed, result.Evidence.State)
			history, err := f.H.store.OrderReviewOperationByID(ctx, reviewer, call)
			require.NoError(t, err)
			require.True(t, history.Completed)
			require.Equal(t, w.Preparation.Description, history.Preparation.Description)
		})
	}
}

func TestMealEvidencePreservesServiceHistoryAndReplay(t *testing.T) {
	t.Parallel()
	f, owner, order := orderEvidenceFixture(t)
	ctx := t.Context()
	args := orderEvidenceArgs(t, f, order, 79109, "service-meal-migration")
	op := uuid.New()
	proof, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, op, args)
	require.NoError(t, err)
	before := orderKindFingerprint(t, f)
	replay, err := f.H.store.SubmitOwnOrderEvidence(ctx, owner, op, args)
	require.NoError(t, err)
	require.Equal(t, proof, replay)
	require.Equal(t, before, orderKindFingerprint(t, f))
}
