package integration_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const orderEvidenceAPI = "/internal/agent/order-payments/"

func orderEvidenceRead(t *testing.T, f paymentFixture, token, method string, args any) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		orderEvidenceAPI+method,
		map[string]any{"protocol_version": 1, "operation_id": "order-proof-read", "arguments": args},
	)
}

func TestOrderEvidenceAPIReadsCurrentStateAndOriginalReceipts(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61301, "api-order-proof")
	op := uuid.New()
	admitted, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	reviewOp, _, confirmed := reviewOrderEvidence(t, f, admitted, db.OrderEvidenceConfirmed)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	detail := map[string]any{"event_id": f.Event.ID, "evidence_id": admitted.ID}
	for _, tc := range []struct{ token, method string }{{f.Owner, "evidence"}, {f.Ambassador, "evidence_for_review"}, {f.Admin, "evidence_for_review"}} {
		code, raw := orderEvidenceRead(t, f, tc.token, tc.method, detail)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out orderWireResult[db.OrderPaymentEvidence]
		require.NoError(t, json.Unmarshal(raw, &out))
		require.NotNil(t, out.Result)
		require.Equal(t, confirmed, *out.Result)
	}
	code, raw := orderEvidenceRead(t, f, f.Ambassador, "evidence", detail)
	require.NotEqual(t, http.StatusOK, code, string(raw))
	code, raw = orderEvidenceRead(t, f, f.Owner, "evidence_for_review", detail)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	for _, tc := range []struct {
		token  string
		op     uuid.UUID
		kind   db.OrderEvidenceOperationKind
		result db.OrderPaymentEvidence
	}{{f.Owner, op, db.OrderEvidenceSubmissionReceipt, admitted}, {f.Ambassador, reviewOp, db.OrderEvidenceReviewReceipt, confirmed}} {
		code, raw = orderEvidenceRead(
			t,
			f,
			tc.token,
			"operation",
			map[string]any{"operation_id": tc.op, "kind": tc.kind},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		var out orderWireResult[db.OrderEvidenceOperation]
		require.NoError(t, json.Unmarshal(raw, &out))
		require.Equal(t, "succeeded", out.Status)
		require.NotNil(t, out.Result)
		require.Equal(t, tc.result, out.Result.Result)
	}
	code, raw = orderEvidenceRead(
		t,
		f,
		f.Owner,
		"operation",
		map[string]any{"operation_id": reviewOp, "kind": "review"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	require.NotContains(t, string(raw), confirmed.ID.String())
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	code, raw = orderEvidenceRead(
		t,
		f,
		f.Ambassador,
		"operation",
		map[string]any{"operation_id": reviewOp, "kind": "review"},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), confirmed.ID.String())
}

func TestOrderEvidenceAPIPagesAreByteBoundedWithoutSkipping(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61302, "api-order-pages")
	ids := []string{}
	for range 21 {
		evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
		require.NoError(t, err)
		ids = append(ids, evidence.ID.String())
		actor := paymentScope(t, f.H, f.AmbassadorID, false)
		change := db.OrderEvidenceReview{
			EventID:         f.Event.ID,
			EvidenceID:      evidence.ID,
			ExpectedVersion: 1,
			Decision:        db.OrderEvidenceRejected,
			Reason:          strings.Repeat("<", 512),
		}
		op := uuid.New()
		prepared, err := f.H.store.PrepareOrderEvidenceReview(t.Context(), actor, op, change)
		require.NoError(t, err)
		_, err = f.H.store.ReviewOrderEvidence(t.Context(), actor, op, prepared.ID, change)
		require.NoError(t, err)
	}
	pending, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	ids = append(ids, pending.ID.String())
	after := uuid.Nil
	found := []string{}
	pages := 0
	shortened := false
	for {
		pages++
		require.Less(t, pages, 25)
		code, raw := orderEvidenceRead(
			t,
			f,
			f.Owner,
			"list",
			map[string]any{"event_id": f.Event.ID, "order_id": order.ID, "after": after, "limit": 20},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.LessOrEqual(t, len(raw), 64<<10, "Full HTTP envelope must fit the advertised limit")
		var out orderWireResult[db.OrderEvidencePage]
		require.NoError(t, json.Unmarshal(raw, &out))
		require.NotNil(t, out.Result)
		for _, e := range out.Result.Evidence {
			found = append(found, e.ID.String())
		}
		if out.Result.Next == nil {
			break
		}
		if len(out.Result.Evidence) < 20 {
			shortened = true
		}
		require.NotEmpty(t, out.Result.Evidence)
		require.Equal(t, out.Result.Evidence[len(out.Result.Evidence)-1].ID, *out.Result.Next)
		require.NotEqual(t, after, *out.Result.Next)
		after = *out.Result.Next
	}
	require.True(t, shortened, "Large literal review reasons must exercise the byte limit")
	sort.Strings(ids)
	require.Equal(t, ids, found, "UUID cursor must not skip or repeat retained evidence")
	code, raw := orderEvidenceRead(t, f, f.Ambassador, "queue", map[string]any{"event_id": f.Event.ID, "limit": 20})
	require.Equal(t, http.StatusOK, code, string(raw))
	var queue orderWireResult[db.OrderEvidencePage]
	require.NoError(t, json.Unmarshal(raw, &queue))
	require.NotNil(t, queue.Result)
	require.Equal(t, []db.OrderPaymentEvidence{pending}, queue.Result.Evidence)
	require.Nil(t, queue.Result.Next)
	code, raw = orderEvidenceRead(t, f, f.Owner, "queue", map[string]any{"event_id": f.Event.ID})
	require.Equal(t, http.StatusForbidden, code, string(raw))
}

func TestOrderEvidenceAPIFilesReauthorizeAfterTelegramIO(t *testing.T) {
	t.Parallel()
	for _, audience := range []string{"owner", "reviewer"} {
		for _, method := range []string{"fetch", "content"} {
			t.Run(audience+"/"+method, func(t *testing.T) {
				t.Parallel()
				checkOrderEvidenceFileRevocation(t, audience, method)
			})
		}
	}
}

func checkOrderEvidenceFileRevocation(t *testing.T, audience, method string) {
	t.Helper()
	f, scope, order := orderEvidenceFixture(t)
	p := f.H.peers
	p.mu.Lock()
	p.fileBytes = []byte("%PDF-1.4\nprivate order proof")
	p.mu.Unlock()
	args := orderEvidenceArgs(t, f, order, 61303, "api-order-file-revoke")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	token, fetch, suffix := f.Owner, "fetch", "/content"
	if audience == "reviewer" {
		token, fetch, suffix = f.Ambassador, "fetch_for_review", "/review-content"
	}
	// First prove the same real HTTP download route succeeds before revocation.
	code, raw := adminRequest(
		t,
		f.H,
		token,
		http.MethodGet,
		"/internal/order-evidence/"+evidence.ID.String()+suffix,
		nil,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "%PDF-1.4\nprivate order proof", string(raw))

	code, raw = orderEvidenceRead(t, f, token, fetch, map[string]any{"external_id": evidence.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var descriptor struct {
		Result struct {
			ID uuid.UUID `json:"id"`
		} `json:"result"`
		Artifacts []map[string]any `json:"artifacts"`
	}
	require.NoError(t, json.Unmarshal(raw, &descriptor))
	require.Equal(t, args.FileID, descriptor.Result.ID)
	require.Len(t, descriptor.Artifacts, 1)
	require.Equal(t, evidence.ID.String(), descriptor.Artifacts[0]["external_id"])
	require.Equal(t, fetch, descriptor.Artifacts[0]["fetch_method"])
	require.Equal(t, "/internal/order-evidence/"+evidence.ID.String()+suffix, descriptor.Artifacts[0]["download_path"])
	done := make(chan error, 1)
	p.mu.Lock()
	p.fileReadHook = func() {
		var hookErr error
		if audience == "owner" {
			_, hookErr = f.H.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
		} else {
			_, hookErr = f.H.store.Pool().
				Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
		}
		done <- hookErr
	}
	p.mu.Unlock()
	if method == "fetch" {
		code, raw = orderEvidenceRead(t, f, token, fetch, map[string]any{"external_id": evidence.ID})
	} else {
		code, raw = adminRequest(
			t,
			f.H,
			token,
			http.MethodGet,
			"/internal/order-evidence/"+evidence.ID.String()+suffix,
			nil,
		)
	}
	require.Contains(
		t,
		[]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
		code,
		string(raw),
	)
	require.NotContains(t, string(raw), "private order proof")
	require.NotContains(t, string(raw), `"artifacts"`)
	select {
	case readErr := <-done:
		require.NoError(t, readErr)
	case <-time.After(time.Second):
		t.Fatal("source read hook did not execute")
	}
}

func TestOrderEvidenceAPIDiscoveryTracksOrdersAuthority(t *testing.T) {
	t.Parallel()
	f, _, _ := orderEvidenceFixture(t)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	for _, tc := range []struct {
		token string
		audit bool
	}{{f.Owner, false}, {f.Ambassador, true}, {f.Admin, true}} {
		code, raw := adminRequest(t, f.H, tc.token, http.MethodPost, orderEvidenceAPI+"discover", nil)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), `"id":"evidence"`)
		require.Contains(t, string(raw), `"id":"operation"`)
		require.Contains(t, string(raw), `"max_result_bytes":65536`)
		require.Contains(t, string(raw), `"id":"submit"`)
		if tc.audit {
			require.Contains(t, string(raw), `"id":"queue"`)
			require.Contains(t, string(raw), `"id":"fetch_for_review"`)
		} else {
			require.NotContains(t, string(raw), `"id":"queue"`)
			require.NotContains(t, string(raw), `"id":"fetch_for_review"`)
		}
	}
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	code, raw := adminRequest(t, f.H, f.Ambassador, http.MethodPost, orderEvidenceAPI+"discover", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), `"id":"queue"`)
}

