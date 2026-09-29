package integration_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func intakePhoto(t *testing.T, f *fixture) (telegram.Update, []byte) {
	t.Helper()
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 12, 18))))
	response := sandboxMediaRequest(t, f, "/lab/photo?user=101&filename=synthetic.png", content.Bytes())
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	update.ID = 100
	return update, content.Bytes()
}

func intakeOrder(t *testing.T, f *fixture, key string) orders.Order {
	t.Helper()
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{EventID: "sandbox-festival",
		Name: "create", Key: key, Origin: "manual", Choice: orderChoice("preparty")})
	require.NoError(t, err)
	return order
}

func TestMediaIntakeReceiptReplayAndPrivateHistory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order := intakeOrder(t, f, "create")
	photo, body := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
	handle(t, f.b, photo)
	assert.Equal(t, body, f.model.input.Attachment.Body)
	assert.Equal(t, "tg-media-100", f.model.input.Attachment.ID)
	handle(t, f.b, photo)
	assert.Equal(t, 1, f.model.calls, "delivery retries reuse interpretation")
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "proof", current.State)
	assert.Equal(t, order.Version+1, current.Version)
	proof, err := (orders.Service{DB: f.db}).OrderProof(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, body, proof.Body)
	history, err := f.b.API.OrderHistory(t.Context(), "alice", order.EventID)
	require.NoError(t, err)
	assert.Equal(t, "agent", history[len(history)-1].Origin)
	f.model.plan = agent.Plan{View: "workflow", Text: "Four."}
	handle(t, f.b, message(101, 101, "What is two plus two?"))
	assert.Nil(t, f.model.input.Attachment, "unrelated requests do not receive image bytes")
}

func TestMediaIntakeAmbiguityInterruptionAndStaleChoice(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := intakeOrder(t, f, "first")
	second := intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"},
	}
	handle(t, f.b, photo)
	choice := intakeChoice(t, f, 101, first.ID)
	f.model.plan = agent.Plan{View: "workflow", Text: "Four."}
	handle(t, f.b, message(102, 101, "What is two plus two?"))
	for _, order := range []orders.Order{first, second} {
		current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
		require.NoError(t, err)
		assert.Equal(t, "unpaid", current.State)
	}
	command := orderCommand("edit", first)
	command.Choice = orderChoice("shuttle")
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	choice.ID = 103
	handle(t, f.b, choice)
	current, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, "unpaid", current.State)
	var status, notice string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT status,notice FROM bot.media_intake WHERE id='tg-media-100'`).
			Scan(&status, &notice),
	)
	assert.Equal(t, "done", status)
	assert.Equal(t, "media.stale", notice)
}

func TestMediaIntakeForeignProposalAndAvatarPreserveOrders(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order := intakeOrder(t, f, "first")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "avatar"},
	}
	handle(t, f.b, photo)
	var status, notice string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT status,notice FROM bot.media_intake WHERE id='tg-media-100'`).
			Scan(&status, &notice),
	)
	assert.Equal(t, "done", status)
	assert.Equal(t, "media.avatar_unavailable", notice)
	f.model.plan = agent.Plan{
		View:        agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", OrderID: order.ID},
	}
	handle(t, f.b, message(101, 202, "Use that receipt"))
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "unpaid", current.State)
}

