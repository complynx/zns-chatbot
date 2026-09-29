package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			run := runPassVM(
				t,
				f,
				19600,
				202,
				"Review Alice payment",
				`await tools.passes.payments.review({event:"dance"});return tools.passes.payments.`+action+`({event:"dance",target:"alice"});`,
			)
			payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
			require.NoError(t, err)
			if scenario == "replaced" || scenario == "revoked" {
				assert.NotEmpty(t, run.Error)
				assert.Equal(t, "pending", payment.Decision)
			} else {
				require.Empty(t, run.Error)
				assert.Equal(t, action+"ed", payment.Decision)
			}
		})
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
