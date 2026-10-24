package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const paymentAPI = "/internal/agent/payments/"

type paymentFixture struct {
	H                              *harness
	Event                          events.Event
	Pass                           registrations.Pass
	OwnerID, AdminID, AmbassadorID uuid.UUID
	Owner, Admin, Ambassador       string
}

func setupPayment(t *testing.T, p *peers) paymentFixture {
	t.Helper()
	h := setup(t, p)
	adminID, admin := registrationUser(t, h, 11001, true)
	ownerID, owner := registrationUser(t, h, 11002, false)
	ambID, amb := registrationUser(t, h, 11003, false)
	event := registrationEvent(t, h, admin)
	event.Document.Deposit = &registrations.DepositPolicy{MinimumMinor: 3000, MinimumBPS: 2500}
	code, data := adminRequest(
		t,
		h,
		admin,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{
			"operation_id":     "api-deposit-policy",
			"expected_version": event.Version,
			"document":         event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	entry := registerForEvent(t, h, owner, event.ID, "api-pay-join", "leader")
	code, data = adminRequest(
		t,
		h,
		admin,
		http.MethodPost,
		assignmentPath(event.ID, entry.ID),
		assignmentBody("api-pay-assign", event, entry),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &entry))
	require.NotNil(t, entry.Pass)
	operator := paymentScope(t, h, adminID, true)
	_, err := h.store.ChangeAuthority(
		context.Background(),
		operator.EventScope,
		operator.Subject,
		"api-pay-role",
		db.AuthorityChange{EventID: event.ID, UserID: ambID, Role: db.AuthorityAmbassador},
		uuid.Nil,
	)
	require.NoError(t, err)
	return paymentFixture{
		H:            h,
		Event:        event,
		Pass:         *entry.Pass,
		OwnerID:      ownerID,
		AdminID:      adminID,
		AmbassadorID: ambID,
		Owner:        owner,
		Admin:        admin,
		Ambassador:   amb,
	}
}
func paymentRequest(t *testing.T, f paymentFixture, token, method, operation string, args any) (int, []byte) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	if method == "submit" {
		body["idempotency_key"] = operation
		body["confirmation"] = map[string]any{"mode": "none"}
	}
	return adminRequest(t, f.H, token, http.MethodPost, paymentAPI+method, body)
}

