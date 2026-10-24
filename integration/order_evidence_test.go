package integration_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func orderEvidenceFixture(t *testing.T) (paymentFixture, db.ProfileScope, db.SubmittedOrder) {
	t.Helper()
	f, scope, args, change, _, _ := orderCapacityFixture(t)
	change.ExpectedVersion = 2
	change.Document.CompleteCapacity.Resources[0].Ceiling = 8
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	return f, scope, additionalEvidenceOrder(t, f, scope, args.ExpectedDraftVersion)
}

func additionalEvidenceOrder(t *testing.T, f paymentFixture, scope db.ProfileScope, version int64) db.SubmittedOrder {
	t.Helper()
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	saved, err := f.H.store.SaveOwnOrdersSelection(
		t.Context(),
		scope,
		db.OrdersSelectionChange{
			OperationID:            uuid.New(),
			EventID:                f.Event.ID,
			ExpectedEventVersion:   f.Event.Version,
			ExpectedCatalogVersion: view.Version,
			ExpectedDraftVersion:   version,
			Selections: []orders.Selection{
				{OfferID: "lunch", ItemID: "soup", Quantity: 2},
				{OfferID: "lunch", ItemID: "tea", Quantity: 1},
			},
		},
	)
	require.NoError(t, err)
	args := db.OrderSubmission{
		EventID:                f.Event.ID,
		ExpectedEventVersion:   f.Event.Version,
		ExpectedCatalogVersion: view.Version,
		ExpectedDraftVersion:   saved.Version,
	}
	op := uuid.New()
	prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	result, err := f.H.store.SubmitOwnOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	return result.Order
}

func orderEvidenceArgs(
	t *testing.T,
	f paymentFixture,
	order db.SubmittedOrder,
	update int64,
	unique string,
) db.OrderEvidenceSubmission {
	t.Helper()
	file := paymentFile(t, f.H, f.OwnerID, update, unique, time.Now().UTC().Truncate(time.Second))
	return db.OrderEvidenceSubmission{
		EventID:              order.EventID,
		OrderID:              order.ID,
		ExpectedOrderVersion: order.Version,
		FileID:               file,
		AmountMinor:          order.Snapshot.Quote.TotalMinor,
	}
}

func reviewOrderEvidence(
	t *testing.T,
	f paymentFixture,
	evidence db.OrderPaymentEvidence,
	decision string,
) (uuid.UUID, db.OrderEvidenceReviewPreparation, db.OrderPaymentEvidence) {
	t.Helper()
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	op := uuid.New()
	args := db.OrderEvidenceReview{
		EventID:         evidence.EventID,
		EvidenceID:      evidence.ID,
		ExpectedVersion: 1,
		Decision:        decision,
		Reason:          "Synthetic checked proof",
	}
	prepared, err := f.H.store.PrepareOrderEvidenceReview(t.Context(), actor, op, args)
	require.NoError(t, err)
	result, err := f.H.store.ReviewOrderEvidence(t.Context(), actor, op, prepared.ID, args)
	require.NoError(t, err)
	return op, prepared, result
}

func orderAllocationUnits(t *testing.T, f paymentFixture, state string) int64 {
	t.Helper()
	var units int64
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT coalesce(sum(units),0)::bigint FROM shuttle_order_allocations WHERE state=$1", state).
			Scan(&units),
	)
	return units
}

func TestOrderEvidenceAdmissionConfirmationAndOriginalReceiptRecovery(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61101, "order-proof-confirm")
	op := uuid.New()
	admitted, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidencePending, admitted.State)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
	_, err = f.H.store.CancelOwnOrder(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderCancellation{EventID: order.EventID, OrderID: order.ID, ExpectedVersion: 1},
		db.OrderSourceMiniApp,
	)
	require.ErrorIs(t, err, db.ErrConflict, "Ordinary cancellation must not release paid or pending resources")
	reviewOp, prepared, confirmed := reviewOrderEvidence(t, f, admitted, db.OrderEvidenceConfirmed)
	require.Equal(t, db.OrderEvidenceConfirmed, confirmed.State)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err := reopened.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	require.Equal(t, admitted, replay, "Admission receipt retains its original pending state")
	current, err := reopened.OwnOrderEvidence(t.Context(), scope, order.EventID, admitted.ID)
	require.NoError(t, err)
	require.Equal(t, confirmed, current)
	decision, err := reopened.ReviewOrderEvidence(
		t.Context(),
		paymentScope(t, f.H, f.AmbassadorID, false),
		reviewOp,
		prepared.ID,
		prepared.Arguments,
	)
	require.NoError(t, err)
	require.Equal(t, confirmed, decision)
}

