package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func capturePassCompletion(t *testing.T, f *fixture, update int64, text, code string) *knowledgeModel {
	t.Helper()
	return capturePassCompletionRuns(t, f, update, text, code)
}

// Each code value is a separate model-proposed script run in the same update.
func capturePassCompletionRuns(t *testing.T, f *fixture, update int64, text string, codes ...string) *knowledgeModel {
	t.Helper()
	f.b.Scripts = scopeVM{}
	plans := make([]agent.Plan, 0, len(codes)+1)
	for _, code := range codes {
		plans = append(
			plans,
			agent.Plan{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		)
	}
	model := &knowledgeModel{plans: append(plans, agent.Plan{View: "workflow", Text: "Checked"})}
	f.b.Model = model
	handleErr := f.b.Handle(t.Context(), message(update, 202, text))
	t.Logf("handle error=%v model calls=%d", handleErr, len(model.inputs))
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	for _, record := range records {
		t.Logf(
			"run error=%q pass_retired=%t history_retired=%t",
			record.Run.Error,
			record.PassRedacted,
			record.HistoryRedacted,
		)
		for _, call := range record.Calls {
			var result struct {
				Complete    bool `json:"complete"`
				Interrupted bool `json:"interrupted"`
				Result      struct {
					Delivered bool `json:"delivered"`
				} `json:"result"`
			}
			if len(call.Outcome.Result) > 0 {
				require.NoError(t, json.Unmarshal(call.Outcome.Result, &result))
			}
			t.Logf(
				"call=%s error=%q complete=%t interrupted=%t delivered=%t",
				call.Outcome.Name,
				call.Outcome.Error,
				result.Complete,
				result.Interrupted,
				result.Result.Delivered,
			)
		}
	}
	require.NoError(t, handleErr)
	return model
}

func TestScriptPassAssignmentSharedHeadDiagnostic(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Test Name'`)
	require.NoError(t, err)
	result := runPassVM(
		t,
		f,
		59400,
		202,
		"Assign pass for 101 using profile",
		`const target=await tools.passes.admin.target({event:"dance",target:"101"});return tools.passes.admin.assign({event:"dance",target:target.admin_target.booking.owner,assignment:{create:true,from_profile:true,total_price:150}});`,
	)
	require.Empty(t, result.Error)
	result = runPassVM(
		t,
		f,
		59401,
		101,
		"Show pass payment",
		`return tools.passes.registration.show({event:"dance",view:"payment"});`,
	)
	require.Empty(t, result.Error)
	var owner, key, state string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT owner_kind,owner_key,state FROM core.delivery_queue WHERE bot_id=$1 AND chat='101' AND state IN ('pending','sending','unknown','parked','paused') ORDER BY lane_sequence LIMIT 1`, f.b.Delivery.BotID).
			Scan(&owner, &key, &state),
	)
	t.Logf("Alice lane head owner=%s key=%s state=%s", owner, key, state)
	require.Equal(t, string(delivery.Passes), owner)
	require.Equal(t, string(delivery.Deferred), state)
	id, err := strconv.ParseInt(key, 10, 64)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverPassNotification(t.Context(), id))
	pumpBotDeliveries(t, f.b)
	var queue json.RawMessage
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object('owner',owner_kind,'key',owner_key,'state',state,'due',not_before<=now()) ORDER BY lane_sequence),'[]'::jsonb) FROM core.delivery_queue WHERE bot_id=$1 AND chat='101'`, f.b.Delivery.BotID).
			Scan(&queue),
	)
	t.Logf("Alice lane after domain dispatch and Bot pump: %s", queue)
	var intents json.RawMessage
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(jsonb_build_object('state',state,'family',reference->>'family','continuation_done',continuation_done)),'[]'::jsonb) FROM bot.delivery_intents WHERE bot_id=$1 AND owner='alice'`, f.b.Delivery.BotID).
			Scan(&intents),
	)
	t.Logf("Alice intent outcomes: %s", intents)
	deliverScriptPassCards(t, f)
	require.Contains(t, passMenuCard(t, f, 101).Text, "150")
}

func TestScriptPassPaymentCompletionDiagnostic(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"accept", "reject"} {
		t.Run(action, func(t *testing.T) { t.Parallel(); verifyPaymentCompletion(t, action) })
	}
}

func paymentAdmission(t *testing.T, f *fixture, update int64) agenthost.ScriptToolRecord {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	for _, record := range records {
		for _, call := range record.Calls {
			if call.Pass != nil && call.Pass.Command != nil {
				return call
			}
		}
	}
	t.Fatal("payment admission missing")
	return agenthost.ScriptToolRecord{}
}

func verifyPaymentCompletion(t *testing.T, action string) {
	t.Helper()
	f := passMenuFixture(t)
	service, _, proof := seedScriptPassPayment(t, f)
	model := capturePassCompletion(
		t,
		f,
		59600,
		"Review Alice payment",
		fmt.Sprintf(
			`await tools.passes.payments.review({event:"dance"});return tools.passes.payments.%s({event:"dance",target:"alice"});`,
			action,
		),
	)
	require.Len(t, model.inputs, 2)
	decision := requirePaymentReceipt(t, f, proof, 59600, action, model, 1)
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, decision, payment.Decision)
}

// The ordinary flow reviews in one run and decides in a later run of the same
// update, so the decision run captures the pending review as queue context.
func TestScriptPassPaymentCompletionAcrossRuns(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"accept", "reject"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service, _, proof := seedScriptPassPayment(t, f)
			model := capturePassCompletionRuns(
				t,
				f,
				59610,
				"Review Alice payment, then decide",
				`return tools.passes.payments.review({event:"dance"});`,
				fmt.Sprintf(`return tools.passes.payments.%s({event:"dance",target:"alice"});`, action),
			)
			require.Len(t, model.inputs, 3)
			var records []agenthost.ScriptRecord
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=59610 AND kind='script_runs'`).
					Scan(&records),
			)
			require.Len(t, records, 2)
			require.False(t, records[1].PassRedacted, "the decision run keeps its minimal receipt")
			require.Empty(t, records[1].PassContext, "no pending review context survives the transition")
			decision := requirePaymentReceipt(t, f, proof, 59610, action, model, 2)
			payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
			require.NoError(t, err)
			require.Equal(t, decision, payment.Decision)
		})
	}
}

