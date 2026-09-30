package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func registrationPaymentFixture(t *testing.T) *fixture {
	t.Helper()
	f := passMenuFixture(t)
	command := bookingCommand("solo", "registration-payment", passbooking.Booking{})
	command.PaymentAdmin = "bob"
	booking, err := (passbooking.Service{DB: f.db, Delivery: f.b.Delivery}).Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Equal(t, "assigned", booking.State)
	return f
}

func TestRegistrationReceiptAgentAmountAndManualReview(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	photo, body := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "100", Currency: "RUB"}}
	handlePassVisible(t, f, photo)
	assert.Equal(t, body, f.model.input.Attachment.Body, "model receives the actual uploaded artwork")
	service := passbooking.Service{DB: f.db}
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "pending", payment.Decision)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "awaiting review")
	proof, err := f.b.API.DownloadPassProof(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, body, proof.Body)
	// Simulate a crash after the domain commit but before recording local completion.
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.media_intake SET status='choose',expires_at=now()-interval '1 day' WHERE id='tg-media-100'`,
	)
	require.NoError(t, err)
	handlePassVisible(t, f, photo)
	assert.Equal(t, 1, f.model.calls)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language='en' WHERE id='bob'`)
	require.NoError(t, err)
	handlePassVisible(t, f, message(201, 202, "/passes"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 202, "Dance"))
	handlePassVisible(t, f, passMenuClick(t, f, 202, 203, "Pass receipts to review"))
	accept := passMenuClick(t, f, 202, 204, "Accept payment · 101")
	foreign := accept
	copyCallback := *accept.Callback
	copyCallback.From.ID = 101
	foreign.Callback, foreign.ID = &copyCallback, 205
	handle(t, f.b, foreign)
	payment, err = service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "pending", payment.Decision)
	handlePassVisible(t, f, accept)
	payment, err = service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "accepted", payment.Decision)
	assert.NotContains(t, passMenuCard(t, f, 202).Text, "awaiting review")
	handlePassVisible(t, f, accept)
	var attempts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&attempts))
	assert.Equal(t, 1, attempts)
	restarted := *f.b
	require.NoError(t, restarted.RenderPassMenu(t.Context(), "alice", 101, ""))
	drainPassNotices(t, f)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "Payment accepted")
}

func TestRegistrationReceiptRussianSemanticChoiceAndAgentReview(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='alice'`)
	require.NoError(t, err)
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "Уточните назначение", MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt"}}
	handlePassVisible(t, f, photo)
	f.model.plan = agent.Plan{View: "workflow", Text: "Четыре."}
	handle(t, f.b, message(102, 101, "Сколько будет два плюс два?"))
	var attempts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&attempts))
	assert.Zero(t, attempts)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", RegistrationEvent: "dance"}}
	handlePassVisible(t, f, message(103, 101, "Первый вариант"))
	assert.Contains(t, passMenuCard(t, f, 101).Text, "ожидает проверки")
	service := passbooking.Service{DB: f.db}
	payment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "pending", payment.Decision)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{registrationRead("payment_queue"), {
		View:               agent.RegistrationView,
		RegistrationAction: &agent.RegistrationProposal{Name: "proof_reject", Event: "dance", Target: "alice"},
	}}}
	handle(t, f.b, message(104, 202, "Отклони чек Алисы за Танцы"))
	payment, err = service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "rejected", payment.Decision)
	require.NoError(t, f.b.RenderPassMenu(t.Context(), "alice", 101, ""))
	drainPassNotices(t, f)
	assert.Contains(t, passMenuCard(t, f, 101).Text, "Чек отклонён")
}

func TestRegistrationReceiptAmbiguousDestinationAndStaleChoice(t *testing.T) {
	t.Parallel()
	f := registrationPaymentFixture(t)
	order := intakeOrder(t, f, "competing-receipt")
	// Use the authoritative order quote as the pass price to force ambiguity.
	quote, err := f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET price=$1::numeric::integer WHERE owner='alice' AND event_id='dance'`,
		quote.TotalRUB,
	)
	require.NoError(t, err)
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: quote.TotalRUB, Currency: "RUB"}}
	handlePassVisible(t, f, photo)
	choice := orderClick(t, f, 101, 301, "Pass payment · Dance · 1050 RUB")
	service := passbooking.Service{DB: f.db}
	booking, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", bookingCommand("cancel", "cancel-before-receipt", booking))
	require.NoError(t, err)
	handle(t, f.b, choice)
	var attempts int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&attempts))
	assert.Zero(t, attempts)
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, "unpaid", current.State)
}
