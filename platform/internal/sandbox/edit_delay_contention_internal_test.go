package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func delayContentionInput(t *testing.T) admittedDelivery {
	t.Helper()
	wire := deliveryRequest{ChatID: []byte("101"), MessageID: 7, Text: "replacement"}
	send, chat, err := wire.resolve()
	require.NoError(t, err)
	text, entities, err := deliveryText(send)
	require.NoError(t, err)
	send.Text = text
	return admittedDelivery{wire: wire, send: send, chat: chat, entities: entities}
}

func TestEditDelayCanceledMutationAdmissionEndsBeforeMutexOpens(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"deadline", "cancellation", "invalidation"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			f := &Fake{
				messages: []telegram.Message{{ID: 7, Chat: telegram.Chat{ID: 101, Type: "private"}, Text: "old"}},
			}
			d := &editDelay{invalidated: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			if reason == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
			}
			defer cancel()
			ctx = context.WithValue(ctx, delayApplyKey{}, d)
			in := delayContentionInput(t)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/botTOKEN/editMessageText", nil).WithContext(ctx)
			f.mu.Lock()
			locked := true
			defer func() {
				if locked {
					f.mu.Unlock()
				}
			}()
			started, done := make(chan struct{}), make(chan struct{})
			go func() {
				close(started)
				defer close(done)
				f.writeAdmittedMessage(response, request, in, "editMessageText")
			}()
			<-started
			if reason == "cancellation" {
				cancel()
			}
			if reason == "invalidation" {
				d.invalidate()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("selected mutation retained a mutex waiter after cancellation")
			}
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Equal(t, 0, f.edits)
			require.Equal(t, "old", f.messages[0].Text)
			f.mu.Unlock()
			locked = false
			// The selected call has ended before unlock; no detached worker remains
			// to mutate when the ordinary SQL-owning critical section later opens.
			require.Equal(t, 0, f.edits)
			require.Equal(t, "old", f.messages[0].Text)
		})
	}
}

func TestEditDelayMutationAdmissionPositiveControlAfterContention(t *testing.T) {
	t.Parallel()
	f := &Fake{messages: []telegram.Message{{ID: 7, Chat: telegram.Chat{ID: 101, Type: "private"}, Text: "old"}}}
	d := &editDelay{invalidated: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, delayApplyKey{}, d)
	in := delayContentionInput(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/botTOKEN/editMessageText", nil).WithContext(ctx)
	f.mu.Lock()
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		defer close(done)
		f.writeAdmittedMessage(response, request, in, "editMessageText")
	}()
	<-started
	f.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("live selected mutation failed to acquire released mutex")
	}
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, f.edits)
	require.Equal(t, "replacement", f.messages[0].Text)
	require.Contains(t, response.Body.String(), `"message_id":7`)
}
