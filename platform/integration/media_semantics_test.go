package integration_test

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func unknownPurposeUpload(t *testing.T, f *fixture, id int64) telegram.Update {
	t.Helper()
	query := url.Values{"user": {"101"}, "filename": {"synthetic.txt"}, "caption": {"What can you do with this file?"}}
	response := sandboxMediaRequest(
		t, f, "/lab/document?"+query.Encode(), []byte("Synthetic text; this is not a payment receipt."),
	)
	defer response.Body.Close()
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	update.ID = id
	return update
}

func TestMediaUnknownPurposePreservesManualContinuation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	order := intakeOrder(t, f, "first")
	update := unknownPurposeUpload(t, f, 100)
	const answer = "I cannot inspect this text file. Tell me its purpose or choose a button."
	f.model.plan = agent.Plan{View: agent.MediaView, Text: answer,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "other"}}
	handle(t, f.b, update)
	card := mediaReconcileCard(t, f, "tg-media-100")
	require.Contains(t, card.Text, answer)
	require.NotEmpty(t, card.Markup.Rows)
	assert.Nil(t, f.model.input.Attachment, "unsupported bytes must not be interpreted as an image")
	handle(t, f.b, proofMediaClick(t, f, 101, "This is a receipt"))
	handle(t, f.b, intakeChoice(t, f, 102, order.ID))
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "proof", current.State)
}

func TestMediaExplicitDismissalRemainsTerminal(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"agent cancel", "Something else", "Cancel"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
			require.NoError(t, err)
			order := intakeOrder(t, f, "first")
			upload := unknownPurposeUpload(t, f, 100)
			f.model.plan = agent.Plan{View: agent.MediaView, Text: "What would you like to do?"}
			if action == "agent cancel" {
				upload.Message.Caption = "Discard this attachment."
				f.model.plan.MediaAction = &agent.MediaProposal{MediaID: "tg-media-100", Intent: "cancel"}
			}
			handle(t, f.b, upload)
			if action != "agent cancel" {
				handle(t, f.b, proofMediaClick(t, f, 101, action))
			}
			card := mediaReconcileCard(t, f, "tg-media-100")
			assert.Contains(t, card.Text, "Attachment request closed. No order was changed.")
			assert.Empty(t, card.Markup.Rows)
			current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, order, current)
		})
	}
}

func TestMediaLatestKnownReceiptExcludesNewUnpaidOrder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	receiptOrder := intakeOrder(t, f, "receipt")
	upload := unknownPurposeUpload(t, f, 100)
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "Please choose the purpose."}
	handle(t, f.b, upload)
	handle(t, f.b, proofMediaClick(t, f, 101, "This is a receipt"))
	handle(t, f.b, intakeChoice(t, f, 102, receiptOrder.ID))
	newer := intakeOrder(t, f, "newer-unpaid")
	unrelated := unknownPurposeUpload(t, f, 103)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-103", Intent: "cancel"},
	}
	unrelated.Message.Caption = "Discard this attachment."
	handle(t, f.b, unrelated)
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "I will use the committed receipt history."}
	handle(t, f.b, message(104, 101, "Мой последний чек уже подтверждён?"))
	require.NotNil(t, f.model.input.MediaContext)
	latest := f.model.input.MediaContext.LatestKnownReceipt
	require.NotNil(t, latest)
	assert.Equal(t, receiptOrder.ID, latest.OrderID)
	assert.NotEqual(t, newer.ID, latest.OrderID)
	assert.Equal(t, "manual", latest.Origin)
	assert.Equal(t, "proof", latest.CurrentState)
	assert.Equal(t, receiptOrder.Version+1, latest.SubmittedVersion)
	assert.False(t, latest.SubmittedAt.IsZero())
	current, err := f.b.API.Order(t.Context(), "alice", newer.EventID, newer.ID)
	require.NoError(t, err)
	assert.Equal(t, newer, current)
	submitted, err := f.b.API.Order(t.Context(), "alice", receiptOrder.EventID, receiptOrder.ID)
	require.NoError(t, err)
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orderCommand("cancel_proof", submitted))
	require.NoError(t, err)
	handle(t, f.b, message(105, 101, "What is the status of my last receipt now?"))
	latest = f.model.input.MediaContext.LatestKnownReceipt
	require.NotNil(t, latest)
	assert.Equal(t, receiptOrder.ID, latest.OrderID)
	assert.Equal(t, "unpaid", latest.CurrentState)
	handle(t, f.b, message(106, 202, "What was Alice's last receipt?"))
	require.NotNil(t, f.model.input.MediaContext)
	assert.Nil(t, f.model.input.MediaContext.LatestKnownReceipt)
}
