package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func expireAVReplaySource(t *testing.T, f *fixture, id string) {
	t.Helper()
	_, err := f.db.Exec(t.Context(), `UPDATE bot.media_intake SET expires_at=now()-interval '1 second' WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.media WHERE id=(SELECT attachment_id FROM bot.media_intake WHERE id=$1)`,
		id,
	)
	require.NoError(t, err)
}

func TestAVVoiceDurableCommandReplayAfterSourceExpiry(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"committed", "pending", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			order := intakeOrder(t, f, "first")
			worker := &avWorker{result: avResult(t, false)}
			worker.result.Transcript.Text = "Add shuttle to order " + order.ID
			f.b.AV = worker
			f.model.plan = agent.Plan{
				View:        agent.OrdersView,
				OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: order.ID, Extra: "shuttle"},
			}
			f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{afterCommit: mode == "committed"}}
			f.b.Host.HTTP = f.b.API.HTTP
			voice := avUpload(t, f, "voice")
			require.Error(t, f.b.Handle(t.Context(), voice))
			var status string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT status FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&status),
			)
			require.Equal(t, "new", status, "failure occurs before finishConsumedVoice")
			expireAVReplaySource(t, f, "tg-media-100")
			if mode == "revoked" {
				_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
				require.NoError(t, err)
			}
			handle(t, f.b, voice)
			handle(t, f.b, voice)
			var version int64
			var choice orders.Choice
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT version,choice FROM core.orders WHERE id=$1`, order.ID).
					Scan(&version, &choice),
			)
			if mode == "revoked" {
				assert.Equal(t, order.Version, version)
				assert.NotContains(t, choice.Extras, "shuttle")
			} else {
				assert.Equal(t, order.Version+1, version)
				assert.Contains(t, choice.Extras, "shuttle")
			}
			var replied bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner='alice' AND update_id=100 AND kind='orders_reply')`).
					Scan(&replied),
			)
			assert.True(t, replied, "saved command must finish its normal reply after source expiry")
			assert.Equal(t, 1, f.model.calls)
			assert.Equal(t, 1, worker.initial)
		})
	}
}

func receiptVoiceReplayFixture(t *testing.T) (*fixture, telegram.Update, orders.Order, []byte, *avWorker) {
	t.Helper()
	f := setup(t)
	intakeOrder(t, f, "first")
	intakeOrder(t, f, "second")
	photo, photoBytes := intakePhoto(t, f)
	f.model.plan = terminalReceiptPlan()
	handle(t, f.b, photo)
	var rendered agent.MediaHint
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT rendered FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&rendered),
	)
	require.GreaterOrEqual(t, len(rendered.Choices), 2)
	choice := rendered.Choices[0]
	order, err := f.b.API.Order(t.Context(), "alice", "sandbox-festival", choice.OrderID)
	require.NoError(t, err)
	worker := &avWorker{result: avResult(t, false)}
	worker.result.Transcript.Text = "Use the first displayed order for that receipt."
	f.b.AV = worker
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", OrderID: choice.OrderID},
	}
	voice := avUpload(t, f, "voice")
	voice.ID = 101
	return f, voice, order, photoBytes, worker
}

func TestAVVoiceReceiptReplayAfterSourceExpiry(t *testing.T) {
	t.Parallel()
	f, voice, order, photoBytes, worker := receiptVoiceReplayFixture(t)
	f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{afterCommit: true}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), voice))
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, "proof", current.State)
	expireAVReplaySource(t, f, "tg-media-101")
	expireAVReplaySource(t, f, "tg-media-100")
	handle(t, f.b, voice)
	handle(t, f.b, voice)
	var sourceStatus, targetStatus, notice string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT status FROM bot.media_intake WHERE id='tg-media-101'`).Scan(&sourceStatus),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT status,notice FROM bot.media_intake WHERE id='tg-media-100'`).
			Scan(&targetStatus, &notice),
	)
	assert.Equal(t, "done", sourceStatus)
	assert.Equal(t, "done", targetStatus)
	assert.Equal(t, "media.saved", notice)
	after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, current, after)
	proof, err := (orders.Service{DB: f.db}).OrderProof(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, photoBytes, proof.Body)
	assert.Equal(t, 2, f.model.calls)
	assert.Equal(t, 1, worker.initial)
}

func TestAVVoiceReplayDoesNotPromoteExpiredTarget(t *testing.T) {
	t.Parallel()
	f, voice, order, _, worker := receiptVoiceReplayFixture(t)
	var attachmentID string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT attachment_id FROM bot.media_intake WHERE id='tg-media-100'`).
			Scan(&attachmentID),
	)
	transport := &mediaRetryTransport{
		path:     "/v1/media/" + attachmentID + "/proof",
		status:   http.StatusServiceUnavailable,
		response: `{"code":"temporarily_unavailable"}`,
	}
	f.b.API.HTTP = &http.Client{Transport: transport}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), voice))
	expireAVReplaySource(t, f, "tg-media-100")
	expireAVReplaySource(t, f, "tg-media-101")
	handle(t, f.b, voice)
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order, current)
	assert.EqualValues(t, 1, transport.calls.Load(), "expired target must not be promoted from the saved proposal")
	var sourceAction string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT last_action FROM bot.media_intake WHERE id='tg-media-101'`).
			Scan(&sourceAction),
	)
	assert.Equal(t, "answer", sourceAction, "receipt failure is resolved through the saved voice plan")
	assert.Equal(t, 2, f.model.calls)
	assert.Equal(t, 1, worker.initial)
}

func TestAVVoiceReplayRequiresSourceBinding(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"media_id", "av_ids", "update_id", "owner"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			order := intakeOrder(t, f, "first")
			f.b.AV = &avWorker{result: avResult(t, false)}
			f.model.plan = agent.Plan{
				View:        agent.OrdersView,
				OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: order.ID, Extra: "shuttle"},
			}
			f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{}}
			f.b.Host.HTTP = f.b.API.HTTP
			voice := avUpload(t, f, "voice")
			require.Error(t, f.b.Handle(t.Context(), voice))
			query := `UPDATE interaction.saved_turns SET payload=jsonb_set(payload,'{media_id}','"other"') WHERE owner='alice' AND update_id=100`
			switch mode {
			case "av_ids":
				query = `UPDATE interaction.saved_turns SET payload=jsonb_set(payload,'{av_ids}','["other"]') WHERE owner='alice' AND update_id=100`
			case "update_id":
				query = `UPDATE bot.media_intake SET update_id=999 WHERE id='tg-media-100'`
			case "owner":
				query = `UPDATE bot.media_intake SET owner='bob' WHERE id='tg-media-100'`
			}
			_, err := f.db.Exec(t.Context(), query)
			require.NoError(t, err)
			expireAVReplaySource(t, f, "tg-media-100")
			if mode == "owner" {
				require.Error(t, f.b.Handle(t.Context(), voice))
			} else {
				handle(t, f.b, voice)
			}
			current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, order, current)
			assert.Equal(t, 1, f.model.calls)
		})
	}
}
