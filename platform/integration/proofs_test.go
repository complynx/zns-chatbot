package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestProofFilesAreOwnerBoundAndPrivate(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	body := []byte("%PDF-1.4 private receipt")
	proof, err := f.b.API.UploadProof(ctx, "alice", "чек.pdf", body)
	require.NoError(t, err)
	retry, err := f.b.API.UploadProof(ctx, "alice", "чек.pdf", body)
	require.NoError(t, err)
	assert.Equal(t, proof, retry)
	foreign, err := f.b.API.UploadProof(ctx, "bob", "чек.pdf", body)
	require.NoError(t, err)
	assert.NotEqual(t, proof.ID, foreign.ID)
	_, err = f.b.API.UploadProof(ctx, "visitor", "receipt.pdf", body)
	requireCode(t, err, "forbidden")
	for _, name := range []string{"", "../receipt.pdf", "bad\nname.pdf"} {
		_, err = f.b.API.UploadProof(ctx, "alice", name, body)
		requireCode(t, err, "invalid_proof")
	}
	order, err := f.b.API.ExecuteOrder(
		ctx,
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Key:     "create",
			Origin:  "manual",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	command := orderCommand("proof", order)
	command.ProofFile = foreign.ID
	_, err = f.b.API.ExecuteOrder(ctx, "alice", command)
	requireCode(t, err, "invalid_proof")
	command.ProofFile = proof.ID
	order, err = f.b.API.ExecuteOrder(ctx, "alice", command)
	require.NoError(t, err)
	service := orders.Service{DB: f.db}
	got, err := service.OrderProof(ctx, "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, body, got.Body)
	_, err = service.OrderProof(ctx, "visitor", order.EventID, order.ID)
	requireCode(t, err, "proof_not_found")
	_, err = service.OrderProof(ctx, "bob", order.EventID, order.ID)
	require.NoError(t, err, "event payment admin can inspect proof")
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		f.b.API.Base+"/v1/order-events/"+order.EventID+"/orders/"+order.ID+"/proof/file",
		nil,
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+f.b.Host.Signer.Token("bob"))
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "nosniff", response.Header.Get("X-Content-Type-Options"))
	downloaded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, body, downloaded)
	_, err = f.b.API.ExecuteOrder(ctx, "alice", orderCommand("cancel_proof", order))
	require.NoError(t, err)
	_, err = service.OrderProof(ctx, "bob", order.EventID, order.ID)
	requireCode(t, err, "proof_not_found")
}

func uploadTelegramDocument(t *testing.T, f *fixture, user int64) telegram.Update {
	t.Helper()
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		f.fake.URL+"/lab/document?filename=receipt.pdf&user="+strconv.FormatInt(user, 10),
		bytes.NewBufferString("%PDF-1.4 receipt"),
	)
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	response, err := f.fake.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	return update
}

// Select the newest attachment card, retaining the actual callback for stale tests.
func proofMediaClick(t *testing.T, f *fixture, update int64, label string) telegram.Update {
	t.Helper()
	messages := chatMessages(t, f, 101)
	for _, message := range slices.Backward(messages) {
		if !strings.HasPrefix(message.Text, "Attachment ") {
			continue
		}
		for _, row := range message.Markup.Rows {
			for _, button := range row {
				if strings.Contains(button.Text, label) {
					return telegram.Update{ID: update, Callback: &telegram.Callback{
						ID: strconv.FormatInt(update, 10), From: telegram.User{ID: 101},
						Data: button.Data, Message: message,
					}}
				}
			}
		}
	}
	t.Fatalf("missing media button %q: %+v", label, messages)
	return telegram.Update{}
}

func TestTelegramProofSelectionSubmissionReviewAndRetry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, localeErr := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, localeErr)
	handle(t, f.b, message(1, 101, "/orders"))
	handle(t, f.b, orderClick(t, f, 101, 2, "New order"))
	handle(t, f.b, orderClick(t, f, 101, 3, "Add: Preparty"))
	handle(t, f.b, orderClick(t, f, 101, 4, "Send receipt"))
	document := uploadTelegramDocument(t, f, 101)
	// The fake and this direct harness have independent update counters.
	document.ID = 100
	handle(t, f.b, document)
	handle(t, f.b, document)
	list, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "unpaid", list[0].State, "a proof hint does not consume an unsupported attachment")
	require.Nil(t, f.model.input.Attachment, "unsupported bytes are not a model image input")
	assert.Contains(t, f.model.input.Text, "unsupported visual format application/pdf")
	assert.Contains(t, f.model.input.Text, "bytes were not interpreted")
	assert.NotContains(t, f.model.input.Text, "%PDF-1.4 receipt")
	handle(t, f.b, proofMediaClick(t, f, 101, "This is a receipt"))
	selection := proofMediaClick(t, f, 102, list[0].ID)
	handle(t, f.b, selection)
	handle(t, f.b, selection)
	list, err = f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.Equal(t, "proof", list[0].State)
	assert.EqualValues(t, 3, list[0].Version)
	history, err := f.b.API.OrderHistory(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, history, 3)
	handle(t, f.b, orderClick(t, f, 101, 110, "Payment BE: Борис"))
	// The submitted Telegram file can change; review must still use Core's bytes.
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE bot.fake_files SET body=$2 WHERE id=$1`,
		document.Message.Document.FileID,
		[]byte("replaced receipt"),
	)
	require.NoError(t, err)
	handle(t, f.b, message(111, 202, "/orders"))
	handle(t, f.b, orderClick(t, f, 202, 112, "Открыть чек"))
	var forwarded *telegram.Document
	for _, message := range chatMessages(t, f, 202) {
		if message.Document != nil {
			forwarded = message.Document
		}
	}
	require.NotNil(t, forwarded)
	assert.NotEqual(t, document.Message.Document.FileID, forwarded.FileID)
	body, err := f.b.TG.Download(t.Context(), *forwarded)
	require.NoError(t, err)
	assert.Equal(t, []byte("%PDF-1.4 receipt"), body)
	_, err = f.db.Exec(t.Context(), `DELETE FROM bot.fake_files WHERE id=$1`, document.Message.Document.FileID)
	require.NoError(t, err)
	handle(t, f.b, orderClick(t, f, 202, 113, "Открыть чек"))
	for _, message := range chatMessages(t, f, 202) {
		if message.Document != nil {
			forwarded = message.Document
		}
	}
	body, err = f.b.TG.Download(t.Context(), *forwarded)
	require.NoError(t, err)
	assert.Equal(t, []byte("%PDF-1.4 receipt"), body, "deleting source file cannot break receipt review")
	handle(t, f.b, orderClick(t, f, 202, 114, "Отклонить оплату"))
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	handle(t, f.b, orderClick(t, f, 101, 115, "Send receipt"))
	pending := uploadTelegramDocument(t, f, 101)
	pending.ID = 116
	handle(t, f.b, pending)
	handle(t, f.b, proofMediaClick(t, f, 117, "This is a receipt"))
	stale := proofMediaClick(t, f, 119, list[0].ID)
	handle(t, f.b, orderClick(t, f, 101, 118, "Add: Shuttle"))
	before, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	handle(t, f.b, stale)
	var staleText string
	for _, message := range chatMessages(t, f, 101) {
		if message.ID == stale.Callback.Message.ID {
			staleText = message.Text
		}
	}
	assert.Contains(t, staleText, "The order or your access changed. Select a current order or send the file again.")
	list, err = f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.Equal(t, "unpaid", list[0].State)
	assert.Equal(t, before, list, "stale media choice cannot mutate the order")
}