func TestOrderEvidenceAPIRejectsFileIdentityChangeDuringRead(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"fetch", "content"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f, scope, order := orderEvidenceFixture(t)
			p := f.H.peers
			p.mu.Lock()
			p.fileBytes = []byte("private original order proof")
			p.mu.Unlock()
			args := orderEvidenceArgs(t, f, order, 61304, "api-order-file-replaced")
			evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
			require.NoError(t, err)
			done := make(chan error, 1)
			p.mu.Lock()
			p.fileReadHook = func() {
				_, hookErr := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_files SET telegram_id='changed-source' WHERE id=$1", args.FileID)
				done <- hookErr
			}
			p.mu.Unlock()
			var code int
			var raw []byte
			if method == "fetch" {
				code, raw = orderEvidenceRead(t, f, f.Owner, "fetch", map[string]any{"external_id": evidence.ID})
			} else {
				code, raw = adminRequest(
					t,
					f.H,
					f.Owner,
					http.MethodGet,
					"/internal/order-evidence/"+evidence.ID.String()+"/content",
					nil,
				)
			}
			require.Equal(t, http.StatusConflict, code, string(raw))
			require.NotContains(t, string(raw), "private original order proof")
			require.NotContains(t, string(raw), `"artifacts"`)
			select {
			case readErr := <-done:
				require.NoError(t, readErr)
			case <-time.After(time.Second):
				t.Fatal("source read hook did not execute")
			}
		})
	}
}