func TestMediaIntakeClarificationContextAndManualHistory(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handle(t, f.b, message(1, 101, "/language en"))
	first := intakeOrder(t, f, "first")
	second := intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	const question = "Which order did you pay for?"
	f.model.plan = agent.Plan{View: agent.MediaView, Text: question,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
	handle(t, f.b, photo)
	selection := intakeChoice(t, f, 102, second.ID)
	f.model.plan = agent.Plan{View: "workflow", Text: "Four."}
	handle(t, f.b, message(101, 101, "What is two plus two?"))
	assert.Nil(t, f.model.input.Attachment)
	assert.Empty(t, f.model.input.Frames)
	require.NotNil(t, f.model.input.MediaContext)
	require.Len(t, f.model.input.MediaContext.Pending, 1)
	hint := f.model.input.MediaContext.Pending[0]
	assert.Equal(t, "tg-media-100", hint.ID)
	assert.Equal(t, "choose", hint.Kind)
	assert.Equal(t, "Select the order for this receipt. No payment has been approved.\n"+question, hint.Question)
	_, visibleQuestion, found := strings.Cut(selection.Callback.Message.Text, "\n")
	require.True(t, found)
	assert.Equal(t, visibleQuestion, hint.Question, "model receives the actual visible clarification")
	assertIntakeContextChoices(t, hint, selection.Callback.Message, first, second)
	handle(t, f.b, selection)
	handle(t, f.b, message(103, 101, "What did I just select?"))
	assert.Nil(t, f.model.input.Attachment, "history does not resend private image bytes")
	assert.Empty(t, f.model.input.Frames)
	require.NotNil(t, f.model.input.MediaContext)
	assert.Empty(t, f.model.input.MediaContext.Pending)
	assert.Equal(t, []agent.MediaEvent{
		{
			ID:      "tg-media-100",
			Status:  "done",
			Action:  "select_order",
			Origin:  "manual",
			OrderID: second.ID,
			Version: second.Version,
			Outcome: "media.saved",
		},
	}, f.model.input.MediaContext.Recent)
	current, err := f.b.API.Order(t.Context(), "alice", second.EventID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, "proof", current.State)
	assert.Equal(t, second.Version+1, current.Version)
	handle(t, f.b, message(104, 202, "What did Alice just select?"))
	require.NotNil(t, f.model.input.MediaContext)
	assert.Empty(t, f.model.input.MediaContext.Pending)
	assert.Empty(t, f.model.input.MediaContext.Recent)
	assert.Empty(t, f.model.input.MediaContext.Candidates)
	assert.Empty(t, f.model.input.MediaContext.SelectedOrderID)
	assert.Nil(t, f.model.input.Attachment)
	assert.Empty(t, f.model.input.Frames)
}

func TestMediaIntakeFollowupUsesLastDisplayedChoices(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	first := intakeOrder(t, f, "first")
	_ = intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
	handle(t, f.b, photo)
	old := intakeChoice(t, f, 101, first.ID)
	command := orderCommand("edit", first)
	command.Choice = orderChoice("shuttle")
	edited, err := f.b.API.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	_, err = f.b.API.SetLanguage(t.Context(), "alice", "ru", false)
	require.NoError(t, err)
	f.model.plan = agent.Plan{View: "workflow", Text: "Here are your current choices."}
	handle(t, f.b, message(102, 101, "Which choices are available?"))
	updated := intakeChoice(t, f, 103, first.ID)
	assert.Equal(t, old.Callback.Message.ID, updated.Callback.Message.ID)
	require.Len(t, f.model.input.MediaContext.Pending, 1)
	hint := f.model.input.MediaContext.Pending[0]
	_, question, found := strings.Cut(updated.Callback.Message.Text, "\n")
	require.True(t, found)
	assert.Equal(t, question, hint.Question)
	assert.Contains(t, hint.Question, "Select the order")
	var labels []string
	for _, row := range updated.Callback.Message.Markup.Rows {
		for _, button := range row {
			labels = append(labels, button.Text)
		}
	}
	var choices []string
	for _, choice := range hint.Choices {
		choices = append(choices, choice.Label)
		if choice.OrderID == first.ID {
			assert.Equal(t, first.Version, choice.Version)
		}
	}
	assert.ElementsMatch(t, labels, choices)
	assert.Contains(t, choices, "Cancel")
	f.model.plan = agent.Plan{View: agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", OrderID: first.ID}}
	handle(t, f.b, message(104, 101, "Use the first displayed choice."))
	current, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, edited, current, "semantic choice retains the displayed version and rejects stale intent")
}

func TestMediaIntakeOrdinalDoesNotShiftAfterOtherOrderDisappears(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := intakeOrder(t, f, "first")
	second := intakeOrder(t, f, "second")
	third := intakeOrder(t, f, "third")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView,
		MediaAction: &agent.MediaProposal{MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
	handle(t, f.b, photo)
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", orderCommand("delete", first))
	require.NoError(t, err)
	f.model.plan.MediaAction.OrderID = second.ID
	handle(t, f.b, message(101, 101, "Use the second displayed order."))
	require.Len(t, f.model.input.MediaContext.Pending, 1)
	choices := f.model.input.MediaContext.Pending[0].Choices
	require.GreaterOrEqual(t, len(choices), 3)
	assert.Equal(t, first.ID, choices[0].OrderID)
	assert.Equal(t, second.ID, choices[1].OrderID)
	assert.Equal(t, third.ID, choices[2].OrderID)
	selected, err := f.b.API.Order(t.Context(), "alice", second.EventID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, "proof", selected.State)
	untouched, err := f.b.API.Order(t.Context(), "alice", third.EventID, third.ID)
	require.NoError(t, err)
	assert.Equal(t, third, untouched)
}

func TestMediaIntakeNaturalChoiceUsesDisplayedOrder(t *testing.T) {
	t.Parallel()
	f := setup(t)
	first := intakeOrder(t, f, "first")
	second := intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN", OrderID: first.ID,
	}}
	handle(t, f.b, photo)
	before, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, "unpaid", before.State, "an initial model-selected ID cannot resolve equal prices")
	f.model.plan.MediaAction.OrderID = second.ID
	handle(t, f.b, message(101, 101, "Use the second displayed order for that receipt."))
	current, err := f.b.API.Order(t.Context(), "alice", second.EventID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, "proof", current.State)
	assert.Equal(t, second.Version+1, current.Version)
	unchanged, err := f.b.API.Order(t.Context(), "alice", first.EventID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, before, unchanged)
}

