package integration_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestScriptPassAssignmentAndManualCard(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.pass_profiles SET legal_name='Test Name'`)
	require.NoError(t, err)
	run := runPassVM(t, f, 19400, 202, "Assign pass for 101 using profile", `
 const target=await tools.passes.admin.target({event:"dance",target:"101"});
 return tools.passes.admin.assign({event:"dance",target:target.admin_target.booking.owner,assignment:{create:true,from_profile:true,total_price:150}});`)
	require.Empty(t, run.Error)
	booking, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", booking.State)
	require.NotNil(t, booking.Price)
	assert.Equal(t, 150, *booking.Price)
	shown := runPassVM(
		t,
		f,
		19401,
		101,
		"Show pass payment",
		`return tools.passes.registration.show({event:"dance",view:"payment"});`,
	)
	require.Empty(t, shown.Error)
	deliverScriptPassCards(t, f)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "150")
}

func TestScriptPassBatchUncoupleAndCancel(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := passbooking.Service{DB: f.db}
	invite := bookingCommand("invite", "pair", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := service.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	accept := bookingCommand("accept", "join", passbooking.Booking{})
	accept.Target = "alice"
	accept.TargetVersion = alice.Version
	_, err = service.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	split := runPassVM(
		t,
		f,
		19500,
		202,
		"Uncouple 101",
		`return tools.passes.batch.uncouple({event:"dance",recipients:[101]});`,
	)
	require.Empty(t, split.Error)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Empty(t, alice.Partner)
	cancel := runPassVM(
		t,
		f,
		19501,
		202,
		"Cancel passes 101 and 202",
		`return tools.passes.batch.cancel({event:"dance",recipients:[101,202]});`,
	)
	require.Empty(t, cancel.Error)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	guessed := runPassVM(
		t,
		f,
		19502,
		202,
		"Cancel another person",
		`return tools.passes.batch.cancel({event:"dance",recipients:[303]});`,
	)
	assert.NotEmpty(t, guessed.Error)
}

type passActionTransport struct {
	before func()
	done   bool
}

func (transport *passActionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if (request.URL.Path == "/v1/passes/actions" || request.URL.Path == "/internal/derived/pass-actions") &&
		!transport.done {
		transport.done = true
		transport.before()
	}
	return http.DefaultTransport.RoundTrip(request)
}

func seedScriptPassPayment(t *testing.T, f *fixture) (passbooking.Service, passbooking.Booking, string) {
	t.Helper()
	service := passbooking.Service{DB: f.db}
	// The importer and host upload path own proof identity; scripts only observe
	// the resulting current payment attempt through the scoped queue.
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index)
 VALUES('dance','alice',1,'assigned','leader','solo','bob',now(),now(),100,0)`,
	)
	require.NoError(t, err)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	proof, err := (orders.Service{DB: f.db}).UploadProof(
		t.Context(),
		"alice",
		"receipt.txt",
		[]byte("local synthetic pass receipt"),
	)
	require.NoError(t, err)
	submit := bookingCommand("proof", "initial-proof", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	return service, alice, proof.ID
}

func TestScriptPassPaymentGenerationAndExecutionACL(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"accept", "reject", "replaced", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service, alice, proof := seedScriptPassPayment(t, f)
			if scenario == "replaced" || scenario == "revoked" {
				f.b.API.HTTP = &http.Client{Transport: &passActionTransport{before: func() {
					if scenario == "revoked" {
						_, err := f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
						require.NoError(t, err)
						return
					}
					payment, err := service.Payment(t.Context(), "bob", "dance", "alice")
					require.NoError(t, err)
					reject := bookingCommand("proof_reject", "replace-reject", passbooking.Booking{})
					reject.Target = "alice"
					reject.TargetVersion = alice.Version
					reject.PaymentAttempt = payment.Attempt
					_, err = service.Execute(t.Context(), "bob", reject)
					require.NoError(t, err)
					current, err := service.Get(t.Context(), "alice", "dance")
					require.NoError(t, err)
					submit := bookingCommand("proof", "replacement", current)
					submit.ProofID = proof
					_, err = service.Execute(t.Context(), "alice", submit)
					require.NoError(t, err)
				}}}
				f.b.Host.HTTP = f.b.API.HTTP
			}
			action := "accept"
			if scenario == "reject" {
				action = "reject"
			}
			model := capturePassCompletion(
				t,
				f,
				19600,
				"Review Alice payment",
				`await tools.passes.payments.review({event:"dance"});return tools.passes.payments.`+action+`({event:"dance",target:"alice"});`,
			)
			payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
			require.NoError(t, err)
			if scenario == "replaced" || scenario == "revoked" {
				require.Len(t, model.inputs, 1, "stale authority must prevent subsequent model calls")
				assertRetiredPassPayment(t, f, 19600)
				assert.Equal(t, "pending", payment.Decision)
			} else {
				require.Len(t, model.inputs, 2)
				runs := model.inputs[1].Script.Runs
				require.NotEmpty(t, runs)
				run := runs[len(runs)-1]
				require.Empty(t, run.Error)
				assert.Equal(t, action+"ed", payment.Decision)
			}
		})
	}
}

func assertRetiredPassPayment(t *testing.T, f *fixture, updateID int64) {
	t.Helper()
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`, updateID).Scan(&records))
	require.NotEmpty(t, records)
	for _, record := range records {
		require.True(t, record.PassRedacted)
		require.NotEmpty(t, record.Run.Error)
		for _, call := range record.Calls {
			require.Empty(t, call.Outcome.Result, "retired payment evidence must be scrubbed")
		}
	}
}

func TestScriptPassRejectsModelAuthority(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	run := runPassVM(t, f, 19700, 101, "Invite 202", `
 await tools.passes.registration.read({event:"dance",view:"home"});
 const rejected=[];
 for(const field of ["actor","owner","version","key","proof_id","queue_invitation"]){
  try{await tools.passes.registration.invite({event:"dance",invite_telegram_id:202,[field]:"invented"});rejected.push(false);}
  catch(_){rejected.push(true);}
 }
 return rejected;`)
	require.Empty(t, run.Error)
	assert.JSONEq(t, `[true,true,true,true,true,true]`, string(run.Result))
	booking, err := (passbooking.Service{DB: f.db}).Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Zero(t, booking.Version)
}

// Drive each real shared-queue owner. A failed attempt is not retried here.
func deliverScriptPassCards(t *testing.T, f *fixture) {
	t.Helper()
	for range 20 {
		pumpBotDeliveries(t, f.b)
		delay := max(f.b.Delivery.BotInterval, f.b.Delivery.ChatInterval)
		require.LessOrEqual(t, delay, 100*time.Millisecond)
		select {
		case <-time.After(delay + time.Millisecond):
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
		dispatched := false
		for _, entry := range botDeliveryCandidates(t, f.b) {
			if entry.Reference.Owner != delivery.Passes {
				continue
			}
			id, err := strconv.ParseInt(entry.Reference.Key, 10, 64)
			require.NoError(t, err)
			require.NoError(t, f.b.DeliverPassNotification(t.Context(), id))
			var state string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT state FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind='passes' AND owner_key=$2 AND effect_key=$3`, f.b.Delivery.BotID, entry.Reference.Key, entry.Reference.Effect).
					Scan(&state),
			)
			require.Contains(
				t,
				[]string{string(delivery.Succeeded), string(delivery.Cancelled)},
				state,
				"fixture must not hide a failed transport attempt",
			)
			dispatched = true
			break
		}
		if !dispatched {
			pumpBotDeliveries(t, f.b)
			return
		}
	}
	t.Fatal("pass delivery queue did not settle")
}