// The payment commits, then its HTTP reply is lost. Completion must come from
// the canonical receipt probe; the command itself is dispatched exactly once.
func TestScriptPassPaymentCompletionLostReply(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service, _, proof := seedScriptPassPayment(t, f)
	transport := &paymentCommandLostReply{}
	f.b.Host.HTTP = &http.Client{Transport: transport}
	model := capturePassCompletion(
		t,
		f,
		59620,
		"Review Alice payment",
		`await tools.passes.payments.review({event:"dance"});return tools.passes.payments.accept({event:"dance",target:"alice"});`,
	)
	require.Equal(t, 1, transport.dispatched, "the committed payment is never dispatched again")
	require.True(t, transport.lost)
	require.Len(t, model.inputs, 2)
	decision := requirePaymentReceipt(t, f, proof, 59620, "accept", model, 1)
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, decision, payment.Decision)
}

type paymentCommandLostReply struct {
	dispatched int
	lost       bool
}

func (transport *paymentCommandLostReply) RoundTrip(request *http.Request) (*http.Response, error) {
	command := request.URL.Path == "/internal/derived/pass-actions"
	if command {
		transport.dispatched++
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil && command && !transport.lost {
		transport.lost = true
		_ = response.Body.Close()
		return nil, errors.New("synthetic lost payment reply")
	}
	return response, err
}

// requirePaymentReceipt checks the minimal ledger and model-visible receipt, the
// retired review payload and a single domain operation. It returns the decision.
func requirePaymentReceipt(
	t *testing.T,
	f *fixture,
	proof string,
	update int64,
	action string,
	model *knowledgeModel,
	input int,
) string {
	t.Helper()
	admitted := paymentAdmission(t, f, update)
	require.NotNil(t, admitted.Pass.Command)
	decision := "accepted"
	if action == "reject" {
		decision = "rejected"
	}
	expected, err := json.Marshal(
		map[string]any{
			"operation_id": admitted.Pass.ID,
			"complete":     true,
			"result":       map[string]string{"decision": decision},
		},
	)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(admitted.Outcome.Result))
	require.Len(t, admitted.ResultAuthorities, 1)
	authority := admitted.ResultAuthorities[0].Registration
	require.Equal(t, passbooking.ReadPrivileged, authority.Kind)
	require.Equal(t, "alice", authority.Owner)
	require.Equal(t, admitted.Pass.Command.TargetVersion+1, authority.Version)
	require.Empty(t, authority.PaymentAttempt)
	var reviewFound, receiptFound bool
	for _, run := range model.inputs[input].Script.Runs {
		for _, call := range run.Calls {
			switch call.Name {
			case "passes.payments.review":
				reviewFound = true
				require.Empty(t, call.Result)
				require.Equal(t, "source_changed", call.Error)
			case "passes.payments." + action:
				receiptFound = true
				require.JSONEq(t, string(expected), string(call.Result))
				require.Empty(t, call.Error)
			}
		}
	}
	require.True(t, reviewFound)
	require.True(t, receiptFound)
	visible, err := json.Marshal(model.inputs[input].Script)
	require.NoError(t, err)
	require.NotContains(t, string(visible), proof)
	require.NotContains(t, string(visible), admitted.Pass.Command.PaymentAttempt)
	digest := sha256.Sum256([]byte(admitted.Pass.Command.Key))
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations WHERE actor='bob' AND event_id='dance' AND key_hash=$1`, hex.EncodeToString(digest[:])).
			Scan(&count),
	)
	require.Equal(t, 1, count, "the payment command committed exactly once")
	return decision
}

func TestScriptPassExportCompletionDiagnostic(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	model := capturePassCompletion(
		t,
		f,
		59300,
		"Export passes and show tier status",
		`const tiers=await tools.passes.tiers({event:"dance"}); const a=await tools.passes.export({}); const b=await tools.passes.export({});return {tiers,a,b};`,
	)
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.NotEmpty(t, runs)
	require.Empty(t, runs[len(runs)-1].Error)
	var pair struct {
		A struct {
			ID          string `json:"operation_id"`
			Complete    bool   `json:"complete"`
			Interrupted bool   `json:"interrupted"`
			Result      struct {
				Delivered bool `json:"delivered"`
			} `json:"result"`
		} `json:"a"`
		B struct {
			ID       string `json:"operation_id"`
			Complete bool   `json:"complete"`
		} `json:"b"`
	}
	require.NoError(t, json.Unmarshal(runs[len(runs)-1].Result, &pair))
	require.NotEmpty(t, pair.A.ID)
	require.Equal(t, pair.A.ID, pair.B.ID)
	require.False(t, pair.A.Complete)
	require.False(t, pair.B.Complete)
	require.False(t, pair.A.Interrupted)
	require.False(t, pair.A.Result.Delivered)
	var pending, receipts int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.delivery_intents WHERE bot_id=$1 AND owner='bob' AND reference->>'family'='pass_export'`, f.b.Delivery.BotID).
			Scan(&pending),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=59300 AND kind='pass_export'`).
			Scan(&receipts),
	)
	captureExportBindings(t, f, 59300)
	t.Logf("export intents=%d receipts before dispatch=%d", pending, receipts)
	require.Zero(t, receipts)
	require.Equal(t, 1, pending)
	pumpBotDeliveries(t, f.b)
	resumed := runPassVM(
		t,
		f,
		59301,
		202,
		"Check export delivery",
		fmt.Sprintf(`return tools.passes.resume({operation_id:%q});`, pair.A.ID),
	)
	require.Empty(t, resumed.Error)
	var receipt struct {
		ID       string `json:"operation_id"`
		Complete bool   `json:"complete"`
		Result   struct {
			Delivered bool `json:"delivered"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(resumed.Result, &receipt))
	require.Equal(t, pair.A.ID, receipt.ID)
	require.True(t, receipt.Complete)
	require.True(t, receipt.Result.Delivered)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=59300 AND kind='pass_export'`).
			Scan(&receipts),
	)
	require.Equal(t, 1, receipts)
}

func captureExportBindings(t *testing.T, f *fixture, update int64) {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	for _, record := range records {
		for _, call := range record.Calls {
			if call.Pass == nil || call.Pass.Name != "passes.export" || call.Source == nil {
				continue
			}
			source, err := json.Marshal(call.Source)
			require.NoError(t, err)
			var same bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT reference->'source'=$2::jsonb FROM bot.delivery_intents WHERE bot_id=$1 AND owner='bob' AND reference->>'family'='pass_export'`, f.b.Delivery.BotID, source).
					Scan(&same),
			)
			t.Logf(
				"export call error=%q exact admitted source matches persisted document=%t authorities=%d",
				call.Outcome.Error,
				same,
				len(call.Source.Authorities),
			)
		}
	}
}

