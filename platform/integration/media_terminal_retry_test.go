package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Fail only the first receipt operation, optionally after Core committed it.
type mediaProofResponseFailure struct {
	afterCommit bool
	failed      atomic.Bool
}

func (transport *mediaProofResponseFailure) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost ||
		(request.URL.Path != "/v1/order-actions" && request.URL.Path != "/internal/derived/order-actions") ||
		!transport.failed.CompareAndSwap(false, true) {
		return http.DefaultTransport.RoundTrip(request)
	}
	if transport.afterCommit {
		response, err := http.DefaultTransport.RoundTrip(request)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"code":"temporarily_unavailable"}`)), Request: request}, nil
}

func expireTerminalMediaSource(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE bot.media_intake SET expires_at=now()-interval '1 second' WHERE id='tg-media-100'`,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`DELETE FROM core.media WHERE id=(SELECT attachment_id FROM bot.media_intake WHERE id='tg-media-100')`,
	)
	require.NoError(t, err)
}

func terminalReceiptPlan() agent.Plan {
	return agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
}

func assertTerminalReceipt(t *testing.T, f *fixture, order orders.Order, original []byte) {
	t.Helper()
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "proof", current.State)
	assert.Equal(t, order.Version+1, current.Version)
	proof, err := (orders.Service{DB: f.db}).OrderProof(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(original, proof.Body))
	var status, notice string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT status,notice FROM bot.media_intake WHERE id='tg-media-100'`).
			Scan(&status, &notice),
	)
	assert.Equal(t, "done", status)
	assert.Equal(t, "media.saved", notice)
	card := mediaReconcileCard(t, f, "tg-media-100")
	assert.Contains(t, card.Text, "Receipt submitted for review. See the order card for the current payment status.")
	assert.Empty(t, card.Markup.Rows)
	assert.Equal(t, 1, f.model.calls, "retry must not reinterpret the expired source")
}

func TestMediaTerminalRetryPreservesCompletedReceiptAfterExpiry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	order := intakeOrder(t, f, "first")
	photo, original := intakePhoto(t, f)
	f.model.plan = terminalReceiptPlan()
	f.b.API.HTTP = &http.Client{Transport: &mediaRetryTransport{
		path:   "/v1/order-events/" + order.EventID + "/orders/" + order.ID + "/payment-instructions",
		status: http.StatusServiceUnavailable, response: `{"code":"temporarily_unavailable"}`,
	}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), photo))
	require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, "tg-media-100"))
	transport := &mediaSavedDeliveryFailure{}
	transport.enabled.Store(true)
	f.b.TG.HTTP = &http.Client{Transport: transport}
	require.Error(t, f.b.Handle(t.Context(), photo))
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, "proof", current.State)
	card := mediaReconcileCard(t, f, "tg-media-100")
	expireTerminalMediaSource(t, f)
	transport.enabled.Store(false)
	handle(t, f.b, photo)
	assertTerminalReceipt(t, f, order, original)
	assert.Equal(t, card.ID, mediaReconcileCard(t, f, "tg-media-100").ID)
	require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, "tg-media-100"))
	assertTerminalReceipt(t, f, order, original)
	after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, current, after)
}

type mediaSavedDeliveryFailure struct{ enabled atomic.Bool }

func (transport *mediaSavedDeliveryFailure) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/editMessageText") || strings.HasSuffix(request.URL.Path, "/sendMessage") {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		var payload telegram.Send
		if err = json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if strings.Contains(payload.Text, "Receipt submitted for review.") && transport.enabled.Load() {
			const response = `{"ok":false,"error_code":503,"description":"delivery unavailable"}`
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestMediaTerminalRetryReplaysCommittedCommandWithoutSource(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	order := intakeOrder(t, f, "first")
	photo, original := intakePhoto(t, f)
	f.model.plan = terminalReceiptPlan()
	f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{afterCommit: true}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), photo), "lost response leaves the durable command for replay")
	before, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, "proof", before.State)
	expireTerminalMediaSource(t, f)
	handle(t, f.b, photo)
	handle(t, f.b, photo)
	assertTerminalReceipt(t, f, order, original)
	after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "idempotent replay must not create another payment attempt")
}

func TestMediaTerminalRetryChecksPersistedCommandVersion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	order := intakeOrder(t, f, "first")
	photo, _ := intakePhoto(t, f)
	f.model.plan = terminalReceiptPlan()
	f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), photo))
	edit := orderCommand("edit", order)
	edit.Choice = orderChoice("shuttle")
	changed, err := f.b.API.ExecuteOrder(t.Context(), "alice", edit)
	require.NoError(t, err)
	expireTerminalMediaSource(t, f)
	handle(t, f.b, photo)
	after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, changed, after)
	assert.Equal(t, "unpaid", after.State)
	card := mediaReconcileCard(t, f, "tg-media-100")
	assert.Contains(t, card.Text, "The order or your access changed.")
	assert.Empty(t, card.Markup.Rows)
	assert.Equal(t, 1, f.model.calls)
}

func TestMediaDurableReceiptSurvivesExpiryAcrossEntryPoints(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"upload", "callback", "text", "reconcile"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
			require.NoError(t, err)
			order := intakeOrder(t, f, "first")
			photo, original := intakePhoto(t, f)
			f.model.plan = agent.Plan{View: agent.MediaView, Text: "Choose the attachment purpose."}
			handle(t, f.b, photo)
			handle(t, f.b, proofMediaClick(t, f, 101, "This is a receipt"))
			selection := intakeChoice(t, f, 102, order.ID)
			operation := selection
			if entry == "text" {
				f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
					MediaID: "tg-media-100", Intent: "receipt", OrderID: order.ID}}
				operation = message(102, 101, "Use order "+order.ID)
			}
			f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{afterCommit: true}}
			f.b.Host.HTTP = f.b.API.HTTP
			require.Error(t, f.b.Handle(t.Context(), operation))
			before, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			require.Equal(t, "proof", before.State)
			beforeCard := mediaReconcileCard(t, f, "tg-media-100")
			calls := f.model.calls
			expireTerminalMediaSource(t, f)
			switch entry {
			case "upload":
				handle(t, f.b, photo)
			case "callback", "text":
				handle(t, f.b, operation)
			case "reconcile":
				startMediaReconciler(t, f)
				require.Eventually(t, func() bool {
					card := mediaReconcileCard(t, f, "tg-media-100")
					return strings.Contains(card.Text, "Receipt submitted for review.")
				}, 5*time.Second, 20*time.Millisecond)
				handle(t, f.b, selection)
				handle(t, f.b, photo)
			}
			card := mediaReconcileCard(t, f, "tg-media-100")
			assert.Equal(t, beforeCard.ID, card.ID)
			assert.Contains(t, card.Text, "Receipt submitted for review.")
			assert.Empty(t, card.Markup.Rows)
			after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			proof, err := (orders.Service{DB: f.db}).OrderProof(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, original, proof.Body)
			assert.Equal(t, calls, f.model.calls)
		})
	}
}

func TestMediaExpiredUnselectedCannotStartReceipt(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"callback", "text"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
			require.NoError(t, err)
			order := intakeOrder(t, f, "first")
			photo, _ := intakePhoto(t, f)
			f.model.plan = agent.Plan{View: agent.MediaView, Text: "Choose the attachment purpose."}
			handle(t, f.b, photo)
			handle(t, f.b, proofMediaClick(t, f, 101, "This is a receipt"))
			operation := intakeChoice(t, f, 102, order.ID)
			if entry == "text" {
				f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
					MediaID: "tg-media-100", Intent: "receipt", OrderID: order.ID}}
				operation = message(102, 101, "Use order "+order.ID)
			}
			expireTerminalMediaSource(t, f)
			handle(t, f.b, operation)
			after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, order, after)
			card := mediaReconcileCard(t, f, "tg-media-100")
			assert.Contains(t, card.Text, "I cannot access or process this attachment.")
			assert.Empty(t, card.Markup.Rows)
			var selected bool
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT command IS NOT NULL FROM bot.media_intake WHERE id='tg-media-100'`).
					Scan(&selected),
			)
			assert.False(t, selected, "expiry cannot create a new receipt operation")
		})
	}
}

