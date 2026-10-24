package integration_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func nativeOrderReviewWriteFixture(t *testing.T) (paymentFixture, db.PaymentScope, db.OrderEvidenceReview, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f, owner, draft := selectionFixtureWithPeers(t, p)
	code, view := readCatalog(t, f, f.Owner, f.Event.ID)
	require.Equal(t, http.StatusOK, code)
	doc := orders.CatalogDocument{
		Currency:        view.Currency,
		CurrencyScale:   view.CurrencyScale,
		Items:           view.Items,
		CompletePricing: view.CompletePricing,
		CompleteCapacity: &orders.CompleteCapacity{
			Version:   1,
			Resources: []orders.CapacityResource{{ID: "seat", Name: orders.Label{EN: "Seat", RU: "Место"}, Ceiling: 1}},
			Bindings:  []orders.CapacityBinding{{OfferID: "lunch", ResourceID: "seat", Units: 1}},
		},
	}
	for _, offer := range view.Offers {
		doc.Offers = append(doc.Offers, offer.Offer)
	}
	op := uuid.New()
	status, _ := commitCatalog(
		t,
		f,
		op,
		prepareCatalog(
			t,
			f,
			op,
			db.OrdersCatalogChange{
				EventID:              f.Event.ID,
				ExpectedEventVersion: f.Event.Version,
				ExpectedVersion:      1,
				Document:             doc,
			},
		),
	)
	require.Equal(t, "succeeded", status)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, f.H.service.Stop(ctx))
	f.H.service = nil
	draft.ExpectedCatalogVersion = 2
	saved, err := f.H.store.SaveOwnOrdersSelection(t.Context(), owner, draft)
	require.NoError(t, err)
	args := db.OrderSubmission{
		EventID:                f.Event.ID,
		ExpectedEventVersion:   f.Event.Version,
		ExpectedCatalogVersion: 2,
		ExpectedDraftVersion:   saved.Version,
	}
	op = uuid.New()
	prep, err := f.H.store.PrepareOwnOrderSubmission(t.Context(), owner, op, args, db.OrderSourceAgent)
	require.NoError(t, err)
	submitted, err := f.H.store.SubmitOwnOrder(t.Context(), owner, op, args, prep.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	evidence, err := f.H.store.SubmitOwnOrderEvidence(
		t.Context(),
		owner,
		uuid.New(),
		orderEvidenceArgs(t, f, submitted.Order, 61901, "native-order-review-write"),
	)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidencePending, evidence.State)
	require.Equal(t, int64(1), orderAllocationUnits(t, f, "held"))
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	return f, paymentScope(
			t,
			f.H,
			f.AmbassadorID,
			false,
		), db.OrderEvidenceReview{
			EventID:         f.Event.ID,
			EvidenceID:      evidence.ID,
			ExpectedVersion: evidence.Version,
			Decision:        db.OrderEvidenceConfirmed,
			Reason:          "Synthetic verified transfer",
		}, detailsAppSession(
			t,
			f.H,
			11003,
			119,
		)
}

func orderReviewFormCall(t *testing.T, f paymentFixture, session appSession, action string, body any) (int, []byte) {
	t.Helper()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		"/app/order-reviews/"+action,
		paymentAppJSON(t, body),
		session.Token,
		appTestOrigin,
	)
	return code, raw
}

func saveOrderReviewForm(
	t *testing.T,
	f paymentFixture,
	session appSession,
	op uuid.UUID,
	revision int64,
	review db.OrderEvidenceReview,
) db.OrderReviewWorkspace {
	t.Helper()
	code, raw := orderReviewFormCall(
		t,
		f,
		session,
		"workspaces",
		map[string]any{"operation_id": op, "expected_revision": revision, "language": "en", "review": review},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var w db.OrderReviewWorkspace
	require.NoError(t, json.Unmarshal(raw, &w))
	return w
}

// A fresh launch needs fresh signed Telegram data, not only another browser nonce.
func freshOrderReviewSession(t *testing.T, h *harness) appSession {
	t.Helper()
	fields, err := url.ParseQuery(appProof(11003))
	require.NoError(t, err)
	fields.Set("query_id", uuid.NewString())
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte("123:test-token"))
	mac := hmac.New(sha256.New, key.Sum(nil))
	_, _ = mac.Write(
		[]byte(
			"auth_date=" + fields.Get(
				"auth_date",
			) + "\nquery_id=" + fields.Get(
				"query_id",
			) + "\nuser=" + fields.Get(
				"user",
			),
		),
	)
	fields.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(fields.Encode(), 120), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	return session
}

func orderReviewConfirmBody(op uuid.UUID, w db.OrderReviewWorkspace) map[string]any {
	return map[string]any{
		"operation_id":          op,
		"canonical_proposal_id": w.Preparation.ID,
		"confirmed":             true,
		"arguments":             orderReviewCommit(w),
	}
}