func submittedPayment(
	t *testing.T,
	f paymentFixture,
	operation string,
	file uuid.UUID,
	amount int64,
	purpose string,
) payments.Evidence {
	t.Helper()
	code, data := paymentRequest(
		t,
		f,
		f.Owner,
		"submit",
		operation,
		payments.Submission{
			EventID:       f.Event.ID,
			PassID:        f.Pass.ID,
			FileID:        file,
			AmountMinor:   amount,
			Currency:      f.Pass.Currency,
			CurrencyScale: f.Pass.CurrencyScale,
			Purpose:       purpose,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var response struct {
		Status string            `json:"status"`
		Result payments.Evidence `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, "succeeded", response.Status)
	require.Equal(t, payments.Pending, response.Result.State)
	return response.Result
}

func preparedPayment(
	t *testing.T,
	f paymentFixture,
	token, operation string,
	args payments.ReviewChange,
) map[string]any {
	t.Helper()
	code, data := paymentRequest(t, f, token, "review/prepare", operation, args)
	require.Equal(t, http.StatusOK, code, string(data))
	var response struct {
		Status        string         `json:"status"`
		Preconditions map[string]any `json:"preconditions"`
		Description   string         `json:"description"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, "prepared", response.Status)
	require.Contains(t, response.Description, args.EvidenceID.String())
	return map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
		"confirmation":     map[string]any{"mode": "exact_parameters", "preconditions": response.Preconditions},
	}
}
func executePayment(t *testing.T, f paymentFixture, token string, body map[string]any) payments.ReviewResult {
	t.Helper()
	code, data := adminRequest(t, f.H, token, http.MethodPost, paymentAPI+"review", body)
	require.Equal(t, http.StatusOK, code, string(data))
	var response struct {
		Status string                `json:"status"`
		Result payments.ReviewResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, "succeeded", response.Status)
	return response.Result
}
func TestPaymentAPIConfirmationTopUpAndRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.fileBytes = []byte("%PDF-1.4\nproof fixture")
	f := setupPayment(t, p)
	for _, tc := range []struct {
		token  string
		review bool
	}{{f.Owner, false}, {f.Ambassador, true}, {f.Admin, true}} {
		code, data := adminRequest(t, f.H, tc.token, http.MethodPost, paymentAPI+"discover", map[string]any{})
		require.Equal(t, http.StatusOK, code, string(data))
		var discovery struct {
			Methods []struct {
				ID string `json:"id"`
			} `json:"methods"`
		}
		require.NoError(t, json.Unmarshal(data, &discovery))
		var ids []string
		for _, method := range discovery.Methods {
			ids = append(ids, method.ID)
		}
		expected := []string{"expectations", "operation", "events", "balance", "evidence", "list", "submit", "fetch"}
		if tc.review {
			expected = append(
				expected,
				"queue",
				"review",
				"review_workspace",
				"save_review",
				"cancel_review",
				"review_commit",
				"review_operation",
			)
		}
		require.ElementsMatch(t, expected, ids)
	}
	file := paymentFile(t, f.H, f.OwnerID, 11101, "api-first-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, f, "api-submit-first", file, 3087, payments.Initial)
	code, data := paymentRequest(
		t,
		f,
		f.Owner,
		"balance",
		"before-credit",
		map[string]any{"event_id": f.Event.ID, "pass_id": f.Pass.ID},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var balance struct {
		Result payments.Balance `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &balance))
	require.Zero(t, balance.Result.ConfirmedMinor)
	args := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	code, _ = paymentRequest(t, f, f.Owner, "review/prepare", "self-confirm-denied", args)
	require.Equal(t, http.StatusForbidden, code)
	body := preparedPayment(t, f, f.Ambassador, "api-confirm-first", args)
	code, data = paymentRequest(
		t,
		f,
		f.Owner,
		"balance",
		"still-no-credit",
		map[string]any{"event_id": f.Event.ID, "pass_id": f.Pass.ID},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &balance))
	require.Zero(t, balance.Result.ConfirmedMinor)
	first := executePayment(t, f, f.Ambassador, body)
	require.True(t, first.Balance.DepositSatisfied)
	require.False(t, first.Balance.FullyPaid)
	require.EqualValues(t, 9258, first.Balance.RemainingMinor)
	require.Equal(t, first, executePayment(t, f, f.Ambassador, body))
	code, data = paymentRequest(t, f, f.Ambassador, "fetch", "proof-read", map[string]any{"external_id": evidence.ID})
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotContains(t, string(data), "fixture-source")
	var fetched struct {
		Artifacts []struct {
			ExternalID string `json:"external_id"`
			Path       string `json:"download_path"`
		} `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(data, &fetched))
	require.Len(t, fetched.Artifacts, 1)
	require.Equal(t, evidence.ID.String(), fetched.Artifacts[0].ExternalID)
	code, data = adminRequest(t, f.H, f.Ambassador, http.MethodGet, fetched.Artifacts[0].Path, nil)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, p.fileBytes, data)
	code, _ = adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		"/internal/agent/files/info",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "no-general-file-access",
			"arguments":        map[string]any{"external_id": file},
		},
	)
	require.Equal(t, http.StatusNotFound, code)
	topfile := paymentFile(t, f.H, f.OwnerID, 11102, "api-topup-proof", time.Now().UTC().Truncate(time.Second))
	topup := submittedPayment(t, f, "api-submit-topup", topfile, 9258, payments.TopUp)
	args = payments.ReviewChange{
		EventID:         f.Event.ID,
		EvidenceID:      topup.ID,
		Decision:        payments.Confirmed,
		AmountMinor:     9258,
		ExpectedVersion: 1,
	}
	finalBody := preparedPayment(t, f, f.Ambassador, "api-confirm-topup", args)
	paid := executePayment(t, f, f.Ambassador, finalBody)
	require.True(t, paid.Balance.FullyPaid)
	require.Zero(t, paid.Balance.RemainingMinor)
	f.H.stop(t)
	f.H.start(t)
	require.Equal(t, paid, executePayment(t, f, f.Ambassador, finalBody))
	code, data = adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		paymentAPI+"review/status",
		map[string]any{"protocol_version": 1, "operation_id": "api-confirm-topup"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"fully_paid":true`)
	code, data = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		paymentAPI+"submit/status",
		map[string]any{"protocol_version": 1, "operation_id": "unknown-op"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"not_found"`)
	code, _ = adminRequest(
		t,
		f.H,
		"malformed",
		http.MethodPost,
		paymentAPI+"submit/status",
		map[string]any{"protocol_version": 1, "operation_id": "api-submit-first"},
	)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestConcurrentPaymentConfirmationsCannotOvercredit(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	sent := time.Now().UTC().Truncate(time.Second)
	a := submittedPayment(
		t,
		f,
		"concurrent-submit-a",
		paymentFile(t, f.H, f.OwnerID, 11201, "concurrent-proof-a", sent),
		f.Pass.PriceMinor,
		payments.Initial,
	)
	b := submittedPayment(
		t,
		f,
		"concurrent-submit-b",
		paymentFile(t, f.H, f.OwnerID, 11202, "concurrent-proof-b", sent),
		f.Pass.PriceMinor,
		payments.Initial,
	)
	first := preparedPayment(
		t,
		f,
		f.Ambassador,
		"concurrent-confirm-a",
		payments.ReviewChange{
			EventID:     f.Event.ID,
			EvidenceID:  a.ID,
			Decision:    payments.Confirmed,
			AmountMinor: f.Pass.PriceMinor,
		},
	)
	second := preparedPayment(
		t,
		f,
		f.Ambassador,
		"concurrent-confirm-b",
		payments.ReviewChange{
			EventID:     f.Event.ID,
			EvidenceID:  b.ID,
			Decision:    payments.Confirmed,
			AmountMinor: f.Pass.PriceMinor,
		},
	)
	results := make(chan string, 2)
	var wg sync.WaitGroup
	for _, body := range []map[string]any{first, second} {
		wg.Go(func() {
			code, data := adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review", body)
			require.Equal(t, http.StatusOK, code, string(data))
			var result struct {
				Status string `json:"status"`
			}
			require.NoError(t, json.Unmarshal(data, &result))
			results <- result.Status
		})
	}
	wg.Wait()
	close(results)
	statuses := []string{}
	for status := range results {
		statuses = append(statuses, status)
	}
	require.ElementsMatch(t, []string{"succeeded", "rejected"}, statuses)
	code, data := paymentRequest(
		t,
		f,
		f.Owner,
		"balance",
		"concurrent-balance",
		map[string]any{"event_id": f.Event.ID, "pass_id": f.Pass.ID},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var result struct {
		Result payments.Balance `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, f.Pass.PriceMinor, result.Result.ConfirmedMinor)
	require.EqualValues(t, 1, result.Result.Version)
}
func TestPaymentReviewRequiresFreshRoleExactProposalAndExpiry(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	evidence := submittedPayment(
		t,
		f,
		"guard-submit",
		paymentFile(t, f.H, f.OwnerID, 11301, "guard-proof", time.Now().UTC().Truncate(time.Second)),
		3087,
		payments.Initial,
	)
	args := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	body := preparedPayment(t, f, f.Ambassador, "guard-confirm", args)
	changed := args
	changed.AmountMinor = 3088
	body["arguments"] = changed
	code, data := adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review", body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	body["arguments"] = args
	proposal := body["confirmation"].(map[string]any)["preconditions"].(map[string]any)["proposal_id"].(string)
	_, err := f.H.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_payment_preparations SET expires_at=clock_timestamp()-int" +
"erval '1 second' WHERE proposal_id=$1", proposal)
	require.NoError(t, err)
	code, data = adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review", body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	body = preparedPayment(t, f, f.Ambassador, "guard-new-confirm", args)
	operator := paymentScope(t, f.H, f.AdminID, true)
	_, err = f.H.store.ChangeAuthority(
		context.Background(),
		operator.EventScope,
		operator.Subject,
		"guard-revoke",
		db.AuthorityChange{EventID: f.Event.ID, UserID: f.AmbassadorID, Role: db.AuthorityNone, ExpectedVersion: 1},
		uuid.Nil,
	)
	require.NoError(t, err)
	code, _ = adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review", body)
	require.Equal(t, http.StatusForbidden, code)
	code, data = paymentRequest(
		t,
		f,
		f.Owner,
		"balance",
		"guard-balance",
		map[string]any{"event_id": f.Event.ID, "pass_id": f.Pass.ID},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"confirmed_minor":0`)
}
func TestRejectedPaymentEvidenceCanBeCorrectedAndLateCaseMustBeExplicit(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	now := time.Now().UTC().Truncate(time.Second)
	due := now.Add(-time.Hour)
	_, err := f.H.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_passes SET payment_due_at=$2,assigned_at=$3 WHERE id=$1", f.Pass.ID, due, due.Add(-time.Hour))
	require.NoError(t, err)
	file := paymentFile(t, f.H, f.OwnerID, 11401, "late-correctable-proof", now)
	evidence := submittedPayment(t, f, "late-submit", file, 3087, payments.Initial)
	require.False(t, evidence.Timely)
	confirm := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	code, _ := paymentRequest(t, f, f.Ambassador, "review/prepare", "late-without-reason", confirm)
	require.Equal(t, http.StatusBadRequest, code)
	reject := payments.ReviewChange{
		EventID:    f.Event.ID,
		EvidenceID: evidence.ID,
		Decision:   payments.Rejected,
		Reason:     "Receipt belongs to a different expectation",
	}
	rejected := executePayment(t, f, f.Ambassador, preparedPayment(t, f, f.Ambassador, "late-reject", reject))
	require.Zero(t, rejected.Balance.ConfirmedMinor)
	require.Equal(t, payments.Rejected, rejected.Evidence.State)
	corrected := submittedPayment(t, f, "late-corrected", file, 3087, payments.Initial)
	require.NotEqual(t, evidence.ID, corrected.ID)
	confirm.EvidenceID = corrected.ID
	confirm.ExpectedVersion = 1
	confirm.Reason = "Event ambassador explicitly accepts this overdue payment after checking the receipt"
	accepted := executePayment(t, f, f.Ambassador, preparedPayment(t, f, f.Ambassador, "late-case-confirm", confirm))
	require.EqualValues(t, 3087, accepted.Balance.ConfirmedMinor)
	require.False(t, accepted.Evidence.Timely)
	require.Equal(t, confirm.Reason, accepted.Review.Reason)
}

func TestPaymentFileRechecksRevocationAfterSourceRead(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"fetch", "content"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.fileBytes = []byte("%PDF-1.4\nprivate proof")
			f := setupPayment(t, p)
			evidence := submittedPayment(
				t,
				f,
				"io-revoke-submit",
				paymentFile(t, f.H, f.OwnerID, 11501, "io-revoke-proof", time.Now().UTC().Truncate(time.Second)),
				3087,
				payments.Initial,
			)
			operator := paymentScope(t, f.H, f.AdminID, true)
			done := make(chan error, 1)
			p.mu.Lock()
			p.fileReadHook = func() {
				_, err := f.H.store.ChangeAuthority(
					context.Background(),
					operator.EventScope,
					operator.Subject,
					"io-revoke-authority",
					db.AuthorityChange{
						EventID:         f.Event.ID,
						UserID:          f.AmbassadorID,
						Role:            db.AuthorityNone,
						ExpectedVersion: 1,
					},
					uuid.Nil,
				)
				done <- err
			}
			p.mu.Unlock()
			var code int
			var data []byte
			if method == "fetch" {
				code, data = paymentRequest(
					t,
					f,
					f.Ambassador,
					"fetch",
					"revoke-during-fetch",
					map[string]any{"external_id": evidence.ID},
				)
			} else {
				code, data = adminRequest(
					t,
					f.H,
					f.Ambassador,
					http.MethodGet,
					"/internal/payment-evidence/"+evidence.ID.String()+"/content",
					nil,
				)
			}
			require.Equal(t, http.StatusForbidden, code, string(data))
			require.NotContains(t, string(data), "%PDF")
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("source read hook did not execute")
			}
		})
	}
}

