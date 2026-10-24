package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const nativePassPaymentHTTPPath = "/app/pass-payments/"

func nativePassPaymentHTTPFixture(t *testing.T) (paymentFixture, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.fileBytes = []byte("%PDF-1.4\nprivate participant payment proof")
	f := setupPayment(t, p)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, f.H.service.Stop(ctx))
	f.H.service = nil
	return f, detailsAppSession(t, f.H, 11002, 131)
}

func nativePassPaymentHTTPEvent(f paymentFixture) string {
	return nativePassPaymentHTTPPath + "events/" + f.Event.ID.String()
}

func nativePassPaymentHTTPBody(
	t *testing.T,
	f paymentFixture,
	operation string,
	file uuid.UUID,
	amount int64,
	purpose string,
) string {
	t.Helper()
	return paymentAppJSON(
		t,
		map[string]any{
			"operation_id": operation,
			"pass_id":      f.Pass.ID,
			"file_id":      file,
			"amount_minor": amount,
			"purpose":      purpose,
		},
	)
}
func nativePassPaymentHTTPSubmit(t *testing.T, f paymentFixture, session appSession, body string) payments.Evidence {
	t.Helper()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		nativePassPaymentHTTPEvent(f)+"/evidence",
		body,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var result payments.Evidence
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func TestNativePassPaymentHTTPDepositTopUpAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	f, owner := nativePassPaymentHTTPFixture(t)
	reviewer := detailsAppSession(t, f.H, 11003, 132)
	firstFile := paymentFile(t, f.H, f.OwnerID, 66101, "native-pass-deposit", time.Now().UTC().Truncate(time.Second))
	body := nativePassPaymentHTTPBody(t, f, "native:deposit", firstFile, 3087, payments.Initial)
	f.H.peers.loom.Close()
	first := nativePassPaymentHTTPSubmit(t, f, owner, body)
	require.Equal(t, payments.Pending, first.State)
	scope := paymentScope(t, f.H, f.OwnerID, false)
	balance, err := f.H.store.PaymentBalance(t.Context(), scope, f.Event.ID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
	review := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  first.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	workspace := paymentAppSaved(t, f, reviewer, review, 0)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		paymentAppConfirmBody(t, workspace),
		reviewer.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	balance, err = f.H.store.PaymentBalance(t.Context(), scope, f.Event.ID, f.Pass.ID)
	require.NoError(t, err)
	require.True(t, balance.DepositSatisfied)
	require.False(t, balance.FullyPaid)
	replay := nativePassPaymentHTTPSubmit(t, f, owner, body)
	require.Equal(t, first, replay)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		nativePassPaymentHTTPEvent(f)+"/operations/native:deposit",
		"",
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt db.MiniAppPaymentEvidenceReceipt
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, first, receipt.Result)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		nativePassPaymentHTTPEvent(f)+"/evidence/"+first.ID.String(),
		"",
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"confirmed"`)
	topFile := paymentFile(t, f.H, f.OwnerID, 66102, "native-pass-topup", time.Now().UTC().Truncate(time.Second))
	top := nativePassPaymentHTTPSubmit(
		t,
		f,
		owner,
		nativePassPaymentHTTPBody(t, f, "native:topup", topFile, balance.RemainingMinor, payments.TopUp),
	)
	review.EvidenceID = top.ID
	review.ExpectedVersion = balance.Version
	review.AmountMinor = balance.RemainingMinor
	workspace = paymentAppSaved(t, f, reviewer, review, 0)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		paymentAppConfirmBody(t, workspace),
		reviewer.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		nativePassPaymentHTTPEvent(f)+"/passes/"+f.Pass.ID.String(),
		"",
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"fully_paid":true`)
	code, raw, _ = appCall(t, f.H, http.MethodGet, nativePassPaymentHTTPPath+"recent", "", owner.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"actions":[`)
	require.Contains(t, string(raw), first.ID.String())
	require.Contains(t, string(raw), top.ID.String())
}

func TestNativePassPaymentHTTPStrictClaimsAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	f, owner := nativePassPaymentHTTPFixture(t)
	file := paymentFile(t, f.H, f.OwnerID, 66201, "native-pass-strict", time.Now().UTC().Truncate(time.Second))
	body := nativePassPaymentHTTPBody(t, f, "strict-claim", file, 3087, payments.Initial)
	for _, invalid := range []string{`{}`, `null`, strings.Replace(body, `,"purpose":"initial"`, "", 1), strings.TrimSuffix(body, "}") + `,"currency":"BYN"}`, strings.TrimSuffix(body, "}") + `,"operation_id":"duplicate"}`, body + ` {}`, strings.Replace(body, `"initial"`, `null`, 1), strings.Replace(body, `3087`, `9007199254740992`, 1)} {
		code, raw, _ := appCall(
			t,
			f.H,
			http.MethodPost,
			nativePassPaymentHTTPEvent(f)+"/evidence",
			invalid,
			owner.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	foreignFile := paymentFile(
		t,
		f.H,
		f.AmbassadorID,
		66202,
		"native-pass-foreign",
		time.Now().UTC().Truncate(time.Second),
	)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		nativePassPaymentHTTPEvent(f)+"/evidence",
		nativePassPaymentHTTPBody(t, f, "foreign-file", foreignFile, 3087, payments.Initial),
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	first := nativePassPaymentHTTPSubmit(t, f, owner, body)
	admin := detailsAppSession(t, f.H, 11001, 133)
	for _, path := range []string{nativePassPaymentHTTPEvent(f) + "/evidence/" + first.ID.String(), nativePassPaymentHTTPEvent(f) + "/passes/" + f.Pass.ID.String(), nativePassPaymentHTTPEvent(f) + "/operations/strict-claim"} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path, "", admin.Token, appTestOrigin)
		require.Equal(t, http.StatusNotFound, code, string(raw))
		require.NotContains(t, string(raw), first.ID.String())
	}
	for _, query := range []string{"?limit=21", "?limit=0", "?after=", "?after=" + uuid.NewString() + "&after=" + uuid.NewString()} {
		code, raw, _ = appCall(
			t,
			f.H,
			http.MethodGet,
			nativePassPaymentHTTPPath+"targets"+query,
			"",
			owner.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
}

func TestNativePassPaymentHTTPAgentOriginHistoryAndFiles(t *testing.T) {
	t.Parallel()
	f, owner := nativePassPaymentHTTPFixture(t)
	file := paymentFile(t, f.H, f.OwnerID, 66301, "agent-origin-native-read", time.Now().UTC().Truncate(time.Second))
	first := submittedPayment(t, f, "agent-proof-native", file, 3087, payments.Initial)
	for _, path := range []string{nativePassPaymentHTTPEvent(f) + "/evidence?pass_id=" + f.Pass.ID.String(), nativePassPaymentHTTPEvent(f) + "/operations/agent-proof-native"} {
		code, raw, _ := appCall(t, f.H, http.MethodGet, path, "", owner.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), first.ID.String())
	}
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodGet,
		nativePassPaymentHTTPEvent(f)+"/files",
		"",
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), file.String())
	require.NotContains(t, string(raw), "fixture-source")
	code, raw, headers := appCall(
		t,
		f.H,
		http.MethodGet,
		nativePassPaymentHTTPEvent(f)+"/files/"+file.String()+"/content",
		"",
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, f.H.peers.fileBytes, raw)
	require.Equal(t, "attachment", headers.Get("Content-Disposition"))
	native := nativePassPaymentHTTPSubmit(
		t,
		f,
		owner,
		nativePassPaymentHTTPBody(t, f, "agent-proof-native", file, 3087, payments.Initial),
	)
	require.Equal(t, first, native)
}

func TestNativePassPaymentHTTPContentRevocationAfterIO(t *testing.T) {
	t.Parallel()
	f, owner := nativePassPaymentHTTPFixture(t)
	file := paymentFile(t, f.H, f.OwnerID, 66401, "native-pass-revoke-content", time.Now().UTC().Truncate(time.Second))
	done := make(chan error, 1)
	f.H.peers.mu.Lock()
	f.H.peers.fileReadHook = func() {
		code, raw, _, err := appHTTPCall(f.H, http.MethodPost, "/app/session/logout", `{}`, owner.Token, appTestOrigin)
		if err == nil && code != http.StatusNoContent {
			err = fmt.Errorf("logout status %d: %s", code, raw)
		}
		done <- err
	}
	f.H.peers.mu.Unlock()
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodGet,
		nativePassPaymentHTTPEvent(f)+"/files/"+file.String()+"/content",
		"",
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
	require.NotContains(t, string(raw), "%PDF")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("file IO revocation did not run")
	}
}

func TestNativePassPaymentHTTPSessionLogoutWhileEventLockedPreventsWrite(t *testing.T) {
	t.Parallel()
	f, owner := nativePassPaymentHTTPFixture(t)
	file := paymentFile(t, f.H, f.OwnerID, 66501, "native-pass-event-wait", time.Now().UTC().Truncate(time.Second))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	body := nativePassPaymentHTTPBody(t, f, "waiting-native-proof", file, 3087, payments.Initial)
	go func() {
		code, raw, _, callErr := appHTTPCall(
			f.H,
			http.MethodPost,
			nativePassPaymentHTTPEvent(f)+"/evidence",
			body,
			owner.Token,
			appTestOrigin,
		)
		done <- response{code, raw, callErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return readErr == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	code, raw, _ := appCall(t, f.H, http.MethodPost, "/app/session/logout", `{}`, owner.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	require.NoError(t, blocker.Rollback(ctx))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
	case <-ctx.Done():
		t.Fatal("blocked submission did not return")
	}
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_payment_evidence WHERE file_id=$1", file).
			Scan(&count),
	)
	require.Zero(t, count)
}

func TestNativePassPaymentHTTPSessionExpiryWhileSourceLockedRollsBack(t *testing.T) {
	t.Parallel()
	f, owner := nativePassPaymentHTTPFixture(t)
	const source = "native-pass-expire-source"
	const operation = "source-wait-expiry"
	file := paymentFile(t, f.H, f.OwnerID, 66601, source, time.Now().UTC().Truncate(time.Second))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))",
		fmt.Sprintf("payment-source:123:org:%s:%s", f.OwnerID, source),
	)
	require.NoError(t, err)
	var expires time.Time
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()+interva" +
"l '2 seconds' WHERE user_id=$1 AND state='active' RETURNING expires_at", owner.UserID).
			Scan(&expires),
	)
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	body := nativePassPaymentHTTPBody(t, f, operation, file, 3087, payments.Initial)
	go func() {
		code, raw, _, callErr := appHTTPCall(
			f.H,
			http.MethodPost,
			nativePassPaymentHTTPEvent(f)+"/evidence",
			body,
			owner.Token,
			appTestOrigin,
		)
		done <- response{code, raw, callErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return readErr == nil && waiting
	}, time.Second, 10*time.Millisecond, "submission must pass initial session authorization and wait on the source lock")
	timer := time.NewTimer(time.Until(expires))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("session expiry wait exceeded test deadline")
	}
	require.NoError(t, blocker.Rollback(ctx))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusUnauthorized, result.code, string(result.raw))
	case <-ctx.Done():
		t.Fatal("source-blocked submission did not finish")
	}
	var evidence, receipts, actions, notices int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT\n (SELECT count(*) FROM shuttle_payment_evidence WHERE file_id=$1)" +
",\n (SELECT count(*) FROM shuttle_payment_operations WHERE actor=$2 AND o" +
"peration_id=$3),\n (SELECT count(*) FROM shuttle_native_payment_evidence_" +
"actions WHERE actor=$2 AND operation_id=$3),\n (SELECT count(*) FROM shut" +
"tle_payment_notifications n JOIN shuttle_payment_evidence e ON e.id=n.ev" +
"idence_id WHERE e.file_id=$1)", file, paymentScope(t, f.H, f.OwnerID, false).Subject, operation).Scan(&evidence, &receipts, &actions, &notices))
	require.Zero(t, evidence, "expired session must not submit evidence")
	require.Zero(t, receipts, "expired submission must not save a receipt")
	require.Zero(t, actions, "expired submission must not save native history")
	require.Zero(t, notices, "expired submission must not notify reviewers")
}
