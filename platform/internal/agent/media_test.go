package agent_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestMediaProposalValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		proposal agent.MediaProposal
		valid    bool
	}{
		{"receipt", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "80.00", Currency: "BYN"}, true},
		{"uncertain", agent.MediaProposal{MediaID: "image", Intent: "clarify"}, true},
		{"avatar", agent.MediaProposal{MediaID: "image", Intent: "avatar"}, true},
		{"registration", agent.MediaProposal{MediaID: "image", Intent: "receipt", RegistrationEvent: "dance"}, true},
		{"mixed_destination", agent.MediaProposal{MediaID: "image", Intent: "receipt", RegistrationEvent: "dance", OrderID: "order"}, false},
		{"avatar_registration", agent.MediaProposal{MediaID: "image", Intent: "avatar", RegistrationEvent: "dance"}, false},
		{"cancel", agent.MediaProposal{MediaID: "image", Intent: "cancel"}, true},
		{"cancel_order", agent.MediaProposal{MediaID: "image", Intent: "cancel", OrderID: "order"}, false},
		{"upper_bound", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "999999999.99", Currency: "RUB"}, true},
		{"currency", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "80", Currency: "USD"}, false},
		{"paid", agent.MediaProposal{MediaID: "image", Intent: "accept_payment"}, false},
		{"zero", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "0.00"}, false},
		{"negative", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "-1"}, false},
		{"precision", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "1.001"}, false},
		{"exponent", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "1e2"}, false},
		{"large", agent.MediaProposal{MediaID: "image", Intent: "receipt", Amount: "1000000000"}, false},
		{"empty_id", agent.MediaProposal{Intent: "receipt"}, false},
		{"long_id", agent.MediaProposal{MediaID: strings.Repeat("x", 129), Intent: "receipt"}, false},
		{"avatar_order", agent.MediaProposal{MediaID: "image", Intent: "avatar", OrderID: "order"}, false},
		{"inspect", agent.MediaProposal{MediaID: "video", Intent: "inspect_video", StartMS: 1000, EndMS: 240000, FrameCount: 8}, true},
		{"inspect_empty", agent.MediaProposal{MediaID: "video", Intent: "inspect_video", FrameCount: 1}, false},
		{"inspect_long", agent.MediaProposal{MediaID: "video", Intent: "inspect_video", EndMS: 240001, FrameCount: 1}, false},
		{"inspect_many", agent.MediaProposal{MediaID: "video", Intent: "inspect_video", EndMS: 2000, FrameCount: 9}, false},
		{"receipt_inspect", agent.MediaProposal{MediaID: "image", Intent: "receipt", EndMS: 2000}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := agent.Validate(agent.Plan{View: agent.MediaView, MediaAction: &tc.proposal})
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	for _, plan := range []agent.Plan{
		{View: "workflow"},
		{View: agent.MediaView, Action: &agent.Proposal{Name: "select"}},
		{View: agent.MediaView, OrderAction: &agent.OrderProposal{Name: "create"}},
		{View: agent.MediaView, ProfileAction: &agent.ProfileProposal{Name: "set", Field: "legal_name", Value: "John Smith"}},
	} {
		plan.MediaAction = &agent.MediaProposal{MediaID: "image", Intent: "receipt"}
		require.Error(t, agent.Validate(plan))
	}
}

func TestScriptedMediaIntent(t *testing.T) {
	t.Parallel()
	var fixture agent.ScriptedServer
	server := httptest.NewServer(fixture.Handler())
	defer server.Close()
	model := agent.Remote{URL: server.URL}
	pending := &agent.MediaContext{
		Pending:         []agent.MediaHint{{ID: "pending", Kind: "photo"}},
		SelectedOrderID: "selected",
	}
	for _, tc := range []struct {
		text, intent, id string
		image            bool
	}{
		{"receipt 80.00 BYN", "receipt", "current", true},
		{"avatar", "avatar", "current", true},
		{"", "clarify", "current", true},
		{"receipt 80.00 BYN", "receipt", "pending", false},
		{"What services are available?", "", "", false},
	} {
		in := agent.Input{Text: tc.text, Language: "en", MediaContext: pending}
		if tc.image {
			in.Attachment = &agent.Attachment{ID: "current", MIME: "image/png", Body: testImage()}
		}
		plan, err := model.Plan(t.Context(), in)
		require.NoError(t, err)
		if tc.intent == "" {
			assert.Nil(t, plan.MediaAction)
			continue
		}
		require.NotNil(t, plan.MediaAction)
		assert.Equal(t, tc.intent, plan.MediaAction.Intent)
		assert.Equal(t, tc.id, plan.MediaAction.MediaID)
		if tc.intent == "receipt" {
			assert.Equal(t, "80.00", plan.MediaAction.Amount)
		}
	}
}

func TestFrameTransportAndBounds(t *testing.T) {
	t.Parallel()
	model := attachmentModel{input: make(chan agent.Input, 1)}
	server := httptest.NewServer(agent.ModelHandler(model))
	defer server.Close()
	remote := agent.Remote{URL: server.URL}
	frame := agent.Attachment{ID: "video", MIME: "image/png", Body: testImage(), TimestampMS: 1200}
	in := agent.Input{Frames: []agent.Attachment{frame, frame}}
	_, err := remote.Plan(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, in.Frames, (<-model.input).Frames)
	assert.NotEmpty(t, in.Frames[0].Body)
	for _, invalid := range []agent.Input{
		{Attachment: &frame, Frames: []agent.Attachment{frame}},
		{Frames: make([]agent.Attachment, 25)},
		{Frames: []agent.Attachment{{MIME: "image/png", Body: testImage(), TimestampMS: -1}}},
		{Frames: []agent.Attachment{{MIME: "image/png", Body: testImage(), TimestampMS: 240001}}},
		{Frames: []agent.Attachment{{MIME: "image/png", Body: append(testImage(), make([]byte, 11<<20)...)}, {MIME: "image/png", Body: append(testImage(), make([]byte, 11<<20)...)}}},
	} {
		_, err = remote.Plan(t.Context(), invalid)
		require.Error(t, err)
	}
}