func TestMediaCommittedReceiptRevocationReportsUnknownOutcome(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	order := intakeOrder(t, f, "first")
	photo, original := intakePhoto(t, f)
	f.model.plan = terminalReceiptPlan()
	f.b.API.HTTP = &http.Client{Transport: &mediaProofResponseFailure{afterCommit: true}}
	f.b.Host.HTTP = f.b.API.HTTP
	require.Error(t, f.b.Handle(t.Context(), photo))
	before, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	require.Equal(t, "proof", before.State)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	handle(t, f.b, photo)
	var outcome string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT notice FROM bot.media_intake WHERE id='tg-media-100'`).Scan(&outcome),
	)
	assert.Equal(t, "media.outcome_unknown", outcome)
	card := mediaReconcileCard(t, f, "tg-media-100")
	assert.Contains(t, card.Text, "I cannot verify whether this receipt was submitted")
	assert.Empty(t, card.Markup.Rows)
	proof, err := (orders.Service{DB: f.db}).OrderProof(t.Context(), "bob", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, original, proof.Body)
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "I will use the available history."}
	handle(t, f.b, message(101, 202, "What happened to Alice's receipt?"))
	require.NotNil(t, f.model.input.MediaContext)
	assert.Empty(t, f.model.input.MediaContext.Recent)
	assert.Nil(t, f.model.input.MediaContext.LatestKnownReceipt)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=true WHERE id='alice'`)
	require.NoError(t, err)
	after, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "denied replay must not mutate a committed receipt")
	handle(t, f.b, message(102, 101, "Was that receipt sent?"))
	require.NotNil(t, f.model.input.MediaContext)
	require.NotEmpty(t, f.model.input.MediaContext.Recent)
	assert.Equal(t, "media.outcome_unknown", f.model.input.MediaContext.Recent[0].Outcome)
	require.NotNil(t, f.model.input.MediaContext.LatestKnownReceipt)
	assert.Equal(t, order.ID, f.model.input.MediaContext.LatestKnownReceipt.OrderID)
	assert.Equal(t, "proof", f.model.input.MediaContext.LatestKnownReceipt.CurrentState)
}