func TestOtherEventAuthorityCanReadActualConfirmedAmountAndDecision(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	file := paymentFile(t, f.H, f.OwnerID, 11601, "auditable-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, f, "auditable-submit", file, 3000, payments.Initial)
	args := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 2500,
		Reason:      "Verified actual transfer differs from reported amount",
	}
	reviewed := executePayment(t, f, f.Ambassador, preparedPayment(t, f, f.Ambassador, "auditable-review", args))
	for _, tc := range []struct{ token, method string }{{f.Owner, "evidence"}, {f.Admin, "evidence"}, {f.Admin, "queue"}} {
		arguments := map[string]any{"event_id": f.Event.ID}
		if tc.method == "evidence" {
			arguments["evidence_id"] = evidence.ID
		}
		code, data := paymentRequest(t, f, tc.token, tc.method, "audit-read", arguments)
		require.Equal(t, http.StatusOK, code, string(data))
		require.Contains(t, string(data), `"amount_minor":3000`)
		require.Contains(t, string(data), `"amount_minor":2500`)
		require.Contains(t, string(data), args.Reason)
		require.Contains(t, string(data), reviewed.Review.ActorID.String())
	}
}

func TestPaymentEventDiscoveryPreservesClosedExpectationsAndOwnPrivacy(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	unrelated := registrationEvent(t, f.H, f.Admin)
	f.Event.Document.Registration.ClosesAt = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	code, data := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPut,
		"/internal/admin/events/"+f.Event.ID.String(),
		map[string]any{
			"operation_id":     "close-sales-for-context",
			"expected_version": f.Event.Version,
			"document":         f.Event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	for _, tc := range []struct {
		token string
		own   bool
	}{{f.Owner, true}, {f.Ambassador, false}} {
		code, data = paymentRequest(t, f, tc.token, "events", "find-payment-context", map[string]any{})
		require.Equal(t, http.StatusOK, code, string(data))
		require.Contains(t, string(data), f.Event.ID.String())
		require.NotContains(t, string(data), unrelated.ID.String())
		require.Equal(t, tc.own, bytes.Contains(data, []byte(f.Pass.ID.String())))
	}
}