func TestScriptPassPaymentReceiptExactScope(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"original", "actor", "target", "attempt", "key", "role_revoked", "new_version", "recreated", "null_assignment", "causal", "foreign_causal"} {
		t.Run(name, func(t *testing.T) { t.Parallel(); verifyPaymentReceiptScope(t, name) })
	}
}

func verifyPaymentReceiptScope(t *testing.T, name string) {
	t.Helper()
	f := passMenuFixture(t)
	service, _, _ := seedScriptPassPayment(t, f)
	capturePassCompletion(
		t,
		f,
		59601,
		"Review Alice payment",
		`await tools.passes.payments.review({event:"dance"});return tools.passes.payments.accept({event:"dance",target:"alice"});`,
	)
	admitted := paymentAdmission(t, f, 59601)
	require.NotNil(t, admitted.Source)
	source := admitted.Source.Clone()
	if name == "causal" || name == "foreign_causal" {
		canonical, err := (derivedmutation.Service{DB: f.db, Registration: service}).PassPaymentReceipt(
			t.Context(),
			"bob",
			*admitted.Pass.Command,
			source,
		)
		require.NoError(t, err)
		require.True(t, canonical.Found)
		causalActor := "bob"
		if name == "foreign_causal" {
			causalActor = "alice"
		}
		source.Authorities = []readsource.Authority{
			{
				Causal: &readsource.CausalSource{
					Actor:       causalActor,
					Generation:  source.Generation,
					Authorities: readsource.Registration([]passbooking.ReadAuthority{canonical.Before}),
				},
			},
		}
	}
	command := *admitted.Pass.Command
	actor, code := changePaymentReceiptScope(t, f, name, &command)
	receipt, err := (derivedmutation.Service{DB: f.db, Registration: service}).PassPaymentReceipt(
		t.Context(),
		actor,
		command,
		source,
	)
	if code != "" {
		var problem *core.ProblemError
		require.ErrorAs(t, err, &problem)
		require.Equal(t, code, problem.Code)
		return
	}
	require.NoError(t, err)
	require.Equal(t, name == "original" || name == "causal", receipt.Found)
	if receipt.Found {
		require.Equal(t, "accepted", receipt.Decision)
		second, repeatErr := (derivedmutation.Service{DB: f.db, Registration: service}).PassPaymentReceipt(
			t.Context(),
			actor,
			command,
			source,
		)
		require.NoError(t, repeatErr)
		require.Equal(t, receipt, second)
	}
}

