package integration_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type inboxControlTransport struct {
	lookups atomic.Int64
	uploads atomic.Int64
	mu      sync.Mutex
	wire    []time.Time
}

func (tr *inboxControlTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	lookup := strings.HasSuffix(request.URL.Path, "/getFile")
	if lookup && tr.lookups.Add(1) == 1 {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":false,"error_code":429,"parameters":{"retry_after":10}}`)),
			Request: request,
		}, nil
	}
	if tr.lookups.Load() > 0 && (lookup || strings.HasSuffix(request.URL.Path, "/getUpdates")) {
		tr.mu.Lock()
		tr.wire = append(tr.wire, time.Now())
		tr.mu.Unlock()
	}
	if request.Method == http.MethodPost && request.URL.Path == "/v1/media" {
		tr.uploads.Add(1)
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestInboxControlCooldownSurvivesRestartWithoutPoisonCharges(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, original := intakePhoto(t, f)
	response := sandboxMediaRequest(t, f, "/lab/photo?user=202&filename=second.png", original)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	batch, err := f.b.TG.Updates(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, batch, 2)
	pacer, err := delivery.NewControlPacer(f.db, f.b.Delivery)
	require.NoError(t, err)
	f.b.TG.Control = pacer
	transport := &inboxControlTransport{}
	client := &http.Client{Transport: transport}
	f.b.TG.HTTP, f.b.API.HTTP, f.b.Host.HTTP = client, client, client
	var modelCalls atomic.Int64
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		modelCalls.Add(1)
		return agent.Plan{View: agent.MediaView, MediaAction: &agent.MediaProposal{
			MediaID: input.Attachment.ID, Intent: "avatar",
		}}, nil
	})
	stop := startRetryInbox(t, f)
	var deadline time.Time
	require.Eventually(t, func() bool {
		var deferred int
		queryErr := f.db.QueryRow(t.Context(), `SELECT p.not_before,
 (SELECT count(*) FROM bot.telegram_inbox i WHERE i.state='pending'
  AND i.failures=0 AND i.next_attempt_at>=p.not_before)
FROM core.delivery_pacing p WHERE p.bot_id=$1 AND p.chat=''`, f.b.Delivery.BotID).
			Scan(&deadline, &deferred)
		return queryErr == nil && deferred == 2 && deadline.After(time.Now())
	}, 10*time.Second, 10*time.Millisecond,
		"wire 429 and the second chat's local admission deferral preserve both failure budgets")
	stop()
	require.Equal(t, int64(1), transport.lookups.Load(), "second getFile must be deferred before the wire")
	require.Zero(t, transport.uploads.Load())
	require.Zero(t, modelCalls.Load())
	require.Equal(t, batch, retryInboxPayloads(t, f))
	assertInboxControlPending(t, f, deadline)

	restarted := *f.b
	restarted.TG.Control, err = delivery.NewControlPacer(f.db, restarted.Delivery)
	require.NoError(t, err)
	admission, err := restarted.TG.Control.Admit(t.Context())
	require.NoError(t, err)
	require.False(t, admission.Ready, "a reconstructed pacer reads the persisted cooldown")
	require.False(t, admission.NotBefore.Before(deadline))
	f.b = &restarted
	stop = startRetryInbox(t, f)
	require.Eventually(t, func() bool {
		var pending, intakes int
		queryErr := f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM bot.telegram_inbox),(SELECT count(*) FROM bot.media_intake)`).Scan(&pending, &intakes)
		return queryErr == nil && pending == 0 && intakes == 2
	}, max(time.Until(deadline), 0)+10*time.Second, 20*time.Millisecond)
	stop()
	require.Equal(t, int64(3), transport.lookups.Load(), "one failed lookup and one successful lookup per photo")
	require.Equal(t, int64(2), transport.uploads.Load(), "each photo is uploaded once")
	require.Equal(t, int64(2), modelCalls.Load())
	transport.mu.Lock()
	wire := append([]time.Time(nil), transport.wire...)
	transport.mu.Unlock()
	require.NotEmpty(t, wire)
	for _, sent := range wire {
		require.False(t, sent.Before(deadline), "neither getFile nor getUpdates may reach the wire during cooldown")
	}
	var stored int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.media_intake i
 JOIN core.media m ON m.id=i.attachment_id WHERE m.body=$1`, original).Scan(&stored))
	require.Equal(t, 2, stored, "both original photo bodies survive deferred intake")
	for _, update := range batch {
		_, err = f.db.Exec(
			t.Context(),
			"INSERT INTO bot.telegram_inbox(update_id,payload) VALUES($1,$2)",
			update.ID,
			update,
		)
		require.NoError(t, err)
	}
	stop = startRetryInbox(t, f)
	require.Eventually(t, func() bool {
		var pending int
		queryErr := f.db.QueryRow(t.Context(), "SELECT count(*) FROM bot.telegram_inbox").Scan(&pending)
		return queryErr == nil && pending == 0
	}, 5*time.Second, 20*time.Millisecond)
	stop()
	require.Equal(t, int64(3), transport.lookups.Load(), "completed replay does not fetch photos again")
	require.Equal(t, int64(2), transport.uploads.Load(), "completed replay does not upload photos again")
	require.Equal(t, int64(2), modelCalls.Load(), "completed replay does not reinterpret photos")
}

func assertInboxControlPending(t *testing.T, f *fixture, deadline time.Time) {
	t.Helper()
	var failures, pending int
	var earliest time.Time
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT sum(failures),count(*),min(next_attempt_at)
FROM bot.telegram_inbox WHERE state='pending'`).Scan(&failures, &pending, &earliest))
	require.Zero(t, failures)
	require.Equal(t, 2, pending)
	require.False(t, earliest.Before(deadline), "both inbox deadlines retain the bot-global cooldown")
}