func TestOrderEvidenceRejectResubmitAndStaleRejectionCannotReleaseNewAttempt(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61102, "order-proof-reject")
	first, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	op, prepared, rejected := reviewOrderEvidence(t, f, first, db.OrderEvidenceRejected)
	require.Equal(t, db.OrderEvidenceRejected, rejected.State)
	var reason string
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT document->'review'->>'reason' FROM shuttle_order_evidence_notices" +
" WHERE evidence_id=$1 AND recipient_id=$2 AND kind='rejected'", first.ID, f.OwnerID).
			Scan(&reason),
	)
	require.Equal(t, "Synthetic checked proof", reason)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	second, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	_, err = f.H.store.ReviewOrderEvidence(
		t.Context(),
		paymentScope(t, f.H, f.AmbassadorID, false),
		op,
		prepared.ID,
		prepared.Arguments,
	)
	require.NoError(t, err, "Original rejection replay has no new effect")
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	_, err = f.H.store.PrepareOrderEvidenceReview(
		t.Context(),
		paymentScope(t, f.H, f.AmbassadorID, false),
		uuid.New(),
		prepared.Arguments,
	)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestOrderEvidenceLastPlaceConcurrentAdmissionAndExhaustionNotices(t *testing.T) {
	t.Parallel()
	f, scope, first := orderEvidenceFixture(t)
	selection, err := f.H.store.OwnOrdersSelection(t.Context(), scope, f.Event.ID)
	require.NoError(t, err)
	second := additionalEvidenceOrder(t, f, scope, selection.Version)
	args := []db.OrderEvidenceSubmission{
		orderEvidenceArgs(t, f, first, 61103, "race-first"),
		orderEvidenceArgs(t, f, second, 61104, "race-second"),
	}
	type outcome struct {
		evidence db.OrderPaymentEvidence
		err      error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, a := range args {
		wg.Go(func() {
			r, e := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), a)
			results <- outcome{r, e}
		})
	}
	wg.Wait()
	close(results)
	counts := map[string]int{}
	var exhausted uuid.UUID
	for result := range results {
		require.NoError(t, result.err)
		counts[result.evidence.State]++
		if result.evidence.State == db.OrderEvidenceExhausted {
			exhausted = result.evidence.ID
			require.Equal(t, "refund_attention_required", result.evidence.Reason)
		}
	}
	require.Equal(t, map[string]int{db.OrderEvidencePending: 1, db.OrderEvidenceExhausted: 1}, counts)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	var notices int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence_notices WHERE evidence_id=$1" +
" AND kind='exhausted' AND recipient_id IN ($2,$3)", exhausted, f.OwnerID, f.AmbassadorID).
			Scan(&notices),
	)
	require.Equal(t, 2, notices, "Customer and ambassador both retain refund-attention business notices")
}

func TestOrderEvidenceProvenanceBlocksPassAndOrderReuseInBothDirections(t *testing.T) {
	t.Parallel()
	for _, passFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "order_first", true: "pass_first"}[passFirst], func(t *testing.T) {
			t.Parallel()
			f, scope, order := orderEvidenceFixture(t)
			args := orderEvidenceArgs(t, f, order, 61105, "cross-proof")
			passArgs := payments.Submission{
				EventID:       f.Event.ID,
				PassID:        f.Pass.ID,
				FileID:        args.FileID,
				AmountMinor:   3000,
				Currency:      f.Pass.Currency,
				CurrencyScale: f.Pass.CurrencyScale,
				Purpose:       payments.Initial,
			}
			owner := paymentScope(t, f.H, f.OwnerID, false)
			if passFirst {
				_, err := f.H.store.SubmitPaymentEvidence(t.Context(), owner, "pass-cross-first", passArgs)
				require.NoError(t, err)
				_, err = f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
				require.ErrorIs(t, err, db.ErrConflict)
			} else {
				_, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
				require.NoError(t, err)
				_, err = f.H.store.SubmitPaymentEvidence(t.Context(), owner, "pass-cross-second", passArgs)
				require.ErrorIs(t, err, db.ErrConflict)
			}
		})
	}
}