func TestMiniAppOrderReviewHTTPHandoffsRecoverSameCanonicalDecision(t *testing.T) {
	t.Parallel()
	for _, start := range []string{"agent", "native"} {
		t.Run(start, func(t *testing.T) {
			t.Parallel()
			f, scope, review, session := nativeOrderReviewWriteFixture(t)
			save := uuid.New()
			var w db.OrderReviewWorkspace
			if start == "agent" {
				var err error
				w, err = f.H.store.PrepareOrderReviewWorkspace(
					t.Context(),
					scope,
					save,
					db.OrderReviewDraftChange{Language: "en", Review: review},
				)
				require.NoError(t, err)
			} else {
				w = saveOrderReviewForm(t, f, session, save, 0, review)
			}
			require.Contains(t, w.Preparation.Description, "Amount to confirm:")
			args := orderReviewCommit(w)
			agentOp, nativeOp := uuid.New(), uuid.New()
			prep, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, agentOp, args)
			require.NoError(t, err)
			var first db.OrderWorkspaceReviewResult
			if start == "native" {
				first, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, agentOp, args, prep.ID)
				require.NoError(t, err)
			}
			code, raw := orderReviewFormCall(t, f, session, "confirmations", orderReviewConfirmBody(nativeOp, w))
			require.Equal(t, http.StatusOK, code, string(raw))
			// Discard the successful body and recover from a fresh browser session.
			session = freshOrderReviewSession(t, f.H)
			code, raw, _ = appCall(
				t,
				f.H,
				http.MethodGet,
				"/app/order-reviews/operations/"+nativeOp.String(),
				"",
				session.Token,
				"",
			)
			require.Equal(t, http.StatusOK, code, string(raw))
			var receipt db.OrderReviewReceipt
			require.NoError(t, json.Unmarshal(raw, &receipt))
			require.True(t, receipt.Completed)
			require.Equal(t, "miniapp", receipt.Source)
			require.NotNil(t, receipt.Review)
			if start == "agent" {
				first = *receipt.Review
			}
			require.Equal(t, first, *receipt.Review)
			require.Equal(t, w.ID, first.CanonicalOperationID)
			require.Equal(t, db.OrderEvidenceConfirmed, first.Evidence.State)
			agent, err := f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, agentOp, args, prep.ID)
			require.NoError(t, err)
			require.Equal(t, first, agent)
			reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			recovered, err := reopened.OrderReviewOperationByID(t.Context(), scope, nativeOp)
			require.NoError(t, err)
			require.Equal(t, first, *recovered.Review)
			_, err = f.H.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_order_evidence_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour' WHERE id=$1", w.Preparation.ID)
			require.NoError(t, err)
			code, raw = orderReviewFormCall(t, f, session, "confirmations", orderReviewConfirmBody(nativeOp, w))
			require.Equal(t, http.StatusOK, code, string(raw))
			var replay db.OrderWorkspaceReviewResult
			require.NoError(t, json.Unmarshal(raw, &replay))
			require.Equal(t, first, replay)
			require.Zero(t, orderAllocationUnits(t, f, "held"))
			require.Equal(t, int64(1), orderAllocationUnits(t, f, "occupied"))
			var reviews, canonical int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_order_evidence_reviews WHERE eviden" +
"ce_id=$1),(SELECT count(*) FROM shuttle_order_evidence_operations WHERE " +
"operation_id=$2 AND kind='review')", review.EvidenceID, w.ID).
					Scan(&reviews, &canonical),
			)
			require.Equal(t, 1, reviews)
			require.Equal(t, 1, canonical)
		})
	}
}

func TestMiniAppOrderReviewHTTPEditsCancelAndOriginalReceipts(t *testing.T) {
	t.Parallel()
	f, _, review, session := nativeOrderReviewWriteFixture(t)
	save := uuid.New()
	first := saveOrderReviewForm(t, f, session, save, 0, review)
	changed := review
	changed.Reason = "Synthetic corrected reason"
	code, raw := orderReviewFormCall(
		t,
		f,
		session,
		"workspaces",
		map[string]any{"operation_id": save, "expected_revision": 0, "language": "en", "review": changed},
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	second := saveOrderReviewForm(t, f, session, uuid.New(), first.Revision, changed)
	code, raw = orderReviewFormCall(t, f, session, "confirmations", orderReviewConfirmBody(uuid.New(), first))
	require.Equal(t, http.StatusConflict, code, string(raw))
	cancelOp := uuid.New()
	cancel := map[string]any{"operation_id": cancelOp, "workspace_id": second.ID, "revision": second.Revision}
	code, raw = orderReviewFormCall(t, f, session, "cancellations", cancel)
	require.Equal(t, http.StatusOK, code, string(raw))
	var cancelled db.OrderReviewWorkspace
	require.NoError(t, json.Unmarshal(raw, &cancelled))
	require.Equal(t, "cancelled", cancelled.State)
	code, retry := orderReviewFormCall(t, f, session, "cancellations", cancel)
	require.Equal(t, http.StatusOK, code, string(retry))
	require.JSONEq(t, string(raw), string(retry))
	code, raw = orderReviewFormCall(t, f, session, "confirmations", orderReviewConfirmBody(uuid.New(), second))
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		"/app/order-reviews/operations/"+save.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt db.OrderReviewReceipt
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, &first, receipt.Workspace)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		"/app/order-reviews/operations/"+cancelOp.String(),
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, &cancelled, receipt.Workspace)
	require.Equal(t, int64(1), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
	var reviews int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence_reviews").Scan(&reviews),
	)
	require.Zero(t, reviews)
}

