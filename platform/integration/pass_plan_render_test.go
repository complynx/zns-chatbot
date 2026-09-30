package integration_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestPassPlanRenderViews(t *testing.T) {
	t.Parallel()
	for _, view := range []string{"workflow", agent.OrdersView, agent.ProfilesView, agent.KnowledgeView, agent.RegistrationView, agent.MediaView} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			f := latePlanFixture(t)
			if view == agent.MediaView {
				_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='alice'`)
				require.NoError(t, err)
				photo, _ := intakePhoto(t, f)
				f.model.plan = agent.Plan{View: agent.MediaView, Text: "What should I do with this image?"}
				handleVisible(t, f.b, photo)
			}
			latePlanModel(t, f, "script", func(agent.Input) (agent.Plan, error) {
				plan := agent.Plan{View: view, Text: latePlanSecret}
				if view == agent.MediaView {
					plan.MediaAction = &agent.MediaProposal{MediaID: "tg-media-100", Intent: "clarify"}
				}
				return plan, nil
			})
			handleVisible(t, f.b, message(46995, 101, "Read my private registration"))
			cards, err := json.Marshal(chatMessages(t, f, 101))
			require.NoError(t, err)
			require.Contains(t, string(cards), latePlanSecret)
			removeArchivedBooking(t, f)
			require.NoError(t, renderPassPlanView(t.Context(), f, view))
			pumpBotDeliveries(t, f.b)
			cards, err = json.Marshal(chatMessages(t, f, 101))
			require.NoError(t, err)
			require.NotContains(t, string(cards), latePlanSecret)
		})
	}
}

func renderPassPlanView(ctx context.Context, f *fixture, view string) error {
	switch view {
	case agent.OrdersView:
		return f.b.RenderOrders(ctx, "alice", 101)
	case agent.ProfilesView:
		return f.b.RenderProfile(ctx, "alice", 101)
	case agent.KnowledgeView:
		return f.b.RenderKnowledge(ctx, "alice", 101)
	case agent.RegistrationView:
		return f.b.RenderPassMenu(ctx, "alice", 101, "")
	case agent.MediaView:
		return f.b.RenderMedia(ctx, "alice", 101, "tg-media-100")
	default:
		return f.b.Render(ctx, "alice", 101)
	}
}

func TestPassPlanMissingSavedProvenance(t *testing.T) {
	t.Parallel()
	for _, missingOrigin := range []bool{false, true} {
		t.Run(strconv.FormatBool(missingOrigin), func(t *testing.T) {
			t.Parallel()
			f := latePlanFixture(t)
			calls := latePlanModel(t, f, "script", func(agent.Input) (agent.Plan, error) {
				return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
			})
			update := message(46996, 101, "Read my private registration")
			handleVisible(t, f.b, update)
			require.Contains(t, workflowCard(t, f).Text, latePlanSecret)
			_, err := f.db.Exec(
				t.Context(),
				`DELETE FROM interaction.saved_turns WHERE owner='alice' AND update_id=46996`,
			)
			require.NoError(t, err)
			if missingOrigin {
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE bot.interactions SET native_markdown=false WHERE update_id=46996;
 DELETE FROM bot.interactions WHERE update_id=46996 AND kind='reply_origin'`,
				)
				require.NoError(t, err)
			}
			require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
			require.Equal(t, 2, *calls)
			require.NoError(t, f.b.Render(t.Context(), "alice", 101))
			pumpBotDeliveries(t, f.b)
			require.NotContains(t, workflowCard(t, f).Text, latePlanSecret)
		})
	}
}
func TestPassPlanArchiveBoundary(t *testing.T) {
	t.Parallel()
	f := latePlanFixture(t)
	// Input persistence follows model completion and precedes reply archival.
	// Revoke there so this probe still brackets archival before reply persistence.
	_, err := f.db.Exec(
		t.Context(),
		`CREATE FUNCTION bot.qa_revoke_before_archive() RETURNS trigger LANGUAGE plpgsql AS $$
    BEGIN
        IF NEW.kind='input' AND NEW.update_id=46997 THEN
            DELETE FROM core.legacy_pass_payment_metadata WHERE event_id='archive';
            DELETE FROM core.pass_bookings WHERE event_id='archive' AND owner='alice';
        END IF;
        RETURN NEW;
    END $$;
    CREATE TRIGGER qa_revoke_before_archive AFTER INSERT ON bot.interactions FOR EACH ROW EXECUTE FUNCTION bot.qa_revoke_before_archive()`,
	)
	require.NoError(t, err)
	calls := latePlanModel(t, f, "script", func(agent.Input) (agent.Plan, error) {
		return agent.Plan{View: "workflow", Text: latePlanSecret}, nil
	})
	update := message(46997, 101, "Read my private registration")
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
	assertPassPlanTerminal(t, f, update.ID)
	require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
	require.Equal(t, 2, *calls)
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	require.NotContains(t, workflowCard(t, f).Text, latePlanSecret)
}

func TestPassPlanMissingSavedSystemNotice(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Model = &unavailableWorkflowModel{}
	handleVisible(t, f.b, message(46998, 101, "help"))
	notice := workflowCard(t, f).Text
	_, err := f.db.Exec(t.Context(), `DELETE FROM interaction.saved_turns WHERE owner='alice' AND update_id=46998`)
	require.NoError(t, err)
	require.NoError(t, f.b.Render(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	require.Equal(t, notice, workflowCard(t, f).Text)
}