func changePaymentReceiptScope(t *testing.T, f *fixture, name string, command *passbooking.Command) (string, string) {
	t.Helper()
	actor, code, mutation := "bob", "", ""
	switch name {
	case "foreign_causal":
		code = "invalid_derivation"
	case "actor":
		actor, code = "alice", "forbidden"
	case "target":
		command.Target = "bob"
		code = "idempotency_conflict"
	case "attempt":
		command.PaymentAttempt = strings.Repeat("f", 64)
		code = "idempotency_conflict"
	case "key":
		command.Key += "-other"
	case "role_revoked":
		mutation = `DELETE FROM core.pass_payment_admins WHERE owner='bob'`
		code = "forbidden"
	case "new_version":
		mutation = `UPDATE core.pass_bookings SET version=version+1 WHERE owner='alice' AND event_id='dance'`
		code = "source_stale"
	case "recreated":
		mutation = `UPDATE core.pass_bookings SET created_at=created_at+interval '1 second' WHERE owner='alice' AND event_id='dance'`
		code = "invalid_derivation"
	case "null_assignment":
		// A waitlisted booking permits no assignment. Keep its version and payment
		// attachment unchanged so the receipt must reject the NULL assignment match.
		mutation = `UPDATE core.pass_bookings SET state='waitlist',assigned_at=NULL,price=NULL,tier_index=NULL WHERE owner='alice' AND event_id='dance'`
		code = "source_stale"
	}
	if mutation != "" {
		_, err := f.db.Exec(t.Context(), mutation)
		require.NoError(t, err)
	}
	return actor, code
}

type paymentCompletionTransport struct {
	before func()
	done   bool
}

func (transport *paymentCompletionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/internal/derived/pass-payments/receipt" && !transport.done {
		transport.done = true
		transport.before()
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestScriptPassPaymentCompletionRevocation(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service, _, proof := seedScriptPassPayment(t, f)
	var callsBeforeRevocation int
	transport := &paymentCompletionTransport{before: func() {
		model, ok := f.b.Model.(*knowledgeModel)
		require.True(t, ok)
		callsBeforeRevocation = len(model.inputs)
		_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
		require.NoError(t, err)
	}}
	f.b.Host.HTTP = &http.Client{Transport: transport}
	model := capturePassCompletion(
		t,
		f,
		59602,
		"Review Alice payment",
		`await tools.passes.payments.review({event:"dance"});return tools.passes.payments.accept({event:"dance",target:"alice"});`,
	)
	require.True(t, transport.done)
	require.Equal(t, 1, callsBeforeRevocation, "the initial input was authorized before revocation")
	require.Len(t, model.inputs, callsBeforeRevocation, "revocation prevents every subsequent model call")
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.Equal(t, "accepted", payment.Decision, "revocation does not undo the committed effect")
	var records []agenthost.ScriptRecord
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=59602 AND kind='script_runs'`).
			Scan(&records),
	)
	require.NotEmpty(t, records)
	for _, record := range records {
		require.True(t, record.PassRedacted)
		for _, call := range record.Calls {
			require.Empty(t, call.Outcome.Result)
		}
		visible, marshalErr := json.Marshal(record.Run)
		require.NoError(t, marshalErr)
		require.NotContains(t, string(visible), proof, "retired ledger output must omit private proof data")
	}
}