func TestMiniAppOrderReviewHTTPRequiresExplicitConsentAndAuthority(t *testing.T) {
	t.Parallel()
	f, _, review, session := nativeOrderReviewWriteFixture(t)
	makeBody := func() map[string]any {
		return map[string]any{
			"operation_id":      uuid.New(),
			"expected_revision": 0,
			"review": map[string]any{
				"event_id":         review.EventID,
				"evidence_id":      review.EvidenceID,
				"expected_version": review.ExpectedVersion,
				"decision":         review.Decision,
				"reason":           review.Reason,
			},
		}
	}
	for _, kind := range []string{"missing-revision", "null-revision", "missing-evidence-version", "null-evidence-version", "unknown-role"} {
		body := makeBody()
		switch kind {
		case "missing-revision":
			delete(body, "expected_revision")
		case "null-revision":
			body["expected_revision"] = nil
		case "missing-evidence-version":
			delete(body["review"].(map[string]any), "expected_version")
		case "null-evidence-version":
			body["review"].(map[string]any)["expected_version"] = nil
		case "unknown-role":
			body["global_admin"] = true
		}
		code, raw := orderReviewFormCall(t, f, session, "workspaces", body)
		require.Equal(t, http.StatusBadRequest, code, "%s: %s", kind, raw)
	}
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		"/app/order-reviews/workspaces",
		paymentAppJSON(t, makeBody()),
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	owner := detailsAppSession(t, f.H, 11002, 121)
	code, raw = orderReviewFormCall(t, f, owner, "workspaces", makeBody())
	require.Equal(t, http.StatusForbidden, code, string(raw))
	w := saveOrderReviewForm(t, f, session, uuid.New(), 0, review)
	body := orderReviewConfirmBody(uuid.New(), w)
	delete(body, "confirmed")
	code, raw = orderReviewFormCall(t, f, session, "confirmations", body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body = orderReviewConfirmBody(uuid.New(), w)
	body["canonical_proposal_id"] = uuid.New()
	code, raw = orderReviewFormCall(t, f, session, "confirmations", body)
	require.Equal(t, http.StatusConflict, code, string(raw))
	body = orderReviewConfirmBody(uuid.New(), w)
	args := orderReviewCommit(w)
	args.Review.Reason = "A different decision"
	body["arguments"] = args
	code, raw = orderReviewFormCall(t, f, session, "confirmations", body)
	require.Equal(t, http.StatusConflict, code, string(raw))
	require.Equal(t, int64(1), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
	var reviews int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence_reviews").Scan(&reviews),
	)
	require.Zero(t, reviews)
}

func TestMiniAppOrderReviewHTTPRevocationDuringConfirmationWaitPreventsEffects(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "role-removed"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, _, review, session := nativeOrderReviewWriteFixture(t)
			w := saveOrderReviewForm(t, f, session, uuid.New(), 0, review)
			operation := uuid.New()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			blocking, err := f.H.store.Pool().Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = blocking.Rollback(context.Background()) }()
			_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
			require.NoError(t, err)
			type outcome struct {
				code int
				raw  []byte
				err  error
			}
			done := make(chan outcome, 1)
			encoded := paymentAppJSON(t, orderReviewConfirmBody(operation, w))
			go func() {
				code, raw, _, requestErr := appHTTPCall(
					f.H,
					http.MethodPost,
					"/app/order-reviews/confirmations",
					encoded,
					session.Token,
					appTestOrigin,
				)
				done <- outcome{code, raw, requestErr}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				readErr := f.H.store.Pool().
					QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocking.Conn().PgConn().PID()).
					Scan(&waiting)
				return readErr == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			expected := http.StatusForbidden
			if kind == "logout" {
				code, raw, _ := appCall(
					t,
					f.H,
					http.MethodPost,
					"/app/session/logout",
					`{}`,
					session.Token,
					appTestOrigin,
				)
				require.Equal(t, http.StatusNoContent, code, string(raw))
				expected = http.StatusUnauthorized
			} else {
				_, err = f.H.store.Pool().
					Exec(ctx, "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
				require.NoError(t, err)
			}
			require.NoError(t, blocking.Rollback(ctx))
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.Equal(t, expected, result.code, string(result.raw))
			case <-ctx.Done():
				t.Fatal("confirmation did not finish")
			}
			var reviews, calls int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(ctx, "SELECT (SELECT count(*) FROM shuttle_order_evidence_reviews),(SELECT cou" +
"nt(*) FROM shuttle_order_review_calls WHERE operation_id=$1)", operation).
					Scan(&reviews, &calls),
			)
			require.Zero(t, reviews)
			require.Zero(t, calls)
			require.Equal(t, int64(1), orderAllocationUnits(t, f, "held"))
			require.Zero(t, orderAllocationUnits(t, f, "occupied"))
		})
	}
}