func TestOrderEvidenceReceiptAndNoticeFailuresRollBackAllEffects(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"shuttle_order_evidence_notices", "shuttle_order_evidence_operations"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			f, scope, order := orderEvidenceFixture(t)
			args := orderEvidenceArgs(t, f, order, 61106, "rollback-proof")
			op := uuid.New()
			_, err := f.H.store.Pool().
				Exec(t.Context(), "CREATE FUNCTION fail_order_proof_write() RETURNS trigger LANGUAGE plpgsq" +
"l AS $$ BEGIN RAISE EXCEPTION 'synthetic proof write failure'; END $$; C" +
"REATE TRIGGER fail_order_proof_write BEFORE INSERT ON "+table+" FOR EACH ROW EXECUTE FUNCTION fail_order_proof_write()")
			require.NoError(t, err)
			_, err = f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
			require.ErrorContains(t, err, "synthetic proof write failure")
			var count int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence").Scan(&count),
			)
			require.Zero(t, count)
			require.Zero(t, orderAllocationUnits(t, f, "held"))
			_, err = f.H.store.Pool().
				Exec(t.Context(), "DROP TRIGGER fail_order_proof_write ON "+table+"; DROP FUNCTION fail_order_proof_write()")
			require.NoError(t, err)
			evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
			require.NoError(t, err)
			require.Equal(t, db.OrderEvidencePending, evidence.State)
		})
	}
}

func TestOrderEvidenceCurrentAuthorityAndCeilingsRemainRequired(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61107, "authority-proof")
	op := uuid.New()
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	doc := orders.CatalogDocument{
		Currency:         view.Currency,
		CurrencyScale:    view.CurrencyScale,
		Items:            view.Items,
		CompletePricing:  view.CompletePricing,
		CompleteCapacity: view.CompleteCapacity,
	}
	for _, offer := range view.Offers {
		doc.Offers = append(doc.Offers, offer.Offer)
	}
	doc.CompleteCapacity.Resources[0].Ceiling = 7
	change := db.OrdersCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      view.Version,
		Document:             doc,
	}
	code, _ := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		ordersWritePath+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": change},
	)
	require.Equal(t, http.StatusConflict, code, "A ceiling cannot drop below pending holds")
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	review := db.OrderEvidenceReview{
		EventID:         f.Event.ID,
		EvidenceID:      evidence.ID,
		ExpectedVersion: 1,
		Decision:        db.OrderEvidenceConfirmed,
		Reason:          "Synthetic authority review",
	}
	reviewOp := uuid.New()
	prepared, err := f.H.store.PrepareOrderEvidenceReview(t.Context(), actor, reviewOp, review)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	_, err = f.H.store.ReviewOrderEvidence(t.Context(), actor, reviewOp, prepared.ID, review)
	require.ErrorIs(t, err, payments.ErrDenied)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.Error(t, err, "Even a matching admission receipt must recheck current owner access")
}

func TestOrderEvidenceReviewFailureRollsBackDecisionAndHolds(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61108, "review-rollback-proof")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	review := db.OrderEvidenceReview{
		EventID:         f.Event.ID,
		EvidenceID:      evidence.ID,
		ExpectedVersion: 1,
		Decision:        db.OrderEvidenceConfirmed,
		Reason:          "Synthetic reviewed proof",
	}
	op := uuid.New()
	prepared, err := f.H.store.PrepareOrderEvidenceReview(t.Context(), actor, op, review)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_order_review_notice() RETURNS trigger LANGUAGE plpg" +
