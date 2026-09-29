package integration_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

type receiptProbeTransport struct {
	outage    bool
	probes    int
	mutations int
}

func (wire *receiptProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch request.URL.Path {
	case "/internal/derived/pass-profiles/receipt":
		wire.probes++
		if wire.outage {
			return nil, errors.New("receipt provider unavailable")
		}
	case "/internal/derived/pass-profiles":
		wire.mutations++
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestSavedPlanReceiptRecoveryRejectsUnsafeContinuation(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"absent_revoked_source", "hash_mismatch", "target_revoked", "receipt_outage"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			ctx := t.Context()
			source := registrationDerivation(t, f.db, "alice")
			command := passes.Command{
				Name:   "set",
				Field:  "legal_name",
				Value:  "Saved exact value",
				Origin: "agent",
				Key:    "tg-profile-88202",
			}
			profileService := passes.Service{DB: f.db}
			switch boundary {
			case "hash_mismatch", "target_revoked":
				seeded := command
				if boundary == "hash_mismatch" {
					seeded.Value = "Different original value"
				}
				_, err := profileService.Execute(ctx, "alice", seeded)
				require.NoError(t, err)
			}
			before, err := profileService.Get(ctx, "alice")
			require.NoError(t, err)
			plan := interaction.SavedPlan{
				Plan:           agent.Plan{View: agent.ProfilesView, Text: "Private model prose must not return"},
				ProfileCommand: &command,
				PassAuthority: &interaction.PlanAuthority{
					Reads:           []interaction.PassContextDependency{},
					ReadAuthorities: source.Authorities,
				},
			}
			plan.BindKind()
			_, err = (interaction.Store{DB: f.db}).SaveWinner(ctx, "alice", 88202, plan)
			require.NoError(t, err)
			switch boundary {
			case "absent_revoked_source":
				_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
			case "target_revoked":
				_, err = f.db.Exec(ctx, `UPDATE core.users SET can_book=false WHERE id='alice'`)
			}
			require.NoError(t, err)
			var receiptsBefore int
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
					Scan(&receiptsBefore),
			)
			wire := &receiptProbeTransport{outage: boundary == "receipt_outage"}
			f.b.Host.HTTP = &http.Client{Transport: wire}
			update := message(88202, 101, "apply saved profile")
			require.Error(t, f.b.Handle(ctx, update))
			require.Equal(t, 1, wire.probes)
			require.Zero(t, wire.mutations, "a rejected or unavailable probe cannot execute")
			var afterName string
			var afterVersion int64
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT legal_name,version FROM core.pass_profiles WHERE owner='alice'`).
					Scan(&afterName, &afterVersion),
			)
			require.Equal(t, before.LegalName, afterName)
			require.Equal(t, before.Version, afterVersion)
			var receiptsAfter, replies int
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT count(*) FROM core.pass_profile_operations WHERE owner='alice'`).
					Scan(&receiptsAfter),
			)
			require.Equal(t, receiptsBefore, receiptsAfter)
			require.NoError(
				t,
				f.db.QueryRow(ctx, `SELECT count(*) FROM bot.interactions WHERE owner='alice' AND update_id=88202 AND kind IN ('profile_reply','profile_answer')`).
					Scan(&replies),
			)
			require.Zero(t, replies, "no recovered body or success notice exists")
			if boundary == "receipt_outage" {
				saved, loadErr := (interaction.Store{DB: f.db}).Load(ctx, "alice", 88202)
				require.NoError(t, loadErr)
				require.Equal(t, interaction.Ready, saved.State)
				wire.outage = false
				require.NoError(
					t,
					f.b.Handle(ctx, update),
					"retry resumes original validated flow after absent receipt",
				)
				require.Equal(t, 1, wire.mutations)
			}
		})
	}
}
