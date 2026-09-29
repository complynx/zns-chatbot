package integration_test

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestSavedPlanRegistrationReviewPublication(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "100", Currency: "RUB"}}
	handle(t, f.b, photo)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{registrationRead("payment_queue"), {
		View:               agent.RegistrationView,
		RegistrationAction: &agent.RegistrationProposal{Name: "proof_reject", Event: "dance", Target: "alice"},
	}}}
	err := f.b.Handle(t.Context(), message(104, 202, "Reject Alice's Dance receipt"))
	payment, readErr := (passbooking.Service{DB: f.db}).Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, readErr)
	var committed bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner='bob' AND update_id=104 AND kind='registration_action' AND content->>'committed'='true')`).
			Scan(&committed),
	)
	t.Logf("payment decision=%s; durable committed outcome=%t; handle error=%v", payment.Decision, committed, err)
	require.Equal(t, "rejected", payment.Decision)
	require.True(t, committed)
	require.NoError(t, err, "a trusted completed command must render its current authorized outcome")
}

type savedMutationResponseLoss struct {
	path         string
	calls        atomic.Int64
	receiptCalls atomic.Int64
}

func (transport *savedMutationResponseLoss) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == transport.path+"/receipt" {
		transport.receiptCalls.Add(1)
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil && request.URL.Path == transport.path && request.Method == http.MethodPost &&
		transport.calls.Add(1) == 1 && response.StatusCode == http.StatusOK {
		_ = response.Body.Close()
		return nil, errors.New("saved mutation response lost after commit")
	}
	return response, err
}

func TestSavedPlanUnknownCommitRecoversAuthoritativeReceipt(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"own_booking_version", "source_revoked", "history_deleted"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			ctx := t.Context()
			plan := interaction.SavedPlan{PassAuthority: &interaction.PlanAuthority{
				Reads: []interaction.PassContextDependency{}, ReadAuthorities: []readsource.Authority{},
			}}
			path := "/internal/derived/pass-profiles"
			var historyID int64
			if boundary == "own_booking_version" {
				current, err := (passbooking.Service{DB: f.db}).Get(ctx, "alice", "dance")
				require.NoError(t, err)
				command := bookingCommand("cancel", "", current)
				plan.RegistrationCommand = &command
				plan.Plan.View = agent.RegistrationView
				plan.PassAuthority.ReadAuthorities = []readsource.Authority{{Registration: passbooking.ReadAuthority{
					Kind: passbooking.ReadOwnerBooking, Event: current.Event, Owner: current.Owner,
					Version: current.Version, CreatedAt: current.CreatedAt,
				}}}
				path = "/internal/derived/pass-actions"
			} else {
				plan.ProfileCommand = &passes.Command{
					Name:   "set",
					Field:  "legal_name",
					Value:  "Recovered private name",
					Origin: "agent",
				}
				plan.Plan.View = agent.ProfilesView
				if boundary == "source_revoked" {
					_, err := f.db.Exec(
						ctx,
						`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
					)
					require.NoError(t, err)
					plan.PassAuthority.ReadAuthorities = []readsource.Authority{
						{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
					}
				} else {
					plan.PassAuthority.PrivateHistory = true
					require.NoError(
						t,
						(conversation.Service{DB: f.db}).AppendOriginal(
							ctx,
							"alice",
							"recovery-source",
							"user",
							"deleted recovery source",
						),
					)
					require.NoError(
						t,
						f.db.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='recovery-source'`).
							Scan(&historyID),
					)
				}
			}
			plan.BindKind()
			_, err := (interaction.Store{DB: f.db}).SaveWinner(ctx, "alice", 88201, plan)
			require.NoError(t, err)
			transport := &savedMutationResponseLoss{path: path}
			f.b.API.HTTP = &http.Client{Transport: transport}
			f.b.Host.HTTP = f.b.API.HTTP
			update := message(88201, 101, "apply the saved choice")
			require.Error(t, f.b.Handle(ctx, update))
			require.EqualValues(
				t,
				1,
				transport.calls.Load(),
				"first mutation response must be lost at the intended boundary",
			)
			var before int
			if boundary == "own_booking_version" {
				current, readErr := (passbooking.Service{DB: f.db}).Get(ctx, "alice", "dance")
				require.NoError(t, readErr)
				require.Equal(t, "cancelled", current.State)
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).
						Scan(&before),
				)
			} else {
				current, readErr := (passes.Service{DB: f.db}).Get(ctx, "alice")
				require.NoError(t, readErr)
				require.Equal(t, "Recovered private name", current.LegalName)
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
						Scan(&before),
				)
			}
			switch boundary {
			case "source_revoked":
				_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
				require.NoError(t, err)
			case "history_deleted":
				require.NoError(t, (conversation.Service{DB: f.db}).DeleteContent(ctx, "alice", historyID))
			}
			retryErr := f.b.Handle(ctx, update)
			t.Logf(
				"boundary=%s; mutation requests=%d; existing receipts=%d; retry error=%v",
				boundary,
				transport.calls.Load(),
				before,
				retryErr,
			)
			require.NoError(t, retryErr)
			require.EqualValues(t, 1, transport.calls.Load(), "recovery never executes a new mutation")
			require.EqualValues(
				t,
				2,
				transport.receiptCalls.Load(),
				"absent then found receipts use the current-authorized typed domain",
			)
			var after int
			if boundary == "own_booking_version" {
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).
						Scan(&after),
				)
			} else {
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
						Scan(&after),
				)
			}
			require.Equal(t, before, after)
		})
	}
}
