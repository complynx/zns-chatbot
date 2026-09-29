package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Both lanes are intercepted so this regression also reaches the old bypass.
// The request can only reach this boundary after saved-plan prechecks succeed.
type savedMutationBarrier struct {
	entered chan string
	release chan struct{}
}

func (barrier *savedMutationBarrier) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		switch request.URL.Path {
		case "/v1/order-actions", "/v1/actions", "/internal/derived/order-actions", "/internal/derived/actions",
			"/v1/passes/actions", "/v1/me/pass-profile/actions", "/internal/derived/pass-actions", "/internal/derived/pass-profiles":
			select {
			case barrier.entered <- request.URL.Path:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			select {
			case <-barrier.release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestSavedPlanMutationRevocationAfterPrecheck(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "workflow", "registration", "profile"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			_, err := f.db.Exec(
				ctx,
				`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
			)
			require.NoError(t, err)
			plan := interaction.SavedPlan{
				PassAuthority: &interaction.PlanAuthority{
					Reads: []interaction.PassContextDependency{},
					ReadAuthorities: []readsource.Authority{
						{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
					},
				},
			}
			switch domain {
			case "orders":
				generation := int64(0)
				plan.Plan.View = agent.OrdersView
				plan.OrderCommand = &orders.Command{EventID: "sandbox-festival", Name: "create", Origin: "agent",
					HistoryGeneration: &generation, Choice: &orders.ChoiceInput{Customer: "saved source"}}
			case "registration":
				command := bookingCommand("solo", "", passbooking.Booking{})
				plan.RegistrationCommand = &command
				plan.Plan.View = agent.RegistrationView
			case "profile":
				plan.ProfileCommand = &passes.Command{
					Name:   "set",
					Field:  "legal_name",
					Value:  "Saved source",
					Origin: "agent",
				}
				plan.Plan.View = agent.ProfilesView
			default:
				plan.Plan = agent.Plan{View: "workflow", Action: &agent.Proposal{Name: "select", SlotID: "massage-1"}}
			}
			plan.BindKind()
			store := interaction.Store{DB: f.db}
			winner, err := store.SaveWinner(ctx, "alice", 88101, plan)
			require.NoError(t, err)
			// A concurrent replanner cannot replace the original source with an empty set.
			plan.PassAuthority.ReadAuthorities = []readsource.Authority{}
			reloaded, err := store.SaveWinner(ctx, "alice", 88101, plan)
			require.NoError(t, err)
			require.Equal(t, winner.PassAuthority, reloaded.PassAuthority)
			barrier := &savedMutationBarrier{entered: make(chan string, 1), release: make(chan struct{})}
			f.b.API.HTTP = &http.Client{Transport: barrier}
			f.b.Host.HTTP = f.b.API.HTTP
			done := make(chan error, 1)
			go func() { done <- f.b.Handle(ctx, message(88101, 101, "resume saved choice")) }()
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal("saved mutation did not reach the execution boundary", ctx.Err())
			}
			_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
			require.NoError(t, err)
			close(barrier.release)
			select {
			case err = <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("saved mutation did not finish", ctx.Err())
			}
			require.Zero(t, f.model.calls, "resume must use the saved command and source")
			var count int
			switch domain {
			case "orders":
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&count),
				)
				require.Zero(t, count, "revoked source must not create an order")
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.order_operations WHERE actor='alice' AND key='tg-order-88101'`).
						Scan(&count),
				)
			case "registration":
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_bookings WHERE owner='alice'`).Scan(&count),
				)
				require.Zero(t, count, "revoked source must not create a booking")
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice'`).
						Scan(&count),
				)
			case "profile":
				current, getErr := (passes.Service{DB: f.db}).Get(ctx, "alice")
				require.NoError(t, getErr)
				require.Empty(t, current.LegalName)
				require.Zero(t, current.Version)
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
						Scan(&count),
				)
			default:
				current, getErr := (workflow.Service{DB: f.db}).Current(ctx, "alice")
				require.NoError(t, getErr)
				require.Zero(t, current.Version, "revoked source must not select a workflow")
				require.NoError(
					t,
					f.db.QueryRow(ctx, `SELECT count(*) FROM core.operations WHERE owner='alice' AND key='tg-88101'`).
						Scan(&count),
				)
			}
			require.Zero(t, count, "refused effects must not create committed receipts")
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT count(*) FROM bot.interactions WHERE owner='alice' AND update_id=88101 AND ((kind IN ('result','order_error','profile_action') AND content::text LIKE '%source_stale%') OR (kind='registration_action' AND content->>'committed'='false'))`).
					Scan(&count),
			)
			require.Equal(t, 1, count, "expected refusal remains durable")
		})
	}
}
