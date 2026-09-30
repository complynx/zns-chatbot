package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// A different surviving grant must not authorize a transformed result from the revoked event.
func TestPrivilegedScriptTransformedResultUsesExactPaymentScope(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"passes.payments.history", "passes.payments.queue"} {
		for _, revoke := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/revoke=%t", tool, revoke), func(t *testing.T) {
				t.Parallel()
				f := setup(t)
				seedPrivilegedReads(t, f)
				_, err := f.db.Exec(t.Context(), `
INSERT INTO core.pass_events(id,finishes_at) VALUES('surviving-payment-scope',now()+interval '30 days');
INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('surviving-payment-scope','alice')`)
				require.NoError(t, err)
				var marker string
				workerRuns := 0
				model := runScriptReads(t, f, hostScriptFunc(func(
					ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback,
				) (json.RawMessage, error) {
					workerRuns++
					data := scriptCall(ctx, t, callback, tool, `{"event":"script-dance"}`)
					if tool == "passes.payments.history" {
						require.Contains(t, string(data), "visitor")
					}
					marker = fmt.Sprintf("derived-payment-%x", sha256.Sum256(data))
					if revoke {
						_, deleteErr := f.db.Exec(ctx,
							`DELETE FROM core.pass_payment_admins WHERE event_id='script-dance' AND owner='alice'`)
						require.NoError(t, deleteErr)
					}
					result, encodeErr := json.Marshal(map[string]string{"digest": marker})
					return result, encodeErr
				}))
				if revoke {
					assertPrivilegedFreshInput(t, f, model, marker)
				}
				require.Equal(t, 1, workerRuns)
				require.Len(t, model.inputs, 2)
				assertPrivilegedPaymentMarker(t, model, marker, revoke)
				caps, err := (core.Service{DB: f.db}).PrivilegedReads(t.Context(), "alice")
				require.NoError(t, err)
				assert.True(t, caps.PaymentReads, "the unrelated event grant remains usable")
				_, err = (passbooking.Service{DB: f.db}).PaymentHistoryPage(
					t.Context(), "alice", "surviving-payment-scope", "")
				require.NoError(t, err)
			})
		}
	}
}

func assertPrivilegedPaymentMarker(t *testing.T, model *knowledgeModel, marker string, revoke bool) {
	t.Helper()
	encoded, err := json.Marshal(model.inputs[1])
	require.NoError(t, err)
	if revoke {
		assert.NotContains(t, string(encoded), marker)
	} else {
		assert.Contains(t, string(encoded), marker)
	}
}

func TestPrivilegedScriptPaymentSourceSurvivesMemoryPersistence(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPrivilegedReads(t, f)
	ctx := t.Context()
	_, err := f.db.Exec(ctx, `
INSERT INTO core.pass_events(id,finishes_at) VALUES('surviving-payment-scope',now()+interval '30 days');
INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('surviving-payment-scope','alice')`)
	require.NoError(t, err)
	var marker string
	model := runScriptReads(t, f, hostScriptFunc(func(
		callCtx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback,
	) (json.RawMessage, error) {
		data := scriptCall(callCtx, t, callback, "passes.payments.history", `{"event":"script-dance"}`)
		require.Contains(t, string(data), "visitor")
		marker = fmt.Sprintf("persisted-payment-%x", sha256.Sum256(data))
		result, encodeErr := json.Marshal(map[string]string{"digest": marker})
		return result, encodeErr
	}))
	require.Len(t, model.inputs, 2)
	require.NotNil(t, model.inputs[1].Script)
	generation := int64(0)
	source := readsource.Derivation{
		Generation:  &generation,
		Authorities: model.inputs[1].Script.ReadAuthorities,
	}
	command := knowledge.Command{
		Name: knowledge.DocumentSet, Key: "payment-copy", Topic: "notes", FactKey: "payment-copy", Text: marker,
	}
	service := knowledge.Service{DB: f.db}
	_, err = service.ExecuteDerived(ctx, "alice", command, source)
	require.NoError(t, err)
	query := knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate, Topic: "notes"}
	before, err := service.SearchMemory(ctx, "alice", query)
	require.NoError(t, err)
	beforeJSON, err := json.Marshal(before)
	require.NoError(t, err)
	require.Contains(t, string(beforeJSON), marker)
	_, err = f.db.Exec(ctx, `DELETE FROM core.pass_payment_admins WHERE event_id='script-dance' AND owner='alice'`)
	require.NoError(t, err)
	// Reconstruct the service so the read is based on persisted provenance, not an in-memory handle.
	reopened := knowledge.Service{DB: f.db}
	after, err := reopened.SearchMemory(ctx, "alice", query)
	require.NoError(t, err)
	afterJSON, err := json.Marshal(after)
	require.NoError(t, err)
	assert.NotContains(t, string(afterJSON), marker)
	command.Key = "late-payment-copy"
	command.FactKey = "late-payment-copy"
	_, err = reopened.ExecuteDerived(ctx, "alice", command, source)
	require.Error(t, err, "a new derivative cannot reuse a revoked source")
	_, err = f.db.Exec(ctx, `INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('script-dance','alice')`)
	require.NoError(t, err)
	restored, err := reopened.SearchMemory(ctx, "alice", query)
	require.NoError(t, err)
	restoredJSON, err := json.Marshal(restored)
	require.NoError(t, err)
	assert.NotContains(t, string(restoredJSON), marker, "observed revocation permanently hides the old derivative")
}