"sql AS $$ BEGIN RAISE EXCEPTION 'synthetic review notice failure'; END $" +
"$; CREATE TRIGGER fail_order_review_notice BEFORE INSERT ON shuttle_orde" +
"r_evidence_notices FOR EACH ROW EXECUTE FUNCTION fail_order_review_notic" +
"e()")
	require.NoError(t, err)
	_, err = f.H.store.ReviewOrderEvidence(t.Context(), actor, op, prepared.ID, review)
	require.ErrorContains(t, err, "synthetic review notice failure")
	current, err := f.H.store.OwnOrderEvidence(t.Context(), scope, f.Event.ID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, evidence, current)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER fail_order_review_notice ON shuttle_order_evidence_notices;" +
" DROP FUNCTION fail_order_review_notice()")
	require.NoError(t, err)
	result, err := f.H.store.ReviewOrderEvidence(t.Context(), actor, op, prepared.ID, review)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidenceConfirmed, result.State)
}

func TestOrderEvidenceUnresolvedAmountRetainsClaimWithoutHolds(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61109, "partial-proof")
	args.AmountMinor--
	result, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidenceNeedsDecision, result.State)
	require.Equal(t, "amount_requires_decision", result.Reason)
	require.Equal(t, args.FileID, result.FileID)
	require.Equal(t, args.OrderID, result.OrderID)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	_, err = f.H.store.PrepareOrderEvidenceReview(
		t.Context(),
		actor,
		uuid.New(),
		db.OrderEvidenceReview{
			EventID:         f.Event.ID,
			EvidenceID:      result.ID,
			ExpectedVersion: 1,
			Decision:        db.OrderEvidenceConfirmed,
			Reason:          "Cannot silently choose a partial payment policy",
		},
	)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestOrderEvidenceRevokedPayerCanStillBeRejectedByCurrentAuthority(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61110, "revoked-payer-proof")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.OwnOrderEvidence(t.Context(), scope, f.Event.ID, evidence.ID)
	require.Error(t, err)
	_, _, rejected := reviewOrderEvidence(t, f, evidence, db.OrderEvidenceRejected)
	require.Equal(t, db.OrderEvidenceRejected, rejected.State)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
}

func TestOrderEvidenceUsesOrdersModuleAndSixDigitCurrencyScale(t *testing.T) {
	t.Parallel()
	f, scope, _ := orderEvidenceFixture(t)
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	doc := orders.CatalogDocument{
		Currency:         view.Currency,
		CurrencyScale:    6,
		Items:            view.Items,
		CompletePricing:  view.CompletePricing,
		CompleteCapacity: view.CompleteCapacity,
	}
	for _, offer := range view.Offers {
		doc.Offers = append(doc.Offers, offer.Offer)
	}
	change := db.OrdersCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      view.Version,
		Document:             doc,
	}
	op := uuid.New()
	status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
	require.Equal(t, "succeeded", status)
	draft, err := f.H.store.OwnOrdersSelection(t.Context(), scope, f.Event.ID)
	require.NoError(t, err)
	order := additionalEvidenceOrder(t, f, scope, draft.Version)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	args := orderEvidenceArgs(t, f, order, 61111, "scale-six-proof")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	require.Equal(t, 6, evidence.CurrencyScale)
	_, _, confirmed := reviewOrderEvidence(t, f, evidence, db.OrderEvidenceConfirmed)
	require.Equal(t, db.OrderEvidenceConfirmed, confirmed.State)
	require.Equal(t, 6, confirmed.CurrencyScale)
	require.Equal(t, evidence.QuotedAmountMinor, confirmed.Review.AmountMinor)
}