func assertIntakeContextChoices(t *testing.T, hint agent.MediaHint, card telegram.Message, first, second orders.Order) {
	t.Helper()
	var visibleLabels, contextLabels []string
	for _, row := range card.Markup.Rows {
		for _, button := range row {
			visibleLabels = append(visibleLabels, button.Text)
		}
	}
	for _, choice := range hint.Choices {
		contextLabels = append(contextLabels, choice.Label)
	}
	assert.ElementsMatch(t, visibleLabels, contextLabels)
	require.Len(t, hint.Choices, 5)
	for _, order := range []orders.Order{first, second} {
		var label string
		for _, visible := range visibleLabels {
			if strings.HasPrefix(visible, order.ID+" · ") {
				label = visible
			}
		}
		require.NotEmpty(t, label)
		assert.Contains(t, hint.Choices, agent.MediaChoice{Action: "order", OrderID: order.ID, Version: order.Version,
			Label: label})
	}
	assert.Contains(t, hint.Choices, agent.MediaChoice{Action: "avatar", Label: "Use as avatar"})
	assert.Contains(t, hint.Choices, agent.MediaChoice{Action: "other", Label: "Something else"})
	assert.Contains(t, hint.Choices, agent.MediaChoice{Action: "cancel", Label: "Cancel"})
}

func intakeChoice(t *testing.T, f *fixture, update int64, orderID string) telegram.Update {
	t.Helper()
	for _, message := range chatMessages(t, f, 101) {
		for _, row := range message.Markup.Rows {
			for _, button := range row {
				if strings.HasPrefix(button.Data, "media:") && strings.Contains(button.Text, orderID) {
					return telegram.Update{
						ID: update,
						Callback: &telegram.Callback{
							ID:      strconv.FormatInt(update, 10),
							From:    telegram.User{ID: 101},
							Data:    button.Data,
							Message: message,
						},
					}
				}
			}
		}
	}
	t.Fatalf("missing media order button %s", orderID)
	return telegram.Update{}
}
