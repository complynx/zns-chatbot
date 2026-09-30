package integration_test

import (
	"bytes"
	"context"
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
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func startMediaReconciler(t *testing.T, f *fixture) {
	t.Helper()
	// Photos were handled directly; the live poller must not replay the fake queue.
	_, err := f.db.Exec(t.Context(), `INSERT INTO bot.cursors(name,value) VALUES('telegram',1000)
 ON CONFLICT(name) DO UPDATE SET value=excluded.value`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case runErr := <-done:
			assert.NoError(t, runErr)
		case <-time.After(5 * time.Second):
			t.Error("media reconciler did not stop")
		}
	})
}

func mediaReconcileCard(t *testing.T, f *fixture, id string) telegram.Message {
	t.Helper()
	for _, message := range chatMessages(t, f, 101) {
		if strings.HasPrefix(message.Text, "Attachment "+id+"\n") ||
			strings.HasPrefix(message.Text, "Вложение "+id+"\n") {
			return message
		}
	}
	t.Fatalf("missing media card %s", id)
	return telegram.Message{}
}

func mediaReconcileButton(card telegram.Message, prefix string) telegram.Button {
	for _, row := range card.Markup.Rows {
		for _, button := range row {
			if strings.HasPrefix(button.Text, prefix) {
				return button
			}
		}
	}
	return telegram.Button{}
}

func TestMediaReconcileRefreshesChoicesAndLanguage(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	first := intakeOrder(t, f, "first")
	second := intakeOrder(t, f, "second")
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
		MediaID: "tg-media-100", Intent: "receipt", Amount: "35", Currency: "BYN"}}
	handleVisible(t, f.b, photo)
	old := intakeChoice(t, f, 101, first.ID)
	command := orderCommand("edit", first)
	command.Choice = orderChoice("shuttle")
	first, err = f.b.API.ExecuteOrder(t.Context(), "alice", command)
	require.NoError(t, err)
	_, err = f.b.API.SetLanguage(t.Context(), "alice", "ru", false)
	require.NoError(t, err)
	startMediaReconciler(t, f)
	require.Eventually(t, func() bool {
		card := mediaReconcileCard(t, f, "tg-media-100")
		button := mediaReconcileButton(card, first.ID+" · 65.00 BYN")
		return button.Data != "" && card.ID == old.Callback.Message.ID &&
			button.Data != old.Callback.Data && strings.Contains(card.Text, "Выберите заказ")
	}, 5*time.Second, 20*time.Millisecond)
	cash := orderCommand("cash", first)
	cash.PaymentAdmin = "bob"
	first, err = f.b.API.ExecuteOrder(t.Context(), "alice", cash)
	require.NoError(t, err)
	_, err = f.b.API.ExecuteOrder(t.Context(), "bob", orderCommand("accept", first))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		card := mediaReconcileCard(t, f, "tg-media-100")
		return mediaReconcileButton(card, first.ID).Data == "" &&
			mediaReconcileButton(card, second.ID).Data != "" && card.ID == old.Callback.Message.ID
	}, 5*time.Second, 20*time.Millisecond)
}

func TestMediaReconcileRetiresUnavailableCards(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, query, language, notice string }{
		{"intake expiry", `UPDATE bot.media_intake SET expires_at=now()-interval '1 second'`, "en", "I cannot access or process this attachment. Please send it again."},
		{"media expiry", `UPDATE core.media SET expires_at=now()-interval '1 second'`, "ru", "Не получается получить или обработать вложение. Отправьте его ещё раз."},
		{"missing media", `DELETE FROM core.media`, "en", "I cannot access or process this attachment. Please send it again."},
		{"revoked access", `UPDATE core.users SET can_book=false WHERE id='alice'`, "ru", "Не получается получить или обработать вложение. Отправьте его ещё раз."},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.b.API.SetLanguage(t.Context(), "alice", test.language, false)
			require.NoError(t, err)
			photo, _ := intakePhoto(t, f)
			f.model.plan = agent.Plan{View: agent.MediaView, Text: "What is this photo?"}
			handleVisible(t, f.b, photo)
			before := mediaReconcileCard(t, f, "tg-media-100")
			require.NotEmpty(t, before.Markup.Rows)
			_, err = f.db.Exec(t.Context(), test.query)
			require.NoError(t, err)
			startMediaReconciler(t, f)
			require.Eventually(t, func() bool {
				card := mediaReconcileCard(t, f, "tg-media-100")
				return card.ID == before.ID && strings.Contains(card.Text, test.notice) && len(card.Markup.Rows) == 0
			}, 5*time.Second, 20*time.Millisecond)
			require.Eventually(t, func() bool {
				var status, notice string
				err = f.db.QueryRow(t.Context(), `SELECT status,notice FROM bot.media_intake WHERE id='tg-media-100'`).
					Scan(&status, &notice)
				return err == nil && status == "done" && notice == "media.unavailable"
			}, 5*time.Second, 20*time.Millisecond)
		})
	}
}

type mediaCardFailure struct {
	messageID int64
	enabled   atomic.Bool
}

func (transport *mediaCardFailure) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/editMessageText") {
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
		if payload.MessageID == transport.messageID && transport.enabled.Load() {
			const response = `{"ok":false,"error_code":503,"description":"temporarily unavailable"}`
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(response)),
				Request:    request,
			}, nil
		}
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestMediaReconcileContinuesAfterCardFailure(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "en", false)
	require.NoError(t, err)
	photo, _ := intakePhoto(t, f)
	f.model.plan = agent.Plan{View: agent.MediaView, Text: "What is this photo?"}
	handle(t, f.b, photo)
	photo.ID = 101
	handle(t, f.b, photo)
	first := mediaReconcileCard(t, f, "tg-media-100")
	second := mediaReconcileCard(t, f, "tg-media-101")
	transport := &mediaCardFailure{messageID: first.ID}
	transport.enabled.Store(true)
	f.b.TG.HTTP = &http.Client{Transport: transport}
	_, err = f.db.Exec(t.Context(), `UPDATE bot.media_intake SET expires_at=now()-interval '1 second'`)
	require.NoError(t, err)
	startMediaReconciler(t, f)
	require.Eventually(t, func() bool {
		card := mediaReconcileCard(t, f, "tg-media-101")
		return card.ID == second.ID && len(card.Markup.Rows) == 0 && strings.Contains(card.Text, "I cannot access")
	}, 5*time.Second, 20*time.Millisecond)
	assert.NotEmpty(t, mediaReconcileCard(t, f, "tg-media-100").Markup.Rows)
	transport.enabled.Store(false)
	require.Eventually(t, func() bool {
		card := mediaReconcileCard(t, f, "tg-media-100")
		return card.ID == first.ID && len(card.Markup.Rows) == 0 && strings.Contains(card.Text, "I cannot access")
	}, 5*time.Second, 20*time.Millisecond)
}
