package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestMediaCommandRetainsSelectionTurnSourceThroughResume(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order := intakeOrder(t, f, "source-proof")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "What is this attachment for?"}
	handle(t, f.b, photo)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, err := f.db.Exec(
		ctx,
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
	)
	require.NoError(t, err)
	plan := interaction.SavedPlan{
		MediaID:              "tg-media-100",
		MediaResolvedOrder:   order.ID,
		MediaResolvedVersion: order.Version,
		Plan: agent.Plan{
			View:        agent.MediaView,
			MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt"},
		},
		PassAuthority: &interaction.PlanAuthority{
			Reads: []interaction.PassContextDependency{},
			ReadAuthorities: []readsource.Authority{
				{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
			},
		},
	}
	plan.BindKind()
	_, err = (interaction.Store{DB: f.db}).SaveWinner(ctx, "alice", 101, plan)
	require.NoError(t, err)
	barrier := &savedMutationBarrier{entered: make(chan string, 1), release: make(chan struct{})}
	f.b.API.HTTP = &http.Client{Transport: barrier}
	f.b.Host.HTTP = f.b.API.HTTP
	done := make(chan error, 1)
	go func() { done <- f.b.Handle(ctx, message(101, 101, "use this receipt")) }()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("media command did not reach the mutation boundary", ctx.Err())
	}
	var stored readsource.Derivation
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT command_source FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&stored),
	)
	require.Equal(
		t,
		plan.PassAuthority.ReadAuthorities,
		stored.Authorities,
		"selection source must not use upload turn 100",
	)
	require.NotNil(t, stored.Generation)
	require.Zero(t, *stored.Generation)
	_, err = f.db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
	require.NoError(t, err)
	close(barrier.release)
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("media command did not finish", ctx.Err())
	}
	current, err := f.b.API.Order(ctx, "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, order.Version, current.Version)
	require.Equal(t, "unpaid", current.State)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT count(*) FROM core.order_operations WHERE actor='alice' AND key='tg-media-100'`).
			Scan(&count),
	)
	require.Zero(t, count)
	handle(t, f.b, photo)
	var resumed readsource.Derivation
	require.NoError(
		t,
		f.db.QueryRow(ctx, `SELECT command_source FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&resumed),
	)
	require.Equal(t, stored, resumed, "upload retry must not replace the selection source")
	require.Equal(t, 1, f.model.calls, "saved selection and upload resume must not replan")
}