func TestOrderEvidenceUnresolvedRequirementsAndDeadlinePreserveClaims(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"unknown", "late", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			var f paymentFixture
			var scope db.ProfileScope
			var order db.SubmittedOrder
			if kind == "unknown" {
				var args db.OrderSubmission
				f, scope, _, args = submissionFixture(t)
				op := uuid.New()
				prepared, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), scope, op, args, db.OrderSourceAgent)
				require.NoError(t, err)
				result, err := f.H.store.SubmitOwnOrder(t.Context(), scope, op, args, prepared.ID, db.OrderSourceAgent)
				require.NoError(t, err)
				order = result.Order
			} else {
				f, scope, order = orderEvidenceFixture(t)
			}
			if kind == "mixed" {
				_, view := readCatalog(t, f, f.Owner, f.Event.ID)
				doc := orders.CatalogDocument{
					Currency:         view.Currency,
					CurrencyScale:    view.CurrencyScale,
					Items:            view.Items,
					CompletePricing:  view.CompletePricing,
					CompleteCapacity: view.CompleteCapacity,
				}
				for _, offer := range view.Offers {
					doc.Offers = append(doc.Offers, offer.Offer)
				}
				doc.CompleteCapacity.Resources = append(
					doc.CompleteCapacity.Resources,
					orders.CapacityResource{
						ID:      "parking",
						Name:    orders.Label{EN: "Parking", RU: "Парковка"},
						Ceiling: 0,
					},
				)
				doc.CompleteCapacity.Bindings = append(
					doc.CompleteCapacity.Bindings,
					orders.CapacityBinding{OfferID: "lunch", ResourceID: "parking", Units: 1},
				)
				change := db.OrdersCatalogChange{
					EventID:              f.Event.ID,
					ExpectedEventVersion: f.Event.Version,
					ExpectedVersion:      view.Version,
					Document:             doc,
				}
				op := uuid.New()
				status, _ := commitCatalog(t, f, op, prepareCatalog(t, f, op, change))
				require.Equal(t, "succeeded", status)
				draft, err := f.H.store.OwnOrdersSelection(t.Context(), scope, f.Event.ID)
				require.NoError(t, err)
				order = additionalEvidenceOrder(t, f, scope, draft.Version)
			}
			args := orderEvidenceArgs(t, f, order, 61112, "unresolved-proof")
			if kind == "late" {
				// Fixture timestamp is deliberately beyond the frozen cutoff; no wall-clock wait.
				args.FileID = paymentFile(
					t,
					f.H,
					f.OwnerID,
					61113,
					"late-order-proof",
					order.Snapshot.SalesClosesAt.Add(time.Second).Truncate(time.Second),
				)
			}
			evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
			require.NoError(t, err)
			require.Equal(t, db.OrderEvidenceNeedsDecision, evidence.State)
			require.Equal(
				t,
				map[string]string{"unknown": "unknown_requirements", "late": "deadline_requires_decision", "mixed": "mixed_capacity_shortage"}[kind],
				evidence.Reason,
			)
			require.Equal(t, args.FileID, evidence.FileID)
			require.Zero(t, orderAllocationUnits(t, f, "held"))
		})
	}
}

func TestOrderEvidenceAdmissionInvalidatesEarlierCeilingReductionApproval(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	_, view := readCatalog(t, f, f.Owner, f.Event.ID)
	doc := orders.CatalogDocument{
		Currency:         view.Currency,
		CurrencyScale:    view.CurrencyScale,
		Items:            view.Items,
		CompletePricing:  view.CompletePricing,
		CompleteCapacity: view.CompleteCapacity,
	}
	for _, offer := range view.Offers {
		doc.Offers = append(doc.Offers, offer.Offer)
	}
	doc.CompleteCapacity.Resources[0].Ceiling = 7
	change := db.OrdersCatalogChange{
		EventID:              f.Event.ID,
		ExpectedEventVersion: f.Event.Version,
		ExpectedVersion:      view.Version,
		Document:             doc,
	}
	op := uuid.New()
	prepared := prepareCatalog(t, f, op, change)
	args := orderEvidenceArgs(t, f, order, 61114, "ceiling-race-proof")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	status, _ := commitCatalog(t, f, op, prepared)
	require.Equal(t, "rejected", status, "New holds invalidate previously prepared lower ceiling")
	_, _, confirmed := reviewOrderEvidence(t, f, evidence, db.OrderEvidenceConfirmed)
	require.Equal(t, db.OrderEvidenceConfirmed, confirmed.State)
	code, _ := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		ordersWritePath+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": uuid.New(), "arguments": change},
	)
	require.Equal(t, http.StatusConflict, code, "Confirmed occupancy still prevents the reduction")
}
